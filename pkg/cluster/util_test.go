package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/postgres-operator/mocks"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/patroni"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	k8sFake "k8s.io/client-go/kubernetes/fake"
)

var externalAnnotations = map[string]string{"existing": "annotation"}

func mustParseTime(s string) metav1.Time {
	v, err := time.Parse("15:04", s)
	if err != nil {
		panic(err)
	}

	return metav1.Time{Time: v.UTC()}
}

func newFakeK8sAnnotationsClient() (k8sutil.KubernetesClient, *k8sFake.Clientset) {
	clientSet := k8sFake.NewSimpleClientset()
	acidClientSet := fakeacidv1.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		PodDisruptionBudgetsGetter:   clientSet.PolicyV1(),
		SecretsGetter:                clientSet.CoreV1(),
		ServicesGetter:               clientSet.CoreV1(),
		StatefulSetsGetter:           clientSet.AppsV1(),
		PostgresqlsGetter:            acidClientSet.AcidV1(),
		PersistentVolumeClaimsGetter: clientSet.CoreV1(),
		PersistentVolumesGetter:      clientSet.CoreV1(),
		EndpointsGetter:              clientSet.CoreV1(),
		ConfigMapsGetter:             clientSet.CoreV1(),
		PodsGetter:                   clientSet.CoreV1(),
		DeploymentsGetter:            clientSet.AppsV1(),
		CronJobsGetter:               clientSet.BatchV1(),
	}, clientSet
}

func clusterLabelsOptions(cluster *Cluster) metav1.ListOptions {
	clusterLabel := labels.Set(map[string]string{cluster.OpConfig.ClusterNameLabel: cluster.Name})
	return metav1.ListOptions{
		LabelSelector: clusterLabel.String(),
	}
}

