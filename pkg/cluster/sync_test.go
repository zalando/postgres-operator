package cluster

import (
	"fmt"
	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func int32ToPointer(value int32) *int32 {
	return &value
}

func deploymentUpdated(cluster *Cluster, err error) error {
	if cluster.ConnectionPool.Deployment.Spec.Replicas == nil ||
		*cluster.ConnectionPool.Deployment.Spec.Replicas != 2 {
		return fmt.Errorf("Wrong nubmer of instances")
	}

	return nil
}

func objectsAreSaved(cluster *Cluster, err error) error {
	if cluster.ConnectionPool == nil {
		return fmt.Errorf("Connection pool resources are empty")
	}

	if cluster.ConnectionPool.Deployment == nil {
		return fmt.Errorf("Deployment was not saved")
	}

	if cluster.ConnectionPool.Service == nil {
		return fmt.Errorf("Service was not saved")
	}

	return nil
}

func objectsAreDeleted(cluster *Cluster, err error) error {
	if cluster.ConnectionPool != nil {
		return fmt.Errorf("Connection pool was not deleted")
	}

	return nil
}

func TestConnPoolSynchronization(t *testing.T) {
	testName := "Test connection pool synchronization"
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
					ConnPoolDefaultMemoryRequest: "100Mi",
					ConnPoolDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:            int32ToPointer(1),
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

	clusterMissingObjects := *cluster
	clusterMissingObjects.KubeClient = k8sutil.ClientMissingObjects()

	clusterMock := *cluster
	clusterMock.KubeClient = k8sutil.NewMockKubernetesClient()

	clusterDirtyMock := *cluster
	clusterDirtyMock.KubeClient = k8sutil.NewMockKubernetesClient()
	clusterDirtyMock.ConnectionPool = &ConnectionPoolObjects{
		Deployment: &appsv1.Deployment{},
		Service:    &v1.Service{},
	}

	clusterNewDefaultsMock := *cluster
	clusterNewDefaultsMock.KubeClient = k8sutil.NewMockKubernetesClient()
	cluster.OpConfig.ConnectionPool.Image = "pooler:2.0"
	cluster.OpConfig.ConnectionPool.NumberOfInstances = int32ToPointer(2)

	tests := []struct {
		subTest string
		oldSpec *acidv1.Postgresql
		newSpec *acidv1.Postgresql
		cluster *Cluster
		check   func(cluster *Cluster, err error) error
	}{
		{
			subTest: "create if doesn't exist",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			cluster: &clusterMissingObjects,
			check:   objectsAreSaved,
		},
		{
			subTest: "create if doesn't exist with a flag",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableConnectionPool: boolToPointer(true),
				},
			},
			cluster: &clusterMissingObjects,
			check:   objectsAreSaved,
		},
		{
			subTest: "create from scratch",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			cluster: &clusterMissingObjects,
			check:   objectsAreSaved,
		},
		{
			subTest: "delete if not needed",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			cluster: &clusterMock,
			check:   objectsAreDeleted,
		},
		{
			subTest: "cleanup if still there",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			cluster: &clusterDirtyMock,
			check:   objectsAreDeleted,
		},
		{
			subTest: "update deployment",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{
						NumberOfInstances: int32ToPointer(1),
					},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{
						NumberOfInstances: int32ToPointer(2),
					},
				},
			},
			cluster: &clusterMock,
			check:   deploymentUpdated,
		},
		{
			subTest: "update image from changed defaults",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPool: &acidv1.ConnectionPool{},
				},
			},
			cluster: &clusterNewDefaultsMock,
			check:   deploymentUpdated,
		},
	}
	for _, tt := range tests {
		err := tt.cluster.syncConnectionPool(tt.oldSpec, tt.newSpec, mockInstallLookupFunction)

		if err := tt.check(tt.cluster, err); err != nil {
			t.Errorf("%s [%s]: Could not synchronize, %+v",
				testName, tt.subTest, err)
		}
	}
}
