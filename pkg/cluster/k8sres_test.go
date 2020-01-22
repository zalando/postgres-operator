package cluster

import (
	"errors"
	"fmt"
	"reflect"

	v1 "k8s.io/api/core/v1"

	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	appsv1 "k8s.io/api/apps/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func toIntStr(val int) *intstr.IntOrString {
	b := intstr.FromInt(val)
	return &b
}

func TestGenerateSpiloJSONConfiguration(t *testing.T) {
	var cluster = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	testName := "TestGenerateSpiloConfig"
	tests := []struct {
		subtest  string
		pgParam  *acidv1.PostgresqlParam
		patroni  *acidv1.Patroni
		role     string
		opConfig config.Config
		result   string
	}{
		{
			subtest:  "Patroni default configuration",
			pgParam:  &acidv1.PostgresqlParam{PgVersion: "9.6"},
			patroni:  &acidv1.Patroni{},
			role:     "zalandos",
			opConfig: config.Config{},
			result:   `{"postgresql":{"bin_dir":"/usr/lib/postgresql/9.6/bin"},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"}],"users":{"zalandos":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{}}}`,
		},
		{
			subtest: "Patroni configured",
			pgParam: &acidv1.PostgresqlParam{PgVersion: "11"},
			patroni: &acidv1.Patroni{
				InitDB: map[string]string{
					"encoding":       "UTF8",
					"locale":         "en_US.UTF-8",
					"data-checksums": "true",
				},
				PgHba:                []string{"hostssl all all 0.0.0.0/0 md5", "host    all all 0.0.0.0/0 md5"},
				TTL:                  30,
				LoopWait:             10,
				RetryTimeout:         10,
				MaximumLagOnFailover: 33554432,
				Slots:                map[string]map[string]string{"permanent_logical_1": {"type": "logical", "database": "foo", "plugin": "pgoutput"}},
			},
			role:     "zalandos",
			opConfig: config.Config{},
			result:   `{"postgresql":{"bin_dir":"/usr/lib/postgresql/11/bin","pg_hba":["hostssl all all 0.0.0.0/0 md5","host    all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"zalandos":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"slots":{"permanent_logical_1":{"database":"foo","plugin":"pgoutput","type":"logical"}}}}}`,
		},
	}
	for _, tt := range tests {
		cluster.OpConfig = tt.opConfig
		result, err := generateSpiloJSONConfiguration(tt.pgParam, tt.patroni, tt.role, logger)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if tt.result != result {
			t.Errorf("%s %s: Spilo Config is %v, expected %v for role %#v and param %#v",
				testName, tt.subtest, result, tt.result, tt.role, tt.pgParam)
		}
	}
}

func TestCreateLoadBalancerLogic(t *testing.T) {
	var cluster = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	testName := "TestCreateLoadBalancerLogic"
	tests := []struct {
		subtest  string
		role     PostgresRole
		spec     *acidv1.PostgresSpec
		opConfig config.Config
		result   bool
	}{
		{
			subtest:  "new format, load balancer is enabled for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: util.True()},
			opConfig: config.Config{},
			result:   true,
		},
		{
			subtest:  "new format, load balancer is disabled for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: util.False()},
			opConfig: config.Config{},
			result:   false,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: true},
			result:   true,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: false},
			result:   false,
		},
	}
	for _, tt := range tests {
		cluster.OpConfig = tt.opConfig
		result := cluster.shouldCreateLoadBalancerForService(tt.role, tt.spec)
		if tt.result != result {
			t.Errorf("%s %s: Load balancer is %t, expect %t for role %#v and spec %#v",
				testName, tt.subtest, result, tt.result, tt.role, tt.spec)
		}
	}
}

