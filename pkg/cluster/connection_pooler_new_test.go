package cluster

import (
	"testing"

	"context"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/labels"

	"k8s.io/client-go/kubernetes/fake"
)

func TestFakeClient(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	namespace := "default"

	l := labels.Set(map[string]string{
		"application": "spilo",
	})

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-deployment1",
			Namespace: namespace,
			Labels:    l,
		},
	}

	clientSet.AppsV1().Deployments(namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})

	deployment2, _ := clientSet.AppsV1().Deployments(namespace).Get(context.TODO(), "my-deployment1", metav1.GetOptions{})

	if deployment.ObjectMeta.Name != deployment2.ObjectMeta.Name {
		t.Errorf("Deployments are not equal")
	}

	deployments, _ := clientSet.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: "application=spilo"})

	if len(deployments.Items) != 1 {
		t.Errorf("Label search does not work")
	}
}

func TestConnectionPoolerCreateDel(t *testing.T) {

	testName := "create-delete-cp"
	clientSet := fake.NewSimpleClientset()
	acidClientSet := fakeacidv1.NewSimpleClientset()
	namespace := "default"

	client := k8sutil.KubernetesClient{
		StatefulSetsGetter: clientSet.AppsV1(),
		ServicesGetter:     clientSet.CoreV1(),
		DeploymentsGetter:  clientSet.AppsV1(),
		PostgresqlsGetter:  acidClientSet.AcidV1(),
		SecretsGetter:      clientSet.CoreV1(),
	}

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acid-fake-cluster",
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler:        boolToPointer(true),
			EnableReplicaConnectionPooler: boolToPointer(true),
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				ConnectionPooler: config.ConnectionPooler{
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:                    int32ToPointer(1),
				},
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					DefaultCPURequest:    "300m",
					DefaultCPULimit:      "300m",
					DefaultMemoryRequest: "300Mi",
					DefaultMemoryLimit:   "300Mi",
					PodRoleLabel:         "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	_, err := cluster.createService(Master)
	assert.NoError(t, err)
	_, err = cluster.createStatefulSet()
	assert.NoError(t, err)

	reason, err := cluster.createConnectionPooler(mockInstallLookupFunction)

	if err != nil {
		t.Errorf("%s: Cannot create connection pooler, %s, %+v",
			testName, err, reason)
	}
	for _, role := range [2]PostgresRole{Master, Replica} {
		if cluster.ConnectionPooler[role] != nil {
			if cluster.ConnectionPooler[role].Deployment == nil {
				t.Errorf("%s: Connection pooler deployment is empty for role %s", testName, role)
			}

			if cluster.ConnectionPooler[role].Service == nil {
				t.Errorf("%s: Connection pooler service is empty for role %s", testName, role)
			}
		}
	}

	oldSpec := &acidv1.Postgresql{
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler:        boolToPointer(true),
			EnableReplicaConnectionPooler: boolToPointer(true),
		},
	}
	newSpec := &acidv1.Postgresql{
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler:        boolToPointer(false),
			EnableReplicaConnectionPooler: boolToPointer(false),
		},
	}

	// Delete connection pooler via sync
	_, err = cluster.syncConnectionPooler(oldSpec, newSpec, mockInstallLookupFunction)
	if err != nil {
		t.Errorf("%s: Cannot sync connection pooler, %s", testName, err)
	}

	for _, role := range [2]PostgresRole{Master, Replica} {
		err = cluster.deleteConnectionPooler(role)
		if err != nil {
			t.Errorf("%s: Cannot delete connection pooler, %s", testName, err)
		}
	}

}
