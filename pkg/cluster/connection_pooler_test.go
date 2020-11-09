package cluster

import (
	"errors"
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

func mockInstallLookupFunction(schema string, user string, role PostgresRole) error {
	return nil
}

func boolToPointer(value bool) *bool {
	return &value
}

func int32ToPointer(value int32) *int32 {
	return &value
}

func TestConnectionPoolerCreationAndDeletion(t *testing.T) {
	testName := "Test connection pooler creation"
	var cluster = New(
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
		}, k8sutil.NewMockKubernetesClient(), acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

	cluster.Spec = acidv1.PostgresSpec{
		ConnectionPooler:              &acidv1.ConnectionPooler{},
		EnableReplicaConnectionPooler: boolToPointer(true),
	}

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

func TestNeedConnectionPooler(t *testing.T) {
	testName := "Test how connection pooler can be enabled"
	var cluster = New(
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
				},
			},
		}, k8sutil.NewMockKubernetesClient(), acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Spec = acidv1.PostgresSpec{
		ConnectionPooler: &acidv1.ConnectionPooler{},
	}

	if !needMasterConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Connection pooler is not enabled with full definition",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableConnectionPooler: boolToPointer(true),
	}

	if !needMasterConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Connection pooler is not enabled with flag",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableConnectionPooler: boolToPointer(false),
		ConnectionPooler:       &acidv1.ConnectionPooler{},
	}

	if needMasterConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Connection pooler is still enabled with flag being false",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableConnectionPooler: boolToPointer(true),
		ConnectionPooler:       &acidv1.ConnectionPooler{},
	}

	if !needMasterConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Connection pooler is not enabled with flag and full",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableConnectionPooler:        boolToPointer(false),
		EnableReplicaConnectionPooler: boolToPointer(false),
		ConnectionPooler:              nil,
	}

	if needMasterConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Connection pooler is enabled with flag false and nil",
			testName)
	}

	// Test for replica connection pooler
	cluster.Spec = acidv1.PostgresSpec{
		ConnectionPooler: &acidv1.ConnectionPooler{},
	}

	if needReplicaConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Replica Connection pooler is not enabled with full definition",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableReplicaConnectionPooler: boolToPointer(true),
	}

	if !needReplicaConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Replica Connection pooler is not enabled with flag",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableReplicaConnectionPooler: boolToPointer(false),
		ConnectionPooler:              &acidv1.ConnectionPooler{},
	}

	if needReplicaConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Replica Connection pooler is still enabled with flag being false",
			testName)
	}

	cluster.Spec = acidv1.PostgresSpec{
		EnableReplicaConnectionPooler: boolToPointer(true),
		ConnectionPooler:              &acidv1.ConnectionPooler{},
	}

	if !needReplicaConnectionPooler(&cluster.Spec) {
		t.Errorf("%s: Replica Connection pooler is not enabled with flag and full",
			testName)
	}
}

func deploymentUpdated(cluster *Cluster, err error, reason SyncReason) error {
	for _, role := range [2]PostgresRole{Master, Replica} {
		if cluster.ConnectionPooler[role] != nil && cluster.ConnectionPooler[role].Deployment != nil &&
			(cluster.ConnectionPooler[role].Deployment.Spec.Replicas == nil ||
				*cluster.ConnectionPooler[role].Deployment.Spec.Replicas != 2) {
			return fmt.Errorf("Wrong number of instances")
		}
	}
	return nil
}

func objectsAreSaved(cluster *Cluster, err error, reason SyncReason) error {
	if cluster.ConnectionPooler == nil {
		return fmt.Errorf("Connection pooler resources are empty")
	}

	for _, role := range []PostgresRole{Master, Replica} {
		if cluster.ConnectionPooler[role].Deployment == nil {
			return fmt.Errorf("Deployment was not saved %s", role)
		}

		if cluster.ConnectionPooler[role].Service == nil {
			return fmt.Errorf("Service was not saved %s", role)
		}
	}

	return nil
}