func TestGeneratePodDisruptionBudget(t *testing.T) {
	tests := []struct {
		c   *Cluster
		out policyv1beta1.PodDisruptionBudget
	}{
		// With multiple instances.
		{
			New(
				Config{OpConfig: config.Config{Resources: config.Resources{ClusterNameLabel: "cluster-name", PodRoleLabel: "spilo-role"}, PDBNameFormat: "postgres-{cluster}-pdb"}},
				k8sutil.KubernetesClient{},
				acidv1.Postgresql{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp-database", Namespace: "myapp"},
					Spec:       acidv1.PostgresSpec{TeamID: "myapp", NumberOfInstances: 3}},
				logger),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: toIntStr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"spilo-role": "master", "cluster-name": "myapp-database"},
					},
				},
			},
		},
		// With zero instances.
		{
			New(
				Config{OpConfig: config.Config{Resources: config.Resources{ClusterNameLabel: "cluster-name", PodRoleLabel: "spilo-role"}, PDBNameFormat: "postgres-{cluster}-pdb"}},
				k8sutil.KubernetesClient{},
				acidv1.Postgresql{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp-database", Namespace: "myapp"},
					Spec:       acidv1.PostgresSpec{TeamID: "myapp", NumberOfInstances: 0}},
				logger),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: toIntStr(0),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"spilo-role": "master", "cluster-name": "myapp-database"},
					},
				},
			},
		},
		// With PodDisruptionBudget disabled.
		{
			New(
				Config{OpConfig: config.Config{Resources: config.Resources{ClusterNameLabel: "cluster-name", PodRoleLabel: "spilo-role"}, PDBNameFormat: "postgres-{cluster}-pdb", EnablePodDisruptionBudget: util.False()}},
				k8sutil.KubernetesClient{},
				acidv1.Postgresql{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp-database", Namespace: "myapp"},
					Spec:       acidv1.PostgresSpec{TeamID: "myapp", NumberOfInstances: 3}},
				logger),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: toIntStr(0),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"spilo-role": "master", "cluster-name": "myapp-database"},
					},
				},
			},
		},
		// With non-default PDBNameFormat and PodDisruptionBudget explicitly enabled.
		{
			New(
				Config{OpConfig: config.Config{Resources: config.Resources{ClusterNameLabel: "cluster-name", PodRoleLabel: "spilo-role"}, PDBNameFormat: "postgres-{cluster}-databass-budget", EnablePodDisruptionBudget: util.True()}},
				k8sutil.KubernetesClient{},
				acidv1.Postgresql{
					ObjectMeta: metav1.ObjectMeta{Name: "myapp-database", Namespace: "myapp"},
					Spec:       acidv1.PostgresSpec{TeamID: "myapp", NumberOfInstances: 3}},
				logger),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-databass-budget",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: toIntStr(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"spilo-role": "master", "cluster-name": "myapp-database"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		result := tt.c.generatePodDisruptionBudget()
		if !reflect.DeepEqual(*result, tt.out) {
			t.Errorf("Expected PodDisruptionBudget: %#v, got %#v", tt.out, *result)
		}
	}
}

func TestShmVolume(t *testing.T) {
	testName := "TestShmVolume"
	tests := []struct {
		subTest string
		podSpec *v1.PodSpec
		shmPos  int
	}{
		{
			subTest: "empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{},
				Containers: []v1.Container{
					{
						VolumeMounts: []v1.VolumeMount{},
					},
				},
			},
			shmPos: 0,
		},
		{
			subTest: "non empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{{}},
				Containers: []v1.Container{
					{
						VolumeMounts: []v1.VolumeMount{
							{},
						},
					},
				},
			},
			shmPos: 1,
		},
	}
	for _, tt := range tests {
		addShmVolume(tt.podSpec)

		volumeName := tt.podSpec.Volumes[tt.shmPos].Name
		volumeMountName := tt.podSpec.Containers[0].VolumeMounts[tt.shmPos].Name

		if volumeName != constants.ShmVolumeName {
			t.Errorf("%s %s: Expected volume %s was not created, have %s instead",
				testName, tt.subTest, constants.ShmVolumeName, volumeName)
		}
		if volumeMountName != constants.ShmVolumeName {
			t.Errorf("%s %s: Expected mount %s was not created, have %s instead",
				testName, tt.subTest, constants.ShmVolumeName, volumeMountName)
		}
	}
}