func checkResourcesInheritedAnnotations(cluster *Cluster, resultAnnotations map[string]string) error {
	clusterOptions := clusterLabelsOptions(cluster)
	// helper functions
	containsAnnotations := func(expected map[string]string, actual map[string]string, objName string, objType string) error {
		if !util.MapContains(actual, expected) {
			return fmt.Errorf("%s %v expected annotations %#v to be contained in %#v", objType, objName, expected, actual)
		}
		return nil
	}

	updateAnnotations := func(annotations map[string]string) map[string]string {
		result := make(map[string]string, 0)
		for anno := range annotations {
			if _, ok := externalAnnotations[anno]; !ok {
				result[anno] = annotations[anno]
			}
		}
		return result
	}

	checkSts := func(annotations map[string]string) error {
		stsList, err := cluster.KubeClient.StatefulSets(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		stsAnnotations := updateAnnotations(annotations)

		for _, sts := range stsList.Items {
			if err := containsAnnotations(stsAnnotations, sts.Annotations, sts.ObjectMeta.Name, "StatefulSet"); err != nil {
				return err
			}
			// pod template
			if err := containsAnnotations(stsAnnotations, sts.Spec.Template.Annotations, sts.ObjectMeta.Name, "StatefulSet pod template"); err != nil {
				return err
			}
			// pvc template
			if err := containsAnnotations(stsAnnotations, sts.Spec.VolumeClaimTemplates[0].Annotations, sts.ObjectMeta.Name, "StatefulSet pvc template"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPods := func(annotations map[string]string) error {
		podList, err := cluster.KubeClient.Pods(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, pod := range podList.Items {
			if err := containsAnnotations(annotations, pod.Annotations, pod.ObjectMeta.Name, "Pod"); err != nil {
				return err
			}
		}
		return nil
	}

	checkSvc := func(annotations map[string]string) error {
		svcList, err := cluster.KubeClient.Services(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, svc := range svcList.Items {
			if err := containsAnnotations(annotations, svc.Annotations, svc.ObjectMeta.Name, "Service"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPdb := func(annotations map[string]string) error {
		pdbList, err := cluster.KubeClient.PodDisruptionBudgets(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, pdb := range pdbList.Items {
			if err := containsAnnotations(updateAnnotations(annotations), pdb.Annotations, pdb.ObjectMeta.Name, "Pod Disruption Budget"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPvc := func(annotations map[string]string) error {
		pvcList, err := cluster.KubeClient.PersistentVolumeClaims(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, pvc := range pvcList.Items {
			if err := containsAnnotations(annotations, pvc.Annotations, pvc.ObjectMeta.Name, "Volume claim"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPooler := func(annotations map[string]string) error {
		for _, role := range []PostgresRole{Master, Replica} {
			deploy, err := cluster.KubeClient.Deployments(namespace).Get(context.TODO(), cluster.connectionPoolerName(role), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if err := containsAnnotations(annotations, deploy.Annotations, deploy.Name, "Deployment"); err != nil {
				return err
			}
			if err := containsAnnotations(updateAnnotations(annotations), deploy.Spec.Template.Annotations, deploy.Name, "Pooler pod template"); err != nil {
				return err
			}
		}
		return nil
	}

	checkCronJob := func(annotations map[string]string) error {
		cronJobList, err := cluster.KubeClient.CronJobs(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, cronJob := range cronJobList.Items {
			if err := containsAnnotations(annotations, cronJob.Annotations, cronJob.ObjectMeta.Name, "Logical backup cron job"); err != nil {
				return err
			}
			if err := containsAnnotations(updateAnnotations(annotations), cronJob.Spec.JobTemplate.Spec.Template.Annotations, cronJob.Name, "Logical backup cron job pod template"); err != nil {
				return err
			}
		}
		return nil
	}

	checkSecrets := func(annotations map[string]string) error {
		secretList, err := cluster.KubeClient.Secrets(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, secret := range secretList.Items {
			if err := containsAnnotations(annotations, secret.Annotations, secret.Name, "Secret"); err != nil {
				return err
			}
		}
		return nil
	}

	checkEndpoints := func(annotations map[string]string) error {
		endpointsList, err := cluster.KubeClient.Endpoints(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, ep := range endpointsList.Items {
			if err := containsAnnotations(annotations, ep.Annotations, ep.Name, "Endpoints"); err != nil {
				return err
			}
		}
		return nil
	}

	checkConfigMaps := func(annotations map[string]string) error {
		cmList, err := cluster.KubeClient.ConfigMaps(namespace).List(context.TODO(), clusterOptions)
		if err != nil {
			return err
		}
		for _, cm := range cmList.Items {
			if err := containsAnnotations(annotations, cm.Annotations, cm.ObjectMeta.Name, "ConfigMap"); err != nil {
				return err
			}
		}
		return nil
	}

	checkFuncs := []func(map[string]string) error{
		checkSts, checkPods, checkSvc, checkPdb, checkPooler, checkCronJob, checkPvc, checkSecrets, checkEndpoints, checkConfigMaps,
	}
	for _, f := range checkFuncs {
		if err := f(resultAnnotations); err != nil {
			return err
		}
	}
	return nil
}

func createPods(cluster *Cluster) []v1.Pod {
	podsList := make([]v1.Pod, 0)
	for i, role := range []PostgresRole{Master, Replica} {
		podsList = append(podsList, v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", cluster.Name, i),
				Namespace: namespace,
				Labels: map[string]string{
					"application":  "spilo",
					"cluster-name": cluster.Name,
					"spilo-role":   string(role),
				},
			},
		})
		podsList = append(podsList, v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-pooler-%s", cluster.Name, role),
				Namespace: namespace,
				Labels:    cluster.connectionPoolerLabels(role, true).MatchLabels,
			},
		})
	}

	return podsList
}

func newInheritedAnnotationsCluster(client k8sutil.KubernetesClient) (*Cluster, error) {
	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
			Annotations: map[string]string{
				"owned-by": "acid",
				"foo":      "bar", // should not be inherited
			},
		},
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler:        boolToPointer(true),
			EnableReplicaConnectionPooler: boolToPointer(true),
			EnableLogicalBackup:           true,
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
			NumberOfInstances: 2,
		},
	}

	cluster := New(
		Config{
			OpConfig: config.Config{
				PatroniAPICheckInterval: time.Duration(1),
				PatroniAPICheckTimeout:  time.Duration(5),
				KubernetesUseConfigMaps: true,
				ConnectionPooler: config.ConnectionPooler{
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:                    k8sutil.Int32ToPointer(1),
				},
				PDBNameFormat:       "postgres-{cluster}-pdb",
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:         map[string]string{"application": "spilo"},
					ClusterNameLabel:      "cluster-name",
					DefaultCPURequest:     "300m",
					DefaultCPULimit:       "300m",
					DefaultMemoryRequest:  "300Mi",
					DefaultMemoryLimit:    "300Mi",
					InheritedAnnotations:  []string{"owned-by"},
					PodRoleLabel:          "spilo-role",
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
					MinInstances:          -1,
					MaxInstances:          -1,
				},
			},
		}, client, pg, logger, eventRecorder)
	cluster.Name = clusterName
	cluster.Namespace = namespace
	_, err := cluster.createStatefulSet()
	if err != nil {
		return nil, err
	}
	_, err = cluster.createService(Master)
	if err != nil {
		return nil, err
	}
	err = cluster.createPodDisruptionBudgets()
	if err != nil {
		return nil, err
	}
	_, err = cluster.createConnectionPooler(mockInstallLookupFunction)
	if err != nil {
		return nil, err
	}
	err = cluster.createLogicalBackupJob()
	if err != nil {
		return nil, err
	}
	pvcList := CreatePVCs(namespace, clusterName, cluster.labelsSet(false), 2, "1Gi")
	for _, pvc := range pvcList.Items {
		_, err = cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}
	podsList := createPods(cluster)
	for _, pod := range podsList {
		_, err = cluster.KubeClient.Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}

	// resources which Patroni creates
	if err = createPatroniResources(cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

func createPatroniResources(cluster *Cluster) error {
	patroniService := cluster.generateService(Replica, &pg.Spec)
	patroniService.ObjectMeta.Name = cluster.serviceName(Patroni)
	_, err := cluster.KubeClient.Services(namespace).Create(context.TODO(), patroniService, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	for _, suffix := range patroniObjectSuffixes {
		metadata := metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", clusterName, suffix),
			Namespace: namespace,
			Annotations: map[string]string{
				"initialize": "123456789",
			},
			Labels: cluster.labelsSet(false),
		}

		if cluster.OpConfig.KubernetesUseConfigMaps {
			configMap := v1.ConfigMap{
				ObjectMeta: metadata,
			}
			_, err := cluster.KubeClient.ConfigMaps(namespace).Create(context.TODO(), &configMap, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			endpoints := v1.Endpoints{
				ObjectMeta: metadata,
			}
			_, err := cluster.KubeClient.Endpoints(namespace).Create(context.TODO(), &endpoints, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func annotateResources(cluster *Cluster) error {
	clusterOptions := clusterLabelsOptions(cluster)
	patchData, err := metaAnnotationsPatch(externalAnnotations)
	if err != nil {
		return err
	}

	stsList, err := cluster.KubeClient.StatefulSets(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, sts := range stsList.Items {
		sts.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.StatefulSets(namespace).Patch(context.TODO(), sts.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	podList, err := cluster.KubeClient.Pods(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		pod.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Pods(namespace).Patch(context.TODO(), pod.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	svcList, err := cluster.KubeClient.Services(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, svc := range svcList.Items {
		svc.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Services(namespace).Patch(context.TODO(), svc.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	pdbList, err := cluster.KubeClient.PodDisruptionBudgets(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, pdb := range pdbList.Items {
		pdb.Annotations = externalAnnotations
		_, err = cluster.KubeClient.PodDisruptionBudgets(namespace).Patch(context.TODO(), pdb.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}

	cronJobList, err := cluster.KubeClient.CronJobs(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, cronJob := range cronJobList.Items {
		cronJob.Annotations = externalAnnotations
		_, err = cluster.KubeClient.CronJobs(namespace).Patch(context.TODO(), cronJob.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}

	pvcList, err := cluster.KubeClient.PersistentVolumeClaims(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, pvc := range pvcList.Items {
		pvc.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.PersistentVolumeClaims(namespace).Patch(context.TODO(), pvc.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	for _, role := range []PostgresRole{Master, Replica} {
		deploy, err := cluster.KubeClient.Deployments(namespace).Get(context.TODO(), cluster.connectionPoolerName(role), metav1.GetOptions{})
		if err != nil {
			return err
		}
		deploy.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Deployments(namespace).Patch(context.TODO(), deploy.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	secrets, err := cluster.KubeClient.Secrets(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		secret.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Secrets(namespace).Patch(context.TODO(), secret.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	endpoints, err := cluster.KubeClient.Endpoints(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, ep := range endpoints.Items {
		ep.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Endpoints(namespace).Patch(context.TODO(), ep.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	configMaps, err := cluster.KubeClient.ConfigMaps(namespace).List(context.TODO(), clusterOptions)
	if err != nil {
		return err
	}
	for _, cm := range configMaps.Items {
		cm.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.ConfigMaps(namespace).Patch(context.TODO(), cm.Name, types.MergePatchType, []byte(patchData), metav1.PatchOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func TestInheritedAnnotations(t *testing.T) {
	// mocks
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client, _ := newFakeK8sAnnotationsClient()
	mockClient := mocks.NewMockHTTPClient(ctrl)

	cluster, err := newInheritedAnnotationsCluster(client)
	assert.NoError(t, err)

	configJson := `{"postgresql": {"parameters": {"log_min_duration_statement": 200, "max_connections": 50}}}, "ttl": 20}`
	response := http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(configJson))),
	}
	mockClient.EXPECT().Do(gomock.Any()).Return(&response, nil).AnyTimes()
	cluster.patroni = patroni.New(patroniLogger, mockClient)

	err = cluster.Sync(&cluster.Postgresql)
	assert.NoError(t, err)

	filterLabels := cluster.labelsSet(false)

	// Finally, tests!
	result := map[string]string{"owned-by": "acid"}
	assert.True(t, reflect.DeepEqual(result, cluster.annotationsSet(nil)))

	// 1. Check initial state
	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)

	// 2. Check annotation value change

	// 2.1 Sync event
	newSpec := cluster.Postgresql.DeepCopy()
	newSpec.Annotations["owned-by"] = "fooSync"
	result["owned-by"] = "fooSync"

	err = cluster.Sync(newSpec)
	assert.NoError(t, err)
	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)

	// + existing PVC without annotations
	cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &CreatePVCs(namespace, clusterName, filterLabels, 3, "1Gi").Items[2], metav1.CreateOptions{})
	err = cluster.Sync(newSpec)
	assert.NoError(t, err)
	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)

	// 2.2 Update event
	newSpec = cluster.Postgresql.DeepCopy()
	newSpec.Annotations["owned-by"] = "fooUpdate"
	result["owned-by"] = "fooUpdate"
	// + new PVC
	cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &CreatePVCs(namespace, clusterName, filterLabels, 4, "1Gi").Items[3], metav1.CreateOptions{})

	err = cluster.Update(cluster.Postgresql.DeepCopy(), newSpec)
	assert.NoError(t, err)

	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)

	// 3. Change from ConfigMaps to Endpoints
	err = cluster.deletePatroniResources()
	assert.NoError(t, err)
	cluster.OpConfig.KubernetesUseConfigMaps = false
	err = createPatroniResources(cluster)
	assert.NoError(t, err)
	err = cluster.Sync(newSpec.DeepCopy())
	assert.NoError(t, err)
	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)

	// 4. Existing annotations (should not be removed)
	err = annotateResources(cluster)
	assert.NoError(t, err)
	maps.Copy(result, externalAnnotations)
	err = cluster.Sync(newSpec.DeepCopy())
	assert.NoError(t, err)
	err = checkResourcesInheritedAnnotations(cluster, result)
	assert.NoError(t, err)
}

func Test_trimCronjobName(t *testing.T) {
	type args struct {
		name string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "short name",
			args: args{
				name: "short-name",
			},
			want: "short-name",
		},
		{
			name: "long name",
			args: args{
				name: "very-very-very-very-very-very-very-very-very-long-db-name",
			},
			want: "very-very-very-very-very-very-very-very-very-long-db",
		},
		{
			name: "long name should not end with dash",
			args: args{
				name: "very-very-very-very-very-very-very-very-very-----------long-db-name",
			},
			want: "very-very-very-very-very-very-very-very-very",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimCronjobName(tt.args.name); got != tt.want {
				t.Errorf("trimCronjobName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInMaintenanceWindow(t *testing.T) {
	now := time.Now()
	futureTimeStart := now.Add(1 * time.Hour)
	futureTimeStartFormatted := futureTimeStart.Format("15:04")
	futureTimeEnd := now.Add(2 * time.Hour)
	futureTimeEndFormatted := futureTimeEnd.Format("15:04")

	tests := []struct {
		name     string
		windows  []acidv1.MaintenanceWindow
		expected bool
	}{
		{
			name:     "no maintenance windows",
			windows:  nil,
			expected: true,
		},
		{
			name: "maintenance windows with everyday",
			windows: []acidv1.MaintenanceWindow{
				{
					Everyday:  true,
					StartTime: mustParseTime("00:00"),
					EndTime:   mustParseTime("23:59"),
				},
			},
			expected: true,
		},
		{
			name: "maintenance windows with weekday",
			windows: []acidv1.MaintenanceWindow{
				{
					Weekday:   now.Weekday(),
					StartTime: mustParseTime("00:00"),
					EndTime:   mustParseTime("23:59"),
				},
			},
			expected: true,
		},
		{
			name: "maintenance windows with future interval time",
			windows: []acidv1.MaintenanceWindow{
				{
					Weekday:   now.Weekday(),
					StartTime: mustParseTime(futureTimeStartFormatted),
					EndTime:   mustParseTime(futureTimeEndFormatted),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster.Spec.MaintenanceWindows = tt.windows
			if isInMaintenanceWindow(cluster.Spec.MaintenanceWindows) != tt.expected {
				t.Errorf("Expected isInMaintenanceWindow to return %t", tt.expected)
			}
		})
	}
}