func MasterobjectsAreSaved(cluster *Cluster, err error, reason SyncReason) error {
	if cluster.ConnectionPooler == nil {
		return fmt.Errorf("Connection pooler resources are empty")
	}

	if cluster.ConnectionPooler[Master].Deployment == nil {
		return fmt.Errorf("Deployment was not saved")
	}

	if cluster.ConnectionPooler[Master].Service == nil {
		return fmt.Errorf("Service was not saved")
	}

	return nil
}

func ReplicaobjectsAreSaved(cluster *Cluster, err error, reason SyncReason) error {
	if cluster.ConnectionPooler == nil {
		return fmt.Errorf("Connection pooler resources are empty")
	}

	if cluster.ConnectionPooler[Replica].Deployment == nil {
		return fmt.Errorf("Deployment was not saved")
	}

	if cluster.ConnectionPooler[Replica].Service == nil {
		return fmt.Errorf("Service was not saved")
	}

	return nil
}

func objectsAreDeleted(cluster *Cluster, err error, reason SyncReason) error {
	for _, role := range [2]PostgresRole{Master, Replica} {
		if cluster.ConnectionPooler[role] != nil &&
			(cluster.ConnectionPooler[role].Deployment != nil || cluster.ConnectionPooler[role].Service != nil) {
			return fmt.Errorf("Connection pooler was not deleted for role %v", role)
		}
	}

	return nil
}

func OnlyMasterDeleted(cluster *Cluster, err error, reason SyncReason) error {

	if cluster.ConnectionPooler[Master] != nil &&
		(cluster.ConnectionPooler[Master].Deployment != nil || cluster.ConnectionPooler[Master].Service != nil) {
		return fmt.Errorf("Connection pooler master was not deleted")
	}
	return nil
}

func OnlyReplicaDeleted(cluster *Cluster, err error, reason SyncReason) error {

	if cluster.ConnectionPooler[Replica] != nil &&
		(cluster.ConnectionPooler[Replica].Deployment != nil || cluster.ConnectionPooler[Replica].Service != nil) {
		return fmt.Errorf("Connection pooler replica was not deleted")
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
	newCluster := func(client k8sutil.KubernetesClient) *Cluster {
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
			}, client, acidv1.Postgresql{}, logger, eventRecorder)
	}
	cluster := newCluster(k8sutil.KubernetesClient{})

	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

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
			subTest: "create from scratch",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler: &acidv1.ConnectionPooler{},
				},
			},
			cluster:          newCluster(k8sutil.ClientMissingObjects()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            MasterobjectsAreSaved,
		},
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
			cluster:          newCluster(k8sutil.ClientMissingObjects()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            MasterobjectsAreSaved,
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
			cluster:          newCluster(k8sutil.ClientMissingObjects()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            MasterobjectsAreSaved,
		},
		{
			subTest: "create no replica with flag",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					EnableReplicaConnectionPooler: boolToPointer(false),
				},
			},
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreDeleted,
		},
		{
			subTest: "create replica if doesn't exist with a flag",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					ConnectionPooler:              &acidv1.ConnectionPooler{},
					EnableReplicaConnectionPooler: boolToPointer(true),
				},
			},
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            ReplicaobjectsAreSaved,
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
			cluster:          newCluster(k8sutil.ClientMissingObjects()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreSaved,
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            OnlyReplicaDeleted,
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            OnlyMasterDeleted,
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            objectsAreDeleted,
		},
		{
			subTest: "cleanup if still there",
			oldSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			newSpec: &acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{},
			},
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
			defaultImage:     "pooler:1.0",
			defaultInstances: 1,
			check:            deploymentUpdated,
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
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
			cluster:          newCluster(k8sutil.NewMockKubernetesClient()),
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