func TestCloneEnv(t *testing.T) {
	testName := "TestCloneEnv"
	tests := []struct {
		subTest   string
		cloneOpts *acidv1.CloneDescription
		env       v1.EnvVar
		envPos    int
	}{
		{
			subTest: "custom s3 path",
			cloneOpts: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				S3WalPath:    "s3://some/path/",
				EndTimestamp: "somewhen",
			},
			env: v1.EnvVar{
				Name:  "CLONE_WALE_S3_PREFIX",
				Value: "s3://some/path/",
			},
			envPos: 1,
		},
		{
			subTest: "generated s3 path, bucket",
			cloneOpts: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				EndTimestamp: "somewhen",
				UID:          "0000",
			},
			env: v1.EnvVar{
				Name:  "CLONE_WAL_S3_BUCKET",
				Value: "wale-bucket",
			},
			envPos: 1,
		},
		{
			subTest: "generated s3 path, target time",
			cloneOpts: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				EndTimestamp: "somewhen",
				UID:          "0000",
			},
			env: v1.EnvVar{
				Name:  "CLONE_TARGET_TIME",
				Value: "somewhen",
			},
			envPos: 4,
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				WALES3Bucket:   "wale-bucket",
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	for _, tt := range tests {
		envs := cluster.generateCloneEnvironment(tt.cloneOpts)

		env := envs[tt.envPos]

		if env.Name != tt.env.Name {
			t.Errorf("%s %s: Expected env name %s, have %s instead",
				testName, tt.subTest, tt.env.Name, env.Name)
		}

		if env.Value != tt.env.Value {
			t.Errorf("%s %s: Expected env value %s, have %s instead",
				testName, tt.subTest, tt.env.Value, env.Value)
		}
	}
}

func TestSecretVolume(t *testing.T) {
	testName := "TestSecretVolume"
	tests := []struct {
		subTest   string
		podSpec   *v1.PodSpec
		secretPos int
	}{
		{
			subTest: "empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{},
				Containers: []v1.Container{
					{
						VolumeMounts: []v1.VolumeMount{},
					},
				},
			},
			secretPos: 0,
		},
		{
			subTest: "non empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{{}},
				Containers: []v1.Container{
					{
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "data",
								ReadOnly:  false,
								MountPath: "/data",
							},
						},
					},
				},
			},
			secretPos: 1,
		},
	}
	for _, tt := range tests {
		additionalSecretMount := "aws-iam-s3-role"
		additionalSecretMountPath := "/meta/credentials"

		numMounts := len(tt.podSpec.Containers[0].VolumeMounts)

		addSecretVolume(tt.podSpec, additionalSecretMount, additionalSecretMountPath)

		volumeName := tt.podSpec.Volumes[tt.secretPos].Name

		if volumeName != additionalSecretMount {
			t.Errorf("%s %s: Expected volume %s was not created, have %s instead",
				testName, tt.subTest, additionalSecretMount, volumeName)
		}

		for i := range tt.podSpec.Containers {
			volumeMountName := tt.podSpec.Containers[i].VolumeMounts[tt.secretPos].Name

			if volumeMountName != additionalSecretMount {
				t.Errorf("%s %s: Expected mount %s was not created, have %s instead",
					testName, tt.subTest, additionalSecretMount, volumeMountName)
			}
		}

		numMountsCheck := len(tt.podSpec.Containers[0].VolumeMounts)

		if numMountsCheck != numMounts+1 {
			t.Errorf("Unexpected number of VolumeMounts: got %v instead of %v",
				numMountsCheck, numMounts+1)
		}
	}
}

