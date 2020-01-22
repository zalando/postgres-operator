package cluster

import (
	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mockInstallLookupFunction(schema string, user string) error {
	return nil
}

func TestConnPoolCreationAndDeletion(t *testing.T) {
	testName := "Test connection pool creation"
	var cluster = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
				ConnectionPool: config.ConnectionPool{
					ConnPoolDefaultCPURequest:    "100m",
					ConnPoolDefaultCPULimit:      "100m",
					ConnPoolDefaultMemoryRequest: "100M",
					ConnPoolDefaultMemoryLimit:   "100M",
				},
			},
		}, k8sutil.NewMockKubernetesClient(), acidv1.Postgresql{}, logger)

	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

	cluster.Spec = acidv1.PostgresSpec{
		ConnectionPool: &acidv1.ConnectionPool{},
	}
	poolResources, err := cluster.createConnectionPool(mockInstallLookupFunction)

	if err != nil {
		t.Errorf("%s: Cannot create connection pool, %s, %+v",
			testName, err, poolResources)
	}

	if poolResources.Deployment == nil {
		t.Errorf("%s: Connection pool deployment is empty", testName)
	}

	if poolResources.Service == nil {
		t.Errorf("%s: Connection pool service is empty", testName)
	}

	err = cluster.deleteConnectionPool()
	if err != nil {
		t.Errorf("%s: Cannot delete connection pool, %s", testName, err)
	}
}