func TestConnectionPoolerPodSpec(t *testing.T) {
	testName := "Test connection pooler pod template generation"
	var cluster = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
				ConnectionPooler: config.ConnectionPooler{
					MaxDBConnections:                     int32ToPointer(60),
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Spec = acidv1.PostgresSpec{
		ConnectionPooler:              &acidv1.ConnectionPooler{},
		EnableReplicaConnectionPooler: boolToPointer(true),
	}
	var clusterNoDefaultRes = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
				ConnectionPooler: config.ConnectionPooler{},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	clusterNoDefaultRes.Spec = acidv1.PostgresSpec{
		ConnectionPooler:              &acidv1.ConnectionPooler{},
		EnableReplicaConnectionPooler: boolToPointer(true),
	}

	noCheck := func(cluster *Cluster, podSpec *v1.PodTemplateSpec, role PostgresRole) error { return nil }

	tests := []struct {
		subTest  string
		spec     *acidv1.PostgresSpec
		expected error
		cluster  *Cluster
		check    func(cluster *Cluster, podSpec *v1.PodTemplateSpec, role PostgresRole) error
	}{
		{
			subTest: "default configuration",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			expected: nil,
			cluster:  cluster,
			check:    noCheck,
		},
		{
			subTest: "no default resources",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			expected: errors.New(`could not generate resource requirements: could not fill resource requests: could not parse default CPU quantity: quantities must match the regular expression '^([+-]?[0-9.]+)([eEinumkKMGTP]*[-+]?[0-9]*)$'`),
			cluster:  clusterNoDefaultRes,
			check:    noCheck,
		},
		{
			subTest: "default resources are set",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testResources,
		},
		{
			subTest: "labels for service",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler:              &acidv1.ConnectionPooler{},
				EnableReplicaConnectionPooler: boolToPointer(true),
			},
			expected: nil,
			cluster:  cluster,
			check:    testLabels,
		},
		{
			subTest: "required envs",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testEnvs,
		},
	}
	for _, role := range [2]PostgresRole{Master, Replica} {
		for _, tt := range tests {
			podSpec, err := tt.cluster.generateConnectionPoolerPodTemplate(role)

			if err != tt.expected && err.Error() != tt.expected.Error() {
				t.Errorf("%s [%s]: Could not generate pod template,\n %+v, expected\n %+v",
					testName, tt.subTest, err, tt.expected)
			}

			err = tt.check(cluster, podSpec, role)
			if err != nil {
				t.Errorf("%s [%s]: Pod spec is incorrect, %+v",
					testName, tt.subTest, err)
			}
		}
	}
}

func TestConnectionPoolerDeploymentSpec(t *testing.T) {
	testName := "Test connection pooler deployment spec generation"
	var cluster = New(
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
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)
	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}
	cluster.ConnectionPooler = map[PostgresRole]*ConnectionPoolerObjects{
		Master: {
			Deployment:     nil,
			Service:        nil,
			LookupFunction: true,
			Name:           "",
			Role:           Master,
		},
	}

	noCheck := func(cluster *Cluster, deployment *appsv1.Deployment) error {
		return nil
	}

	tests := []struct {
		subTest  string
		spec     *acidv1.PostgresSpec
		expected error
		cluster  *Cluster
		check    func(cluster *Cluster, deployment *appsv1.Deployment) error
	}{
		{
			subTest: "default configuration",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler:              &acidv1.ConnectionPooler{},
				EnableReplicaConnectionPooler: boolToPointer(true),
			},
			expected: nil,
			cluster:  cluster,
			check:    noCheck,
		},
		{
			subTest: "owner reference",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler:              &acidv1.ConnectionPooler{},
				EnableReplicaConnectionPooler: boolToPointer(true),
			},
			expected: nil,
			cluster:  cluster,
			check:    testDeploymentOwnwerReference,
		},
		{
			subTest: "selector",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler:              &acidv1.ConnectionPooler{},
				EnableReplicaConnectionPooler: boolToPointer(true),
			},
			expected: nil,
			cluster:  cluster,
			check:    testSelector,
		},
	}
	for _, tt := range tests {
		deployment, err := tt.cluster.generateConnectionPoolerDeployment(cluster.ConnectionPooler[Master])

		if err != tt.expected && err.Error() != tt.expected.Error() {
			t.Errorf("%s [%s]: Could not generate deployment spec,\n %+v, expected\n %+v",
				testName, tt.subTest, err, tt.expected)
		}

		err = tt.check(cluster, deployment)
		if err != nil {
			t.Errorf("%s [%s]: Deployment spec is incorrect, %+v",
				testName, tt.subTest, err)
		}
	}
}