func testResources(cluster *Cluster, podSpec *v1.PodTemplateSpec) error {
	cpuReq := podSpec.Spec.Containers[0].Resources.Requests["cpu"]
	if cpuReq.String() != cluster.OpConfig.ConnectionPool.ConnPoolDefaultCPURequest {
		return fmt.Errorf("CPU request doesn't match, got %s, expected %s",
			cpuReq.String(), cluster.OpConfig.ConnectionPool.ConnPoolDefaultCPURequest)
	}

	memReq := podSpec.Spec.Containers[0].Resources.Requests["memory"]
	if memReq.String() != cluster.OpConfig.ConnectionPool.ConnPoolDefaultMemoryRequest {
		return fmt.Errorf("Memory request doesn't match, got %s, expected %s",
			memReq.String(), cluster.OpConfig.ConnectionPool.ConnPoolDefaultMemoryRequest)
	}

	cpuLim := podSpec.Spec.Containers[0].Resources.Limits["cpu"]
	if cpuLim.String() != cluster.OpConfig.ConnectionPool.ConnPoolDefaultCPULimit {
		return fmt.Errorf("CPU limit doesn't match, got %s, expected %s",
			cpuLim.String(), cluster.OpConfig.ConnectionPool.ConnPoolDefaultCPULimit)
	}

	memLim := podSpec.Spec.Containers[0].Resources.Limits["memory"]
	if memLim.String() != cluster.OpConfig.ConnectionPool.ConnPoolDefaultMemoryLimit {
		return fmt.Errorf("Memory limit doesn't match, got %s, expected %s",
			memLim.String(), cluster.OpConfig.ConnectionPool.ConnPoolDefaultMemoryLimit)
	}

	return nil
}

func testLabels(cluster *Cluster, podSpec *v1.PodTemplateSpec) error {
	poolLabels := podSpec.ObjectMeta.Labels["connection-pool"]

	if poolLabels != cluster.connPoolLabelsSelector().MatchLabels["connection-pool"] {
		return fmt.Errorf("Pod labels do not match, got %+v, expected %+v",
			podSpec.ObjectMeta.Labels, cluster.connPoolLabelsSelector().MatchLabels)
	}

	return nil
}

func testEnvs(cluster *Cluster, podSpec *v1.PodTemplateSpec) error {
	required := map[string]bool{
		"PGHOST":               false,
		"PGPORT":               false,
		"PGUSER":               false,
		"PGSCHEMA":             false,
		"PGPASSWORD":           false,
		"CONNECTION_POOL_MODE": false,
		"CONNECTION_POOL_PORT": false,
	}

	envs := podSpec.Spec.Containers[0].Env
	for _, env := range envs {
		required[env.Name] = true
	}

	for env, value := range required {
		if !value {
			return fmt.Errorf("Environment variable %s is not present", env)
		}
	}

	return nil
}

func testCustomPodTemplate(cluster *Cluster, podSpec *v1.PodTemplateSpec) error {
	if podSpec.ObjectMeta.Name != "test-pod-template" {
		return fmt.Errorf("Custom pod template is not used, current spec %+v",
			podSpec)
	}

	return nil
}

func TestConnPoolPodSpec(t *testing.T) {
	testName := "Test connection pool pod template generation"
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	var clusterNoDefaultRes = New(
		Config{
			OpConfig: config.Config{
				ProtectedRoles: []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
				ConnectionPool: config.ConnectionPool{},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	noCheck := func(cluster *Cluster, podSpec *v1.PodTemplateSpec) error { return nil }

	tests := []struct {
		subTest  string
		spec     *acidv1.PostgresSpec
		expected error
		cluster  *Cluster
		check    func(cluster *Cluster, podSpec *v1.PodTemplateSpec) error
	}{
		{
			subTest: "default configuration",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    noCheck,
		},
		{
			subTest: "no default resources",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: errors.New(`could not generate resource requirements: could not fill resource requests: could not parse default CPU quantity: quantities must match the regular expression '^([+-]?[0-9.]+)([eEinumkKMGTP]*[-+]?[0-9]*)$'`),
			cluster:  clusterNoDefaultRes,
			check:    noCheck,
		},
		{
			subTest: "default resources are set",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testResources,
		},
		{
			subTest: "labels for service",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testLabels,
		},
		{
			subTest: "required envs",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testEnvs,
		},
		{
			subTest: "custom pod template",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{
					PodTemplate: &v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: "test-pod-template",
						},
					},
				},
			},
			expected: nil,
			cluster:  cluster,
			check:    testCustomPodTemplate,
		},
	}
	for _, tt := range tests {
		podSpec, err := tt.cluster.generateConnPoolPodTemplate(tt.spec)

		if err != tt.expected && err.Error() != tt.expected.Error() {
			t.Errorf("%s [%s]: Could not generate pod template,\n %+v, expected\n %+v",
				testName, tt.subTest, err, tt.expected)
		}

		err = tt.check(cluster, podSpec)
		if err != nil {
			t.Errorf("%s [%s]: Pod spec is incorrect, %+v",
				testName, tt.subTest, err)
		}
	}
}

