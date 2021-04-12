package cluster

import (
	"testing"
	"time"

	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"k8s.io/client-go/kubernetes/fake"
)

func newFakeK8sSyncClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	acidClientSet := fakeacidv1.NewSimpleClientset()
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		PodsGetter:         clientSet.CoreV1(),
		PostgresqlsGetter:  acidClientSet.AcidV1(),
		StatefulSetsGetter: clientSet.AppsV1(),
	}, clientSet
}

func TestSyncStatefulSetsAnnotations(t *testing.T) {
	testName := "test syncing statefulsets annotations"
	client, _ := newFakeK8sSyncClient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clusterName,
			Namespace:   namespace,
			Annotations: map[string]string{"test-anno": "true"},
		},
		Spec: acidv1.PostgresSpec{
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:         map[string]string{"application": "spilo"},
					ClusterNameLabel:      "cluster-name",
					DefaultCPURequest:     "300m",
					DefaultCPULimit:       "300m",
					DefaultMemoryRequest:  "300Mi",
					DefaultMemoryLimit:    "300Mi",
					InheritedAnnotations:  []string{"test-anno"},
					PodRoleLabel:          "spilo-role",
					ResourceCheckInterval: time.Duration(3),
					ResourceCheckTimeout:  time.Duration(10),
				},
			},
		}, client, pg, logger, eventRecorder)

	cluster.Name = clusterName
	cluster.Namespace = namespace

	// create a new Postgresql resource
	_, err := cluster.KubeClient.Postgresqls(namespace).Create(
		context.TODO(), &pg, metav1.CreateOptions{})
	assert.NoError(t, err)

	// create a statefulset
	sts, err := cluster.createStatefulSet()
	assert.NoError(t, err)
	cluster.Statefulset = sts

	// update postgresql and remove annotation to force sync
	newPg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	_, err = cluster.KubeClient.Postgresqls(namespace).Update(
		context.TODO(), &newPg, metav1.UpdateOptions{})
	assert.NoError(t, err)

	// empty annotations on cluster ObjectMeta object as well
	cluster.ObjectMeta.Annotations = map[string]string{}

	// first compare running with desired statefulset - they should not match
	desiredSts, err := cluster.generateStatefulSet(&cluster.Postgresql.Spec)
	assert.NoError(t, err)

	cmp := cluster.compareStatefulSetWith(desiredSts)
	if cmp.match {
		t.Errorf("%s: match between current and desired statefulsets albeit differences: %#v", testName, cmp)
	}

	// now sync statefulset - the diff should trigger a replacement
	cluster.syncStatefulSet()

	// compare them again
	_, err = cluster.KubeClient.StatefulSets(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
	assert.NoError(t, err)

	cmp = cluster.compareStatefulSetWith(desiredSts)
	if !cmp.match {
		t.Errorf("%s: current and desired statefulsets are not matching %#v", testName, cmp)
	}
}
