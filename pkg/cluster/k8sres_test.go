package cluster

import (
	"reflect"

	"testing"

	"github.com/stretchr/testify/assert"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	v1 "k8s.io/api/core/v1"
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

func TestTLS(t *testing.T) {
	var err error
	var spec acidv1.PostgresSpec
	var cluster *Cluster

	makeSpec := func(tls acidv1.TLSDescription) acidv1.PostgresSpec {
		return acidv1.PostgresSpec{
			TeamID: "myapp", NumberOfInstances: 1,
			Resources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			},
			Volume: acidv1.Volume{
				Size: "1G",
			},
			TLS: &tls,
		}
	}

	cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				ProtectedRoles:      []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)
	spec = makeSpec(acidv1.TLSDescription{SecretName: "my-secret", CAFile: "ca.crt"})
	s, err := cluster.generateStatefulSet(&spec)
	if err != nil {
		assert.NoError(t, err)
	}

	fsGroup := int64(103)
	assert.Equal(t, &fsGroup, s.Spec.Template.Spec.SecurityContext.FSGroup, "has a default FSGroup assigned")

	defaultMode := int32(0640)
	volume := v1.Volume{
		Name: "tls-secret",
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName:  "my-secret",
				DefaultMode: &defaultMode,
			},
		},
	}
	assert.Contains(t, s.Spec.Template.Spec.Volumes, volume, "the pod gets a secret volume")

	assert.Contains(t, s.Spec.Template.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
		MountPath: "/tls",
		Name:      "tls-secret",
		ReadOnly:  true,
	}, "the volume gets mounted in /tls")

	assert.Contains(t, s.Spec.Template.Spec.Containers[0].Env, v1.EnvVar{Name: "SSL_CERTIFICATE_FILE", Value: "/tls/tls.crt"})
	assert.Contains(t, s.Spec.Template.Spec.Containers[0].Env, v1.EnvVar{Name: "SSL_PRIVATE_KEY_FILE", Value: "/tls/tls.key"})
	assert.Contains(t, s.Spec.Template.Spec.Containers[0].Env, v1.EnvVar{Name: "SSL_CA_FILE", Value: "/tls/ca.crt"})
}