func testDeploymentOwnwerReference(cluster *Cluster, deployment *appsv1.Deployment) error {
	owner := deployment.ObjectMeta.OwnerReferences[0]

	if owner.Name != cluster.Statefulset.ObjectMeta.Name {
		return fmt.Errorf("Ownere reference is incorrect, got %s, expected %s",
			owner.Name, cluster.Statefulset.ObjectMeta.Name)
	}

	return nil
}

func testSelector(cluster *Cluster, deployment *appsv1.Deployment) error {
	labels := deployment.Spec.Selector.MatchLabels
	expected := cluster.connPoolLabelsSelector().MatchLabels

	if labels["connection-pool"] != expected["connection-pool"] {
		return fmt.Errorf("Labels are incorrect, got %+v, expected %+v",
			labels, expected)
	}

	return nil
}

func TestConnPoolDeploymentSpec(t *testing.T) {
	testName := "Test connection pool deployment spec generation"
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)
	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
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
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    noCheck,
		},
		{
			subTest: "owner reference",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testDeploymentOwnwerReference,
		},
		{
			subTest: "selector",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			expected: nil,
			cluster:  cluster,
			check:    testSelector,
		},
	}
	for _, tt := range tests {
		deployment, err := tt.cluster.generateConnPoolDeployment(tt.spec)

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

func testServiceOwnwerReference(cluster *Cluster, service *v1.Service) error {
	owner := service.ObjectMeta.OwnerReferences[0]

	if owner.Name != cluster.Statefulset.ObjectMeta.Name {
		return fmt.Errorf("Ownere reference is incorrect, got %s, expected %s",
			owner.Name, cluster.Statefulset.ObjectMeta.Name)
	}

	return nil
}

func testServiceSelector(cluster *Cluster, service *v1.Service) error {
	selector := service.Spec.Selector

	if selector["connection-pool"] != cluster.connPoolName() {
		return fmt.Errorf("Selector is incorrect, got %s, expected %s",
			selector["connection-pool"], cluster.connPoolName())
	}

	return nil
}

func TestConnPoolServiceSpec(t *testing.T) {
	testName := "Test connection pool service spec generation"
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)
	cluster.Statefulset = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-sts",
		},
	}

	noCheck := func(cluster *Cluster, deployment *v1.Service) error {
		return nil
	}

	tests := []struct {
		subTest string
		spec    *acidv1.PostgresSpec
		cluster *Cluster
		check   func(cluster *Cluster, deployment *v1.Service) error
	}{
		{
			subTest: "default configuration",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			cluster: cluster,
			check:   noCheck,
		},
		{
			subTest: "owner reference",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			cluster: cluster,
			check:   testServiceOwnwerReference,
		},
		{
			subTest: "selector",
			spec: &acidv1.PostgresSpec{
				ConnectionPool: &acidv1.ConnectionPool{},
			},
			cluster: cluster,
			check:   testServiceSelector,
		},
	}
	for _, tt := range tests {
		service := tt.cluster.generateConnPoolService(tt.spec)

		if err := tt.check(cluster, service); err != nil {
			t.Errorf("%s [%s]: Service spec is incorrect, %+v",
				testName, tt.subTest, err)
		}
	}
}
