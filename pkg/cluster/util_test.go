package cluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sFake "k8s.io/client-go/kubernetes/fake"
)

func newFakeK8sAnnotationsClient() (k8sutil.KubernetesClient, *k8sFake.Clientset) {
	clientSet := k8sFake.NewSimpleClientset()
	acidClientSet := fakeacidv1.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		DeploymentsGetter:            clientSet.AppsV1(),
		PersistentVolumeClaimsGetter: clientSet.CoreV1(),
		PodDisruptionBudgetsGetter:   clientSet.PolicyV1beta1(),
		ServicesGetter:               clientSet.CoreV1(),
		StatefulSetsGetter:           clientSet.AppsV1(),
		PostgresqlsGetter:            acidClientSet.AcidV1(),
	}, clientSet
}

func TestInheritedAnnotations(t *testing.T) {
	testName := "test inheriting annotations from manifest"
	client, _ := newFakeK8sAnnotationsClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	annotationValue := "acid"

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
			Annotations: map[string]string{
				"owned-by": annotationValue,
			},
		},
		Spec: acidv1.PostgresSpec{
			EnableReplicaConnectionPooler: boolToPointer(true),
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					InheritedAnnotations: []string{"owned-by"},
					PodRoleLabel:         "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	cluster.Name = clusterName
	cluster.Namespace = namespace

	// test annotationsSet function
	inheritedAnnotations := cluster.annotationsSet(nil)

	poolerListOptions := metav1.ListOptions{
		LabelSelector: labels.Set(cluster.connectionPoolerLabels(Master, false).MatchLabels).String(),
	}

	// check pooler deployment annotations
	cluster.createConnectionPooler(cluster.installLookupFunction)
	deployList, err := client.Deployments(namespace).List(context.TODO(), poolerListOptions)
	assert.NoError(t, err)
	for _, deploy := range deployList.Items {
		if !(util.MapContains(deploy.ObjectMeta.Annotations, inheritedAnnotations)) {
			t.Errorf("%s: Deployment %v not inherited annotations %#v, got %#v", testName, deploy.ObjectMeta.Name, inheritedAnnotations, deploy.ObjectMeta.Annotations)
		}
	}

	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(false).String(),
	}

	// check statefulset annotations
	cluster.createStatefulSet()
	stsList, err := client.StatefulSets(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	for _, sts := range stsList.Items {
		if !(util.MapContains(sts.ObjectMeta.Annotations, inheritedAnnotations)) {
			t.Errorf("%s: StatefulSet %v not inherited annotations %#v, got %#v", testName, sts.ObjectMeta.Name, inheritedAnnotations, sts.ObjectMeta.Annotations)
		}

		if !(util.MapContains(sts.Spec.Template.Annotations, inheritedAnnotations)) {
			t.Errorf("%s: pod template %v not inherited annotations %#v, got %#v", testName, sts.ObjectMeta.Name, inheritedAnnotations, sts.ObjectMeta.Annotations)
		}
	}

	// check service annotations
	cluster.createService(Master)
	svcList, err := client.Services(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	for _, svc := range svcList.Items {
		if !(util.MapContains(svc.ObjectMeta.Annotations, inheritedAnnotations)) {
			t.Errorf("%s: Service %v not inherited annotations %#v, got %#v", testName, svc.ObjectMeta.Name, inheritedAnnotations, svc.ObjectMeta.Annotations)
		}
	}

	// check pod disruption budget annotations
	cluster.createPodDisruptionBudget()
	pdbList, err := client.PodDisruptionBudgets(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	for _, pdb := range pdbList.Items {
		if !(util.MapContains(pdb.ObjectMeta.Annotations, inheritedAnnotations)) {
			t.Errorf("%s: Pod Disruption Budget %v not inherited annotations %#v, got %#v", testName, pdb.ObjectMeta.Name, inheritedAnnotations, pdb.ObjectMeta.Annotations)
		}
	}
}