func testResources(cluster *Cluster, podSpec *v1.PodTemplateSpec, role PostgresRole) error {
	cpuReq := podSpec.Spec.Containers[0].Resources.Requests["cpu"]
	if cpuReq.String() != cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultCPURequest {
		return fmt.Errorf("CPU request doesn't match, got %s, expected %s",
			cpuReq.String(), cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultCPURequest)
	}

	memReq := podSpec.Spec.Containers[0].Resources.Requests["memory"]
	if memReq.String() != cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultMemoryRequest {
		return fmt.Errorf("Memory request doesn't match, got %s, expected %s",
			memReq.String(), cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultMemoryRequest)
	}

	cpuLim := podSpec.Spec.Containers[0].Resources.Limits["cpu"]
	if cpuLim.String() != cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultCPULimit {
		return fmt.Errorf("CPU limit doesn't match, got %s, expected %s",
			cpuLim.String(), cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultCPULimit)
	}

	memLim := podSpec.Spec.Containers[0].Resources.Limits["memory"]
	if memLim.String() != cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultMemoryLimit {
		return fmt.Errorf("Memory limit doesn't match, got %s, expected %s",
			memLim.String(), cluster.OpConfig.ConnectionPooler.ConnectionPoolerDefaultMemoryLimit)
	}

	return nil
}

func testLabels(cluster *Cluster, podSpec *v1.PodTemplateSpec, role PostgresRole) error {
	poolerLabels := podSpec.ObjectMeta.Labels["connection-pooler"]

	if poolerLabels != cluster.connectionPoolerLabelsSelector(role).MatchLabels["connection-pooler"] {
		return fmt.Errorf("Pod labels do not match, got %+v, expected %+v",
			podSpec.ObjectMeta.Labels, cluster.connectionPoolerLabelsSelector(role).MatchLabels)
	}

	return nil
}

func testSelector(cluster *Cluster, deployment *appsv1.Deployment) error {
	labels := deployment.Spec.Selector.MatchLabels
	expected := cluster.connectionPoolerLabelsSelector(Master).MatchLabels

	if labels["connection-pooler"] != expected["connection-pooler"] {
		return fmt.Errorf("Labels are incorrect, got %+v, expected %+v",
			labels, expected)
	}

	return nil
}

func testServiceSelector(cluster *Cluster, service *v1.Service, role PostgresRole) error {
	selector := service.Spec.Selector

	if selector["connection-pooler"] != cluster.connectionPoolerName(role) {
		return fmt.Errorf("Selector is incorrect, got %s, expected %s",
			selector["connection-pooler"], cluster.connectionPoolerName(role))
	}

	return nil
}

func TestConnectionPoolerServiceSpec(t *testing.T) {
	testName := "Test connection pooler service spec generation"
	var cluster = New(
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
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)
	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}
	cluster.ConnectionPooler = map[PostgresRole]*ConnectionPoolerObjects{
		Master: {
			Deployment:     nil,
			Service:        nil,
			LookupFunction: false,
			Role:           Master,
		},
		Replica: {
			Deployment:     nil,
			Service:        nil,
			LookupFunction: false,
			Role:           Replica,
		},
	}

	noCheck := func(cluster *Cluster, deployment *v1.Service, role PostgresRole) error {
		return nil
	}

	tests := []struct {
		subTest string
		spec    *acidv1.PostgresSpec
		cluster *Cluster
		check   func(cluster *Cluster, deployment *v1.Service, role PostgresRole) error
	}{
		{
			subTest: "default configuration",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			cluster: cluster,
			check:   noCheck,
		},
		{
			subTest: "owner reference",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler: &acidv1.ConnectionPooler{},
			},
			cluster: cluster,
			check:   testServiceOwnwerReference,
		},
		{
			subTest: "selector",
			spec: &acidv1.PostgresSpec{
				ConnectionPooler:              &acidv1.ConnectionPooler{},
				EnableReplicaConnectionPooler: boolToPointer(true),
			},
			cluster: cluster,
			check:   testServiceSelector,
		},
	}
	for _, role := range [2]PostgresRole{Master, Replica} {
		for _, tt := range tests {
			service := tt.cluster.generateConnectionPoolerService(tt.cluster.ConnectionPooler[role])

			if err := tt.check(cluster, service, role); err != nil {
				t.Errorf("%s [%s]: Service spec is incorrect, %+v",
					testName, tt.subTest, err)
			}
		}
	}
}
