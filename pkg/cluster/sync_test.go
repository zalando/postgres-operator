package cluster

import (
	"fmt"
	"strings"
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

func deploymentUpdated(cluster *Cluster, err error, reason SyncReason) error {
	for _, role := range cluster.RolesConnectionPooler() {
		if cluster.ConnectionPooler.Deployment[role] != nil &&
			(cluster.ConnectionPooler.Deployment[role].Spec.Replicas == nil ||
				*cluster.ConnectionPooler.Deployment[role].Spec.Replicas != 2) {
			return fmt.Errorf("Wrong number of instances")
		}
	}
	return nil
}

func objectsAreSaved(cluster *Cluster, err error, reason SyncReason) error {
	if cluster.ConnectionPooler == nil {
		return fmt.Errorf("Connection pooler resources are empty")
	}

	for _, role := range cluster.RolesConnectionPooler() {
		if role != "" {
			if cluster.ConnectionPooler.Deployment[role] == nil {
				return fmt.Errorf("Deployment was not saved %s", role)
			}

			if cluster.ConnectionPooler.Service[role] == nil {
				return fmt.Errorf("Service was not saved %s", role)
			}
		}
	}

	return nil
}

func objectsAreDeleted(cluster *Cluster, err error, reason SyncReason) error {
	if cluster.ConnectionPooler != nil {
		return fmt.Errorf("Connection pooler was not deleted")
	}

	return nil
}

func OnlyMasterDeleted(cluster *Cluster, err error, reason SyncReason) error {

	for _, role := range cluster.RolesConnectionPooler() {
		if cluster.ConnectionPooler != nil &&
			(cluster.ConnectionPooler.Deployment[role] != nil && cluster.ConnectionPooler.Service[role] != nil) {
			return fmt.Errorf("Connection pooler master was not deleted")
		}
	}
	return nil
}

func OnlyReplicaDeleted(cluster *Cluster, err error, reason SyncReason) error {

	for _, role := range cluster.RolesConnectionPooler() {
		if cluster.ConnectionPooler != nil &&
			(cluster.ConnectionPooler.Deployment[role] != nil && cluster.ConnectionPooler.Service[role] != nil) {
			return fmt.Errorf("Connection pooler replica was not deleted")
		}
	}
	return nil
}

func noEmptySync(cluster *Cluster, err error, reason SyncReason) error {
	for _, msg := range reason {
		if strings.HasPrefix(msg, "update [] from '<nil>' to '") {
			return fmt.Errorf("There is an empty reason, %s", msg)
		}
	}

	return nil
}

func TestConnectionPoolerSynchronization(t *testing.T) {
	testName := "Test connection pooler synchronization"
	newCluster := func() *Cluster {
		return New(
			Config{
				OpConfig: config.Config{
					ProtectedRoles: []string{"admin"},
					Auth: config.Auth{
						SuperUsername:       superUserName,
						ReplicationUsername: replicationUserName,
					},
					ConnectionPooler: config.ConnectionPooler{
						ConnectionPoolerDefaultCPURequest:    "100m",
						ConnectionPoolerDefaultCPULimit:      "100m",
						ConnectionPoolerDefaultMemoryRequest: "100Mi",
						ConnectionPoolerDefaultMemoryLimit:   "100Mi",
						NumberOfInstances:                    int32ToPointer(1),
					},
				},
			}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)
	}
	cluster := newCluster()

	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

	clusterMissingObjects := newCluster()
	clusterMissingObjects.KubeClient = k8sutil.ClientMissingObjects()

	clusterMock := newCluster()
	clusterMock.KubeClient = k8sutil.NewMockKubernetesClient()

	clusterDirtyMock := newCluster()
	clusterDirtyMock.KubeClient = k8sutil.NewMockKubernetesClient()
	clusterDirtyMock.ConnectionPooler = &ConnectionPoolerObjects{
		Deployment: make(map[PostgresRole]*appsv1.Deployment),
		Service:    make(map[PostgresRole]*v1.Service),
	}
	clusterDirtyMock.ConnectionPooler.Deployment[Master] = &appsv1.Deployment{}
	clusterDirtyMock.ConnectionPooler.Service[Master] = &v1.Service{}
	clusterReplicaDirtyMock := newCluster()
	clusterReplicaDirtyMock.KubeClient = k8sutil.NewMockKubernetesClient()
	clusterReplicaDirtyMock.ConnectionPooler = &ConnectionPoolerObjects{
		Deployment: make(map[PostgresRole]*appsv1.Deployment),
		Service:    make(map[PostgresRole]*v1.Service),
	}

	clusterDirtyMock.ConnectionPooler.Deployment[Replica] = &appsv1.Deployment{}
	clusterDirtyMock.ConnectionPooler.Service[Replica] = &v1.Service{}
	clusterNewDefaultsMock := newCluster()
	clusterNewDefaultsMock.KubeClient = k8sutil.NewMockKubernetesClient()

	tests := []struct {
		subTest          string
		oldSpec          *acidv1.Postgresql
		newSpec          *acidv1.Postgresql
		cluster          *Cluster
		defaultImage     string
		defaultInstances int32
		check            func(cluster *Cluster, err error, reason SyncReason) error
	}{
		{
			subTest: "create if doesn't exist",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			cluster:          clusterMissingObjects,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
		},
		{
			subTest: "create if doesn't exist with a flag",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableConnectionPooler: boolToPointer(true),
				},
			},
			cluster:          clusterMissingObjects,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
		},
		{
			subTest: "create replica if doesn't exist with a flag",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableReplicaConnectionPooler: boolToPointer(true),
				},
			},
			cluster:          clusterReplicaDirtyMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
		},
		{
			subTest: "create from scratch",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			cluster:          clusterMissingObjects,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
		},
		{
			subTest: "create both master and replica",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler:              &acidv1.ConnectionPooler{},
					EnableReplicaConnectionPooler: boolToPointer(true),
					EnableConnectionPooler:        boolToPointer(true),
				},
			},
			cluster:          clusterMissingObjects,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
		},
		{
			subTest: "delete if not needed",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			cluster:          clusterMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreDeleted,
		},
		{
			subTest: "delete only master if not needed",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler:       &acidv1.ConnectionPooler{},
					EnableConnectionPooler: boolToPointer(true),
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableReplicaConnectionPooler: boolToPointer(true),
				},
			},
			cluster:          clusterMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            OnlyMasterDeleted,
		},
		{
			subTest: "delete only replica if not needed",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler:              &acidv1.ConnectionPooler{},
					EnableReplicaConnectionPooler: boolToPointer(true),
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			cluster:          clusterMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            OnlyReplicaDeleted,
		},
		{
			subTest: "cleanup if still there",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			cluster:          clusterDirtyMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreDeleted,
		},
		{
			subTest: "update deployment",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{
						NumberOfInstances: int32ToPointer(1),
					},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{
						NumberOfInstances: int32ToPointer(2),
					},
				},
			},
			cluster:          clusterMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            deploymentUpdated,
		},
		{
			subTest: "update image from changed defaults",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			cluster:          clusterNewDefaultsMock,
			defaultImage:     "pooler:2.0",
			defaultInstances: 2,
			check:            deploymentUpdated,
		},
		{
			subTest: "there is no sync from nil to an empty spec",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableConnectionPooler: boolToPointer(true),
					ConnectionPooler:       nil,
				},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableConnectionPooler: boolToPointer(true),
					ConnectionPooler:       &acidv1.ConnectionPooler{},
				},
			},
			cluster:          clusterMock,
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            noEmptySync,
		},
	}
	for _, tt := range tests {
		tt.cluster.OpConfig.ConnectionPooler.Image = tt.defaultImage
		tt.cluster.OpConfig.ConnectionPooler.NumberOfInstances =
			int32ToPointer(tt.defaultInstances)

		reason, err := tt.cluster.syncConnectionPooler(tt.oldSpec,
			tt.newSpec, mockInstallLookupFunction)

		if err := tt.check(tt.cluster, err, reason); err != nil {
			t.Errorf("%s [%s]: Could not synchronize, %+v",
				testName, tt.subTest, err)
		}
	}
}
