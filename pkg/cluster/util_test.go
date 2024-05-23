package cluster

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/postgres-operator/mocks"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/patroni"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sFake "k8s.io/client-go/kubernetes/fake"
)

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
		PodsGetter:                   clientSet.CoreV1(),
		DeploymentsGetter:            clientSet.AppsV1(),
	}, clientSet
}

func createPods(cluster *Cluster) []v1.Pod {
	podsList := make([]v1.Pod, 0)
	for i, role := range []PostgresRole{Master, Replica} {
		podsList = append(podsList, v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", clusterName, i),
				Namespace: namespace,
				Labels: map[string]string{
					"application":  "spilo",
					"cluster-name": clusterName,
					"spilo-role":   string(role),
				},
			},
		})
		podsList = append(podsList, v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-pooler-%s", clusterName, role),
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
				"foo":      "bar",
			},
		},
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler:        boolToPointer(true),
			EnableReplicaConnectionPooler: boolToPointer(true),
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	cluster := New(
		Config{
			OpConfig: config.Config{
				PatroniAPICheckInterval: time.Duration(1),
				PatroniAPICheckTimeout:  time.Duration(5),
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
					IgnoredAnnotations:    []string{"ignore"},
					PodRoleLabel:          "spilo-role",
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
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
	_, err = cluster.createPodDisruptionBudget()
	if err != nil {
		return nil, err
	}
	_, err = cluster.createConnectionPooler(mockInstallLookupFunction)
	if err != nil {
		return nil, err
	}
	pvcList := CreatePVCs(namespace, clusterName, cluster.labelsSet(false), 2, "1Gi")
	for _, pvc := range pvcList.Items {
		cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	}
	podsList := createPods(cluster)
	for _, pod := range podsList {
		cluster.KubeClient.Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	}

	return cluster, nil
}

func annotateResources(cluster *Cluster) error {
	externalAnnotations := map[string]string{"ignore": "me", "do-not-ignore": "me"}
	listOptions := metav1.ListOptions{LabelSelector: cluster.labelsSet(false).String()}

	stsList, err := cluster.KubeClient.StatefulSets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	for _, sts := range stsList.Items {
		sts.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.StatefulSets(namespace).Update(context.TODO(), &sts, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	podList, err := cluster.KubeClient.Pods(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		pod.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Pods(namespace).Update(context.TODO(), &pod, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	svcList, err := cluster.KubeClient.Services(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	for _, svc := range svcList.Items {
		svc.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Services(namespace).Update(context.TODO(), &svc, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	pdbList, err := cluster.KubeClient.PodDisruptionBudgets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	for _, pdb := range pdbList.Items {
		pdb.Annotations = externalAnnotations
		_, err = cluster.KubeClient.PodDisruptionBudgets(namespace).Update(context.TODO(), &pdb, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	pvcList, err := cluster.KubeClient.PersistentVolumeClaims(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	for _, pvc := range pvcList.Items {
		pvc.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.PersistentVolumeClaims(namespace).Update(context.TODO(), &pvc, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	for _, role := range []PostgresRole{Master, Replica} {
		deploy, err := cluster.KubeClient.Deployments(namespace).Get(context.TODO(), cluster.connectionPoolerName(role), metav1.GetOptions{})
		if err != nil {
			return err
		}
		deploy.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Deployments(namespace).Update(context.TODO(), deploy, metav1.UpdateOptions{}); err != nil {
			return err
		}
		poolerPodList, err := cluster.KubeClient.Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set(cluster.connectionPoolerLabels(role, true).MatchLabels).String(),
		})
		if err != nil {
			return err
		}
		for _, pod := range poolerPodList.Items {
			pod.Annotations = externalAnnotations
			if _, err = cluster.KubeClient.Pods(namespace).Update(context.TODO(), &pod, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
	}

	secrets, err := cluster.KubeClient.Secrets(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		secret.Annotations = externalAnnotations
		if _, err = cluster.KubeClient.Secrets(namespace).Update(context.TODO(), &secret, metav1.UpdateOptions{}); err != nil {
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
	listOptions := metav1.ListOptions{
		LabelSelector: filterLabels.String(),
	}

	// helper functions
	containsAnnotations := func(expected map[string]string, actual map[string]string, objName string, objType string) error {
		if expected == nil {
			if len(actual) != 0 {
				return fmt.Errorf("%s%v expected not to have any annotations, got: %#v", objType, objName, actual)
			}
		} else if !(reflect.DeepEqual(expected, actual)) {
			return fmt.Errorf("%s%v expected annotations: %#v, got: %#v", objType, objName, expected, actual)
		}
		return nil
	}

	checkSts := func(annotations map[string]string, noTemplate bool) error {
		stsList, err := client.StatefulSets(namespace).List(context.TODO(), listOptions)
		if err != nil {
			return err
		}

		for _, sts := range stsList.Items {
			if err := containsAnnotations(annotations, sts.ObjectMeta.Annotations, sts.ObjectMeta.Name, "StatefulSet"); err != nil {
				return err
			}
			if noTemplate {
				continue
			}
			// pod template
			if err := containsAnnotations(annotations, sts.Spec.Template.ObjectMeta.Annotations, sts.ObjectMeta.Name, "StatefulSet pod template"); err != nil {
				return err
			}
			// pvc template
			if err := containsAnnotations(annotations, sts.Spec.VolumeClaimTemplates[0].Annotations, sts.ObjectMeta.Name, "StatefulSet pvc template"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPods := func(annotations map[string]string, labelSelector metav1.ListOptions) error {
		podList, err := client.Pods(namespace).List(context.TODO(), labelSelector)
		if err != nil {
			return err
		}
		for _, pod := range podList.Items {
			if err := containsAnnotations(annotations, pod.ObjectMeta.Annotations, pod.ObjectMeta.Name, "Pod"); err != nil {
				return err
			}
		}
		return nil
	}

	checkSvc := func(annotations map[string]string, labelSelector metav1.ListOptions) error {
		svcList, err := client.Services(namespace).List(context.TODO(), labelSelector)
		if err != nil {
			return err
		}
		for _, svc := range svcList.Items {
			if err := containsAnnotations(annotations, svc.ObjectMeta.Annotations, svc.ObjectMeta.Name, "Service"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPdb := func(annotations map[string]string) error {
		pdbList, err := client.PodDisruptionBudgets(namespace).List(context.TODO(), listOptions)
		if err != nil {
			return err
		}
		for _, pdb := range pdbList.Items {
			if err := containsAnnotations(annotations, pdb.ObjectMeta.Annotations, pdb.ObjectMeta.Name, "Pod Disruption Budget"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPvc := func(annotations map[string]string) error {
		pvcs, err := cluster.listPersistentVolumeClaims()
		if err != nil {
			return err
		}
		for _, pvc := range pvcs {
			if err := containsAnnotations(annotations, pvc.ObjectMeta.Annotations, pvc.ObjectMeta.Name, "Volume claim"); err != nil {
				return err
			}
		}
		return nil
	}

	checkPooler := func(annotations map[string]string, noTemplate bool) error {
		for _, role := range []PostgresRole{Master, Replica} {
			poolerListOptions := metav1.ListOptions{
				LabelSelector: labels.Set(cluster.connectionPoolerLabels(role, true).MatchLabels).String(),
			}

			deploy, err := client.Deployments(namespace).Get(context.TODO(), cluster.connectionPoolerName(role), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if err := containsAnnotations(annotations, deploy.Annotations, deploy.Name, "Deployment"); err != nil {
				return err
			}
			if err := checkSvc(annotations, poolerListOptions); err != nil {
				return err
			}
			if err := checkPods(annotations, poolerListOptions); err != nil {
				return err
			}
			if noTemplate {
				continue
			}
			if err := containsAnnotations(annotations, deploy.Spec.Template.Annotations, deploy.Name, "Pooler pod template"); err != nil {
				return err
			}
		}
		return nil
	}

	checkSecrets := func(annotations map[string]string) error {
		secretList, err := cluster.KubeClient.Secrets(namespace).List(context.TODO(), listOptions)
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

	checkResources := func(resultAnnotations map[string]string, noTemplate bool) error {
		if err = checkSts(resultAnnotations, noTemplate); err != nil {
			return err
		}
		if err = checkPods(resultAnnotations, listOptions); err != nil {
			return err
		}
		if err = checkSvc(resultAnnotations, listOptions); err != nil {
			return err
		}
		if err = checkPdb(resultAnnotations); err != nil {
			return err
		}
		if err = checkPooler(resultAnnotations, noTemplate); err != nil {
			return err
		}
		if err = checkPvc(resultAnnotations); err != nil {
			return err
		}
		if err = checkSecrets(resultAnnotations); err != nil {
			return err
		}
		return nil
	}

	// Finally, tests!
	result := map[string]string{"owned-by": "acid"}
	assert.True(t, reflect.DeepEqual(result, cluster.annotationsSet(nil)))

	// 1. Check initial state
	err = checkResources(result, false)
	assert.NoError(t, err)

	// 2. Check annotation value change

	// 2.1 Sync event
	newSpec := cluster.Postgresql.DeepCopy()
	newSpec.ObjectMeta.Annotations["owned-by"] = "fooSync"
	result["owned-by"] = "fooSync"
	// + new PVC
	cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &CreatePVCs(namespace, clusterName, filterLabels, 1, "1Gi").Items[0], metav1.CreateOptions{})
	err = cluster.Sync(newSpec)
	assert.NoError(t, err)

	err = checkResources(result, false)
	assert.NoError(t, err)

	// 2.2 Update event
	newSpec = cluster.Postgresql.DeepCopy()
	newSpec.ObjectMeta.Annotations["owned-by"] = "fooUpdate"
	result["owned-by"] = "fooUpdate"
	// + new PVC
	cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &CreatePVCs(namespace, clusterName, filterLabels, 1, "1Gi").Items[0], metav1.CreateOptions{})

	err = cluster.Update(cluster.Postgresql.DeepCopy(), newSpec)
	assert.NoError(t, err)

	err = checkResources(result, false)
	assert.NoError(t, err)

	// 3. Ignored annotations
	err = annotateResources(cluster)
	assert.NoError(t, err)
	err = cluster.Sync(newSpec.DeepCopy())
	assert.NoError(t, err)
	err = checkResources(map[string]string{"ignore": "me", "owned-by": "fooUpdate"}, true)
	assert.NoError(t, err)

	// 4. Check removal of an inherited annotation

	// 4.1 remove parameter from operator config
	cluster.OpConfig.IgnoredAnnotations = nil
	cluster.OpConfig.InheritedAnnotations = nil
	err = cluster.Sync(newSpec.DeepCopy())
	assert.NoError(t, err)

	err = checkResources(nil, false)
	assert.NoError(t, err)

	cluster.OpConfig.InheritedAnnotations = []string{"owned-by"}
	err = cluster.Sync(newSpec.DeepCopy())
	assert.NoError(t, err)

	// 4.2 delete value for the inherited annotation
	delete(newSpec.Annotations, "owned-by")
	err = cluster.Update(cluster.Postgresql.DeepCopy(), newSpec.DeepCopy())
	assert.NoError(t, err)

	err = checkResources(nil, false)
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
