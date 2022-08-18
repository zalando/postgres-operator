package cluster

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	"testing"

	"github.com/stretchr/testify/assert"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

func newFakeK8sTestClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	acidClientSet := fakeacidv1.NewSimpleClientset()
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		PodsGetter:         clientSet.CoreV1(),
		PostgresqlsGetter:  acidClientSet.AcidV1(),
		StatefulSetsGetter: clientSet.AppsV1(),
	}, clientSet
}

// For testing purposes
type ExpectedValue struct {
	envIndex       int
	envVarConstant string
	envVarValue    string
	envVarValueRef *v1.EnvVarSource
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

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
				PgHba:                 []string{"hostssl all all 0.0.0.0/0 md5", "host    all all 0.0.0.0/0 md5"},
				TTL:                   30,
				LoopWait:              10,
				RetryTimeout:          10,
				MaximumLagOnFailover:  33554432,
				SynchronousMode:       true,
				SynchronousModeStrict: true,
				SynchronousNodeCount:  1,
				Slots:                 map[string]map[string]string{"permanent_logical_1": {"type": "logical", "database": "foo", "plugin": "pgoutput"}},
			},
			role:     "zalandos",
			opConfig: config.Config{},
			result:   `{"postgresql":{"bin_dir":"/usr/lib/postgresql/11/bin","pg_hba":["hostssl all all 0.0.0.0/0 md5","host    all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"zalandos":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"synchronous_mode":true,"synchronous_mode_strict":true,"synchronous_node_count":1,"slots":{"permanent_logical_1":{"database":"foo","plugin":"pgoutput","type":"logical"}}}}}`,
		},
	}
	for _, tt := range tests {
		cluster.OpConfig = tt.opConfig
		result, err := generateSpiloJSONConfiguration(tt.pgParam, tt.patroni, tt.role, false, logger)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if tt.result != result {
			t.Errorf("%s %s: Spilo Config is %v, expected %v for role %#v and param %#v",
				testName, tt.subtest, result, tt.result, tt.role, tt.pgParam)
		}
	}
}

func TestExtractPgVersionFromBinPath(t *testing.T) {
	testName := "TestExtractPgVersionFromBinPath"
	tests := []struct {
		subTest  string
		binPath  string
		template string
		expected string
	}{
		{
			subTest:  "test current bin path with decimal against hard coded template",
			binPath:  "/usr/lib/postgresql/9.6/bin",
			template: pgBinariesLocationTemplate,
			expected: "9.6",
		},
		{
			subTest:  "test current bin path against hard coded template",
			binPath:  "/usr/lib/postgresql/12/bin",
			template: pgBinariesLocationTemplate,
			expected: "12",
		},
		{
			subTest:  "test alternative bin path against a matching template",
			binPath:  "/usr/pgsql-12/bin",
			template: "/usr/pgsql-%v/bin",
			expected: "12",
		},
	}

	for _, tt := range tests {
		pgVersion, err := extractPgVersionFromBinPath(tt.binPath, tt.template)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if pgVersion != tt.expected {
			t.Errorf("%s %s: Expected version %s, have %s instead",
				testName, tt.subTest, tt.expected, pgVersion)
		}
	}
}

const (
	testPodEnvironmentConfigMapName      = "pod_env_cm"
	testPodEnvironmentSecretName         = "pod_env_sc"
	testPodEnvironmentObjectNotExists    = "idonotexist"
	testPodEnvironmentSecretNameAPIError = "pod_env_sc_apierror"
	testResourceCheckInterval            = 3
	testResourceCheckTimeout             = 10
)

type mockSecret struct {
	v1core.SecretInterface
}

type mockConfigMap struct {
	v1core.ConfigMapInterface
}

func (c *mockSecret) Get(ctx context.Context, name string, options metav1.GetOptions) (*v1.Secret, error) {
	if name == testPodEnvironmentSecretNameAPIError {
		return nil, fmt.Errorf("Secret PodEnvironmentSecret API error")
	}
	if name != testPodEnvironmentSecretName {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Group: "core", Resource: "secret"}, name)
	}
	secret := &v1.Secret{}
	secret.Name = testPodEnvironmentSecretName
	secret.Data = map[string][]byte{
		"clone_aws_access_key_id":                []byte("0123456789abcdef0123456789abcdef"),
		"custom_variable":                        []byte("secret-test"),
		"standby_google_application_credentials": []byte("0123456789abcdef0123456789abcdef"),
	}
	return secret, nil
}

func (c *mockConfigMap) Get(ctx context.Context, name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	if name != testPodEnvironmentConfigMapName {
		return nil, fmt.Errorf("NotFound")
	}
	configmap := &v1.ConfigMap{}
	configmap.Name = testPodEnvironmentConfigMapName
	configmap.Data = map[string]string{
		// hard-coded clone env variable, can set when not specified in manifest
		"clone_aws_endpoint": "s3.eu-west-1.amazonaws.com",
		// custom variable, can be overridden by c.Spec.Env
		"custom_variable": "configmap-test",
		// hard-coded env variable, can not be overridden
		"kubernetes_scope_label": "pgaas",
		// hard-coded env variable, can be overridden
		"wal_s3_bucket": "global-s3-bucket-configmap",
	}
	return configmap, nil
}

type MockSecretGetter struct {
}

type MockConfigMapsGetter struct {
}

func (c *MockSecretGetter) Secrets(namespace string) v1core.SecretInterface {
	return &mockSecret{}
}

func (c *MockConfigMapsGetter) ConfigMaps(namespace string) v1core.ConfigMapInterface {
	return &mockConfigMap{}
}

func newMockKubernetesClient() k8sutil.KubernetesClient {
	return k8sutil.KubernetesClient{
		SecretsGetter:    &MockSecretGetter{},
		ConfigMapsGetter: &MockConfigMapsGetter{},
	}
}
func newMockCluster(opConfig config.Config) *Cluster {
	cluster := &Cluster{
		Config:     Config{OpConfig: opConfig},
		KubeClient: newMockKubernetesClient(),
		logger:     logger,
	}
	return cluster
}

func TestPodEnvironmentConfigMapVariables(t *testing.T) {
	testName := "TestPodEnvironmentConfigMapVariables"
	tests := []struct {
		subTest  string
		opConfig config.Config
		envVars  []v1.EnvVar
		err      error
	}{
		{
			subTest: "no PodEnvironmentConfigMap",
			envVars: []v1.EnvVar{},
		},
		{
			subTest: "missing PodEnvironmentConfigMap",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentObjectNotExists,
					},
				},
			},
			err: fmt.Errorf("could not read PodEnvironmentConfigMap: NotFound"),
		},
		{
			subTest: "Pod environment vars configured by PodEnvironmentConfigMap",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
				},
			},
			envVars: []v1.EnvVar{
				{
					Name:  "clone_aws_endpoint",
					Value: "s3.eu-west-1.amazonaws.com",
				},
				{
					Name:  "custom_variable",
					Value: "configmap-test",
				},
				{
					Name:  "kubernetes_scope_label",
					Value: "pgaas",
				},
				{
					Name:  "wal_s3_bucket",
					Value: "global-s3-bucket-configmap",
				},
			},
		},
	}
	for _, tt := range tests {
		c := newMockCluster(tt.opConfig)
		vars, err := c.getPodEnvironmentConfigMapVariables()
		if !reflect.DeepEqual(vars, tt.envVars) {
			t.Errorf("%s %s: expected `%v` but got `%v`",
				testName, tt.subTest, tt.envVars, vars)
		}
		if tt.err != nil {
			if err.Error() != tt.err.Error() {
				t.Errorf("%s %s: expected error `%v` but got `%v`",
					testName, tt.subTest, tt.err, err)
			}
		} else {
			if err != nil {
				t.Errorf("%s %s: expected no error but got error: `%v`",
					testName, tt.subTest, err)
			}
		}
	}
}

// Test if the keys of an existing secret are properly referenced
func TestPodEnvironmentSecretVariables(t *testing.T) {
	maxRetries := int(testResourceCheckTimeout / testResourceCheckInterval)
	testName := "TestPodEnvironmentSecretVariables"
	tests := []struct {
		subTest  string
		opConfig config.Config
		envVars  []v1.EnvVar
		err      error
	}{
		{
			subTest: "No PodEnvironmentSecret configured",
			envVars: []v1.EnvVar{},
		},
		{
			subTest: "Secret referenced by PodEnvironmentSecret does not exist",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecret:  testPodEnvironmentObjectNotExists,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
			},
			err: fmt.Errorf("could not read Secret PodEnvironmentSecretName: still failing after %d retries: secret.core %q not found", maxRetries, testPodEnvironmentObjectNotExists),
		},
		{
			subTest: "API error during PodEnvironmentSecret retrieval",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecret:  testPodEnvironmentSecretNameAPIError,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
			},
			err: fmt.Errorf("could not read Secret PodEnvironmentSecretName: Secret PodEnvironmentSecret API error"),
		},
		{
			subTest: "Pod environment vars reference all keys from secret configured by PodEnvironmentSecret",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecret:  testPodEnvironmentSecretName,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
			},
			envVars: []v1.EnvVar{
				{
					Name: "clone_aws_access_key_id",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "clone_aws_access_key_id",
						},
					},
				},
				{
					Name: "custom_variable",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "custom_variable",
						},
					},
				},
				{
					Name: "standby_google_application_credentials",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "standby_google_application_credentials",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		c := newMockCluster(tt.opConfig)
		vars, err := c.getPodEnvironmentSecretVariables()
		sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
		if !reflect.DeepEqual(vars, tt.envVars) {
			t.Errorf("%s %s: expected `%v` but got `%v`",
				testName, tt.subTest, tt.envVars, vars)
		}
		if tt.err != nil {
			if err.Error() != tt.err.Error() {
				t.Errorf("%s %s: expected error `%v` but got `%v`",
					testName, tt.subTest, tt.err, err)
			}
		} else {
			if err != nil {
				t.Errorf("%s %s: expected no error but got error: `%v`",
					testName, tt.subTest, err)
			}
		}
	}

}

func testEnvs(cluster *Cluster, podSpec *v1.PodTemplateSpec, role PostgresRole) error {
	required := map[string]bool{
		"PGHOST":                 false,
		"PGPORT":                 false,
		"PGUSER":                 false,
		"PGSCHEMA":               false,
		"PGPASSWORD":             false,
		"CONNECTION_POOLER_MODE": false,
		"CONNECTION_POOLER_PORT": false,
	}

	container := getPostgresContainer(&podSpec.Spec)
	envs := container.Env
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

func TestGenerateSpiloPodEnvVars(t *testing.T) {
	var dummyUUID = "efd12e58-5786-11e8-b5a7-06148230260c"

	expectedClusterNameLabel := []ExpectedValue{
		{
			envIndex:       5,
			envVarConstant: "KUBERNETES_SCOPE_LABEL",
			envVarValue:    "cluster-name",
		},
	}
	expectedSpiloWalPathCompat := []ExpectedValue{
		{
			envIndex:       12,
			envVarConstant: "ENABLE_WAL_PATH_COMPAT",
			envVarValue:    "true",
		},
	}
	expectedValuesS3Bucket := []ExpectedValue{
		{
			envIndex:       15,
			envVarConstant: "WAL_S3_BUCKET",
			envVarValue:    "global-s3-bucket",
		},
		{
			envIndex:       16,
			envVarConstant: "WAL_BUCKET_SCOPE_SUFFIX",
			envVarValue:    fmt.Sprintf("/%s", dummyUUID),
		},
		{
			envIndex:       17,
			envVarConstant: "WAL_BUCKET_SCOPE_PREFIX",
			envVarValue:    "",
		},
	}
	expectedValuesGCPCreds := []ExpectedValue{
		{
			envIndex:       15,
			envVarConstant: "WAL_GS_BUCKET",
			envVarValue:    "global-gs-bucket",
		},
		{
			envIndex:       16,
			envVarConstant: "WAL_BUCKET_SCOPE_SUFFIX",
			envVarValue:    fmt.Sprintf("/%s", dummyUUID),
		},
		{
			envIndex:       17,
			envVarConstant: "WAL_BUCKET_SCOPE_PREFIX",
			envVarValue:    "",
		},
		{
			envIndex:       18,
			envVarConstant: "GOOGLE_APPLICATION_CREDENTIALS",
			envVarValue:    "some-path-to-credentials",
		},
	}
	expectedS3BucketConfigMap := []ExpectedValue{
		{
			envIndex:       17,
			envVarConstant: "wal_s3_bucket",
			envVarValue:    "global-s3-bucket-configmap",
		},
	}
	expectedCustomS3BucketSpec := []ExpectedValue{
		{
			envIndex:       15,
			envVarConstant: "WAL_S3_BUCKET",
			envVarValue:    "custom-s3-bucket",
		},
	}
	expectedCustomVariableSecret := []ExpectedValue{
		{
			envIndex:       16,
			envVarConstant: "custom_variable",
			envVarValueRef: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: testPodEnvironmentSecretName,
					},
					Key: "custom_variable",
				},
			},
		},
	}
	expectedCustomVariableConfigMap := []ExpectedValue{
		{
			envIndex:       16,
			envVarConstant: "custom_variable",
			envVarValue:    "configmap-test",
		},
	}
	expectedCustomVariableSpec := []ExpectedValue{
		{
			envIndex:       15,
			envVarConstant: "CUSTOM_VARIABLE",
			envVarValue:    "spec-env-test",
		},
	}
	expectedCloneEnvSpec := []ExpectedValue{
		{
			envIndex:       16,
			envVarConstant: "CLONE_WALE_S3_PREFIX",
			envVarValue:    "s3://another-bucket",
		},
		{
			envIndex:       19,
			envVarConstant: "CLONE_WAL_BUCKET_SCOPE_PREFIX",
			envVarValue:    "",
		},
		{
			envIndex:       20,
			envVarConstant: "CLONE_AWS_ENDPOINT",
			envVarValue:    "s3.eu-central-1.amazonaws.com",
		},
	}
	expectedCloneEnvConfigMap := []ExpectedValue{
		{
			envIndex:       16,
			envVarConstant: "CLONE_WAL_S3_BUCKET",
			envVarValue:    "global-s3-bucket",
		},
		{
			envIndex:       17,
			envVarConstant: "CLONE_WAL_BUCKET_SCOPE_SUFFIX",
			envVarValue:    fmt.Sprintf("/%s", dummyUUID),
		},
		{
			envIndex:       21,
			envVarConstant: "clone_aws_endpoint",
			envVarValue:    "s3.eu-west-1.amazonaws.com",
		},
	}
	expectedCloneEnvSecret := []ExpectedValue{
		{
			envIndex:       21,
			envVarConstant: "clone_aws_access_key_id",
			envVarValueRef: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: testPodEnvironmentSecretName,
					},
					Key: "clone_aws_access_key_id",
				},
			},
		},
	}
	expectedStandbyEnvSecret := []ExpectedValue{
		{
			envIndex:       15,
			envVarConstant: "STANDBY_WALE_GS_PREFIX",
			envVarValue:    "gs://some/path/",
		},
		{
			envIndex:       20,
			envVarConstant: "standby_google_application_credentials",
			envVarValueRef: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: testPodEnvironmentSecretName,
					},
					Key: "standby_google_application_credentials",
				},
			},
		},
	}

	testName := "TestGenerateSpiloPodEnvVars"
	tests := []struct {
		subTest            string
		opConfig           config.Config
		cloneDescription   *acidv1.CloneDescription
		standbyDescription *acidv1.StandbyDescription
		expectedValues     []ExpectedValue
		pgsql              acidv1.Postgresql
	}{
		{
			subTest: "will set ENABLE_WAL_PATH_COMPAT env",
			opConfig: config.Config{
				EnableSpiloWalPathCompat: true,
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedSpiloWalPathCompat,
		},
		{
			subTest: "will set WAL_S3_BUCKET env",
			opConfig: config.Config{
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedValuesS3Bucket,
		},
		{
			subTest: "will set GOOGLE_APPLICATION_CREDENTIALS env",
			opConfig: config.Config{
				WALGSBucket:    "global-gs-bucket",
				GCPCredentials: "some-path-to-credentials",
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedValuesGCPCreds,
		},
		{
			subTest: "will not override global config KUBERNETES_SCOPE_LABEL parameter",
			opConfig: config.Config{
				Resources: config.Resources{
					ClusterNameLabel: "cluster-name",
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName, // contains kubernetes_scope_label, too
					},
				},
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedClusterNameLabel,
			pgsql: acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					Env: []v1.EnvVar{
						{
							Name:  "KUBERNETES_SCOPE_LABEL",
							Value: "my-scope-label",
						},
					},
				},
			},
		},
		{
			subTest: "will override global WAL_S3_BUCKET parameter from pod environment config map",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
				},
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedS3BucketConfigMap,
		},
		{
			subTest: "will override global WAL_S3_BUCKET parameter from manifest `env` section",
			opConfig: config.Config{
				WALGSBucket: "global-s3-bucket",
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCustomS3BucketSpec,
			pgsql: acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					Env: []v1.EnvVar{
						{
							Name:  "WAL_S3_BUCKET",
							Value: "custom-s3-bucket",
						},
					},
				},
			},
		},
		{
			subTest: "will set CUSTOM_VARIABLE from pod environment secret and not config map",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
					PodEnvironmentSecret:  testPodEnvironmentSecretName,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCustomVariableSecret,
		},
		{
			subTest: "will set CUSTOM_VARIABLE from pod environment config map",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
				},
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCustomVariableConfigMap,
		},
		{
			subTest: "will override CUSTOM_VARIABLE of pod environment secret/configmap from manifest `env` section",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
					PodEnvironmentSecret:  testPodEnvironmentSecretName,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
			},
			cloneDescription:   &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCustomVariableSpec,
			pgsql: acidv1.Postgresql{
				Spec: acidv1.PostgresSpec{
					Env: []v1.EnvVar{
						{
							Name:  "CUSTOM_VARIABLE",
							Value: "spec-env-test",
						},
					},
				},
			},
		},
		{
			subTest: "will set CLONE_ parameters from spec and not global config or pod environment config map",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
				},
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				EndTimestamp: "somewhen",
				UID:          dummyUUID,
				S3WalPath:    "s3://another-bucket",
				S3Endpoint:   "s3.eu-central-1.amazonaws.com",
			},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCloneEnvSpec,
		},
		{
			subTest: "will set CLONE_AWS_ENDPOINT parameter from pod environment config map",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: spec.NamespacedName{
						Name: testPodEnvironmentConfigMapName,
					},
				},
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				EndTimestamp: "somewhen",
				UID:          dummyUUID,
			},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCloneEnvConfigMap,
		},
		{
			subTest: "will set CLONE_AWS_ACCESS_KEY_ID parameter from pod environment secret",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecret:  testPodEnvironmentSecretName,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription: &acidv1.CloneDescription{
				ClusterName:  "test-cluster",
				EndTimestamp: "somewhen",
				UID:          dummyUUID,
			},
			standbyDescription: &acidv1.StandbyDescription{},
			expectedValues:     expectedCloneEnvSecret,
		},
		{
			subTest: "will set STANDBY_GOOGLE_APPLICATION_CREDENTIALS parameter from pod environment secret",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecret:  testPodEnvironmentSecretName,
					ResourceCheckInterval: time.Duration(testResourceCheckInterval),
					ResourceCheckTimeout:  time.Duration(testResourceCheckTimeout),
				},
				WALES3Bucket: "global-s3-bucket",
			},
			cloneDescription: &acidv1.CloneDescription{},
			standbyDescription: &acidv1.StandbyDescription{
				GSWalPath: "gs://some/path/",
			},
			expectedValues: expectedStandbyEnvSecret,
		},
	}

	for _, tt := range tests {
		c := newMockCluster(tt.opConfig)
		c.Postgresql = tt.pgsql
		actualEnvs := c.generateSpiloPodEnvVars(
			types.UID(dummyUUID), exampleSpiloConfig, tt.cloneDescription, tt.standbyDescription)

		for _, ev := range tt.expectedValues {
			env := actualEnvs[ev.envIndex]

			if env.Name != ev.envVarConstant {
				t.Errorf("%s %s: expected env name %s, have %s instead",
					testName, tt.subTest, ev.envVarConstant, env.Name)
			}

			if ev.envVarValueRef != nil {
				if !reflect.DeepEqual(env.ValueFrom, ev.envVarValueRef) {
					t.Errorf("%s %s: expected env value reference %#v, have %#v instead",
						testName, tt.subTest, ev.envVarValueRef, env.ValueFrom)
				}
				continue
			}

			if env.Value != ev.envVarValue {
				t.Errorf("%s %s: expected env value %s, have %s instead",
					testName, tt.subTest, ev.envVarValue, env.Value)
			}
		}
	}
}

func TestGetNumberOfInstances(t *testing.T) {
	testName := "TestGetNumberOfInstances"
	tests := []struct {
		subTest         string
		config          config.Config
		annotationKey   string
		annotationValue string
		desired         int32
		provided        int32
	}{
		{
			subTest: "no constraints",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      -1,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "",
				},
			},
			annotationKey:   "",
			annotationValue: "",
			desired:         2,
			provided:        2,
		},
		{
			subTest: "minInstances defined",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      2,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "",
				},
			},
			annotationKey:   "",
			annotationValue: "",
			desired:         1,
			provided:        2,
		},
		{
			subTest: "maxInstances defined",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      -1,
					MaxInstances:                      5,
					IgnoreInstanceLimitsAnnotationKey: "",
				},
			},
			annotationKey:   "",
			annotationValue: "",
			desired:         10,
			provided:        5,
		},
		{
			subTest: "ignore minInstances",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      2,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "ignore-instance-limits",
				},
			},
			annotationKey:   "ignore-instance-limits",
			annotationValue: "true",
			desired:         1,
			provided:        1,
		},
		{
			subTest: "want to ignore minInstances but wrong key",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      2,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "ignore-instance-limits",
				},
			},
			annotationKey:   "ignoring-instance-limits",
			annotationValue: "true",
			desired:         1,
			provided:        2,
		},
		{
			subTest: "want to ignore minInstances but wrong value",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      2,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "ignore-instance-limits",
				},
			},
			annotationKey:   "ignore-instance-limits",
			annotationValue: "active",
			desired:         1,
			provided:        2,
		},
		{
			subTest: "annotation set but no constraints to ignore",
			config: config.Config{
				Resources: config.Resources{
					MinInstances:                      -1,
					MaxInstances:                      -1,
					IgnoreInstanceLimitsAnnotationKey: "ignore-instance-limits",
				},
			},
			annotationKey:   "ignore-instance-limits",
			annotationValue: "true",
			desired:         1,
			provided:        1,
		},
	}

	for _, tt := range tests {
		var cluster = New(
			Config{
				OpConfig: tt.config,
			}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

		cluster.Spec.NumberOfInstances = tt.desired
		if tt.annotationKey != "" {
			cluster.ObjectMeta.Annotations = make(map[string]string)
			cluster.ObjectMeta.Annotations[tt.annotationKey] = tt.annotationValue
		}
		numInstances := cluster.getNumberOfInstances(&cluster.Spec)

		if numInstances != tt.provided {
			t.Errorf("%s %s: Expected to get %d instances, have %d instead",
				testName, tt.subTest, tt.provided, numInstances)
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

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

func TestAppendEnvVar(t *testing.T) {
	testName := "TestAppendEnvVar"
	tests := []struct {
		subTest      string
		envs         []v1.EnvVar
		envsToAppend []v1.EnvVar
		expectedSize int
	}{
		{
			subTest: "append two variables - one with same key that should get rejected",
			envs: []v1.EnvVar{
				{
					Name:  "CUSTOM_VARIABLE",
					Value: "test",
				},
			},
			envsToAppend: []v1.EnvVar{
				{
					Name:  "CUSTOM_VARIABLE",
					Value: "new-test",
				},
				{
					Name:  "ANOTHER_CUSTOM_VARIABLE",
					Value: "another-test",
				},
			},
			expectedSize: 2,
		},
		{
			subTest: "append empty slice",
			envs: []v1.EnvVar{
				{
					Name:  "CUSTOM_VARIABLE",
					Value: "test",
				},
			},
			envsToAppend: []v1.EnvVar{},
			expectedSize: 1,
		},
		{
			subTest: "append nil",
			envs: []v1.EnvVar{
				{
					Name:  "CUSTOM_VARIABLE",
					Value: "test",
				},
			},
			envsToAppend: nil,
			expectedSize: 1,
		},
	}

	for _, tt := range tests {
		finalEnvs := appendEnvVars(tt.envs, tt.envsToAppend...)

		if len(finalEnvs) != tt.expectedSize {
			t.Errorf("%s %s: expected %d env variables, got %d",
				testName, tt.subTest, tt.expectedSize, len(finalEnvs))
		}

		for _, env := range tt.envs {
			for _, finalEnv := range finalEnvs {
				if env.Name == finalEnv.Name {
					if env.Value != finalEnv.Value {
						t.Errorf("%s %s: expected env value %s of variable %s, got %s instead",
							testName, tt.subTest, env.Value, env.Name, finalEnv.Value)
					}
				}
			}
		}
	}
}

func TestStandbyEnv(t *testing.T) {
	testName := "TestStandbyEnv"
	tests := []struct {
		subTest     string
		standbyOpts *acidv1.StandbyDescription
		env         v1.EnvVar
		envPos      int
		envLen      int
	}{
		{
			subTest: "from custom s3 path",
			standbyOpts: &acidv1.StandbyDescription{
				S3WalPath: "s3://some/path/",
			},
			env: v1.EnvVar{
				Name:  "STANDBY_WALE_S3_PREFIX",
				Value: "s3://some/path/",
			},
			envPos: 0,
			envLen: 3,
		},
		{
			subTest: "ignore gs path if s3 is set",
			standbyOpts: &acidv1.StandbyDescription{
				S3WalPath: "s3://some/path/",
				GSWalPath: "gs://some/path/",
			},
			env: v1.EnvVar{
				Name:  "STANDBY_METHOD",
				Value: "STANDBY_WITH_WALE",
			},
			envPos: 1,
			envLen: 3,
		},
		{
			subTest: "from remote primary",
			standbyOpts: &acidv1.StandbyDescription{
				StandbyHost: "remote-primary",
			},
			env: v1.EnvVar{
				Name:  "STANDBY_HOST",
				Value: "remote-primary",
			},
			envPos: 0,
			envLen: 1,
		},
		{
			subTest: "from remote primary with port",
			standbyOpts: &acidv1.StandbyDescription{
				StandbyHost: "remote-primary",
				StandbyPort: "9876",
			},
			env: v1.EnvVar{
				Name:  "STANDBY_PORT",
				Value: "9876",
			},
			envPos: 1,
			envLen: 2,
		},
		{
			subTest: "from remote primary - ignore WAL path",
			standbyOpts: &acidv1.StandbyDescription{
				GSWalPath:   "gs://some/path/",
				StandbyHost: "remote-primary",
			},
			env: v1.EnvVar{
				Name:  "STANDBY_HOST",
				Value: "remote-primary",
			},
			envPos: 0,
			envLen: 1,
		},
	}

	var cluster = New(
		Config{}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	for _, tt := range tests {
		envs := cluster.generateStandbyEnvironment(tt.standbyOpts)

		env := envs[tt.envPos]

		if env.Name != tt.env.Name {
			t.Errorf("%s %s: Expected env name %s, have %s instead",
				testName, tt.subTest, tt.env.Name, env.Name)
		}

		if env.Value != tt.env.Value {
			t.Errorf("%s %s: Expected env value %s, have %s instead",
				testName, tt.subTest, tt.env.Value, env.Value)
		}

		if len(envs) != tt.envLen {
			t.Errorf("%s %s: Expected number of env variables %d, have %d instead",
				testName, tt.subTest, tt.envLen, len(envs))
		}
	}
}

func TestNodeAffinity(t *testing.T) {
	var err error
	var spec acidv1.PostgresSpec
	var cluster *Cluster
	var spiloRunAsUser = int64(101)
	var spiloRunAsGroup = int64(103)
	var spiloFSGroup = int64(103)

	makeSpec := func(nodeAffinity *v1.NodeAffinity) acidv1.PostgresSpec {
		return acidv1.PostgresSpec{
			TeamID: "myapp", NumberOfInstances: 1,
			Resources: &acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			},
			Volume: acidv1.Volume{
				Size: "1G",
			},
			NodeAffinity: nodeAffinity,
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
				Resources: config.Resources{
					SpiloRunAsUser:  &spiloRunAsUser,
					SpiloRunAsGroup: &spiloRunAsGroup,
					SpiloFSGroup:    &spiloFSGroup,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	nodeAff := &v1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{
				v1.NodeSelectorTerm{
					MatchExpressions: []v1.NodeSelectorRequirement{
						v1.NodeSelectorRequirement{
							Key:      "test-label",
							Operator: v1.NodeSelectorOpIn,
							Values: []string{
								"test-value",
							},
						},
					},
				},
			},
		},
	}
	spec = makeSpec(nodeAff)
	s, err := cluster.generateStatefulSet(&spec)
	if err != nil {
		assert.NoError(t, err)
	}

	assert.NotNil(t, s.Spec.Template.Spec.Affinity.NodeAffinity, "node affinity in statefulset shouldn't be nil")
	assert.Equal(t, s.Spec.Template.Spec.Affinity.NodeAffinity, nodeAff, "cluster template has correct node affinity")
}

func testDeploymentOwnerReference(cluster *Cluster, deployment *appsv1.Deployment) error {
	owner := deployment.ObjectMeta.OwnerReferences[0]

	if owner.Name != cluster.Statefulset.ObjectMeta.Name {
		return fmt.Errorf("Ownere reference is incorrect, got %s, expected %s",
			owner.Name, cluster.Statefulset.ObjectMeta.Name)
	}

	return nil
}

func testServiceOwnerReference(cluster *Cluster, service *v1.Service, role PostgresRole) error {
	owner := service.ObjectMeta.OwnerReferences[0]

	if owner.Name != cluster.Statefulset.ObjectMeta.Name {
		return fmt.Errorf("Ownere reference is incorrect, got %s, expected %s",
			owner.Name, cluster.Statefulset.ObjectMeta.Name)
	}

	return nil
}

func TestTLS(t *testing.T) {

	client, _ := newFakeK8sTestClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	tlsSecretName := "my-secret"
	spiloRunAsUser := int64(101)
	spiloRunAsGroup := int64(103)
	spiloFSGroup := int64(103)
	defaultMode := int32(0640)
	mountPath := "/tls"

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			TeamID: "myapp", NumberOfInstances: 1,
			Resources: &acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			},
			Volume: acidv1.Volume{
				Size: "1G",
			},
			TLS: &acidv1.TLSDescription{
				SecretName: tlsSecretName, CAFile: "ca.crt"},
			AdditionalVolumes: []acidv1.AdditionalVolume{
				acidv1.AdditionalVolume{
					Name:      tlsSecretName,
					MountPath: mountPath,
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName:  tlsSecretName,
							DefaultMode: &defaultMode,
						},
					},
				},
			},
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				ProtectedRoles:      []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
				Resources: config.Resources{
					SpiloRunAsUser:  &spiloRunAsUser,
					SpiloRunAsGroup: &spiloRunAsGroup,
					SpiloFSGroup:    &spiloFSGroup,
				},
			},
		}, client, pg, logger, eventRecorder)

	// create a statefulset
	sts, err := cluster.createStatefulSet()
	assert.NoError(t, err)

	fsGroup := int64(103)
	assert.Equal(t, &fsGroup, sts.Spec.Template.Spec.SecurityContext.FSGroup, "has a default FSGroup assigned")

	volume := v1.Volume{
		Name: "my-secret",
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName:  "my-secret",
				DefaultMode: &defaultMode,
			},
		},
	}
	assert.Contains(t, sts.Spec.Template.Spec.Volumes, volume, "the pod gets a secret volume")

	postgresContainer := getPostgresContainer(&sts.Spec.Template.Spec)
	assert.Contains(t, postgresContainer.VolumeMounts, v1.VolumeMount{
		MountPath: "/tls",
		Name:      "my-secret",
	}, "the volume gets mounted in /tls")

	assert.Contains(t, postgresContainer.Env, v1.EnvVar{Name: "SSL_CERTIFICATE_FILE", Value: "/tls/tls.crt"})
	assert.Contains(t, postgresContainer.Env, v1.EnvVar{Name: "SSL_PRIVATE_KEY_FILE", Value: "/tls/tls.key"})
	assert.Contains(t, postgresContainer.Env, v1.EnvVar{Name: "SSL_CA_FILE", Value: "/tls/ca.crt"})
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
						Name: "postgres",
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
		postgresContainer := getPostgresContainer(tt.podSpec)

		volumeName := tt.podSpec.Volumes[tt.shmPos].Name
		volumeMountName := postgresContainer.VolumeMounts[tt.shmPos].Name

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
		postgresContainer := getPostgresContainer(tt.podSpec)

		numMounts := len(postgresContainer.VolumeMounts)

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

		postgresContainer = getPostgresContainer(tt.podSpec)
		numMountsCheck := len(postgresContainer.VolumeMounts)

		if numMountsCheck != numMounts+1 {
			t.Errorf("Unexpected number of VolumeMounts: got %v instead of %v",
				numMountsCheck, numMounts+1)
		}
	}
}

func TestAdditionalVolume(t *testing.T) {
	testName := "TestAdditionalVolume"

	client, _ := newFakeK8sTestClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	sidecarName := "sidecar"
	additionalVolumes := []acidv1.AdditionalVolume{
		{
			Name:             "test1",
			MountPath:        "/test1",
			TargetContainers: []string{"all"},
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
		{
			Name:             "test2",
			MountPath:        "/test2",
			TargetContainers: []string{sidecarName},
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
		{
			Name:             "test3",
			MountPath:        "/test3",
			TargetContainers: []string{}, // should mount only to postgres
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
		{
			Name:             "test4",
			MountPath:        "/test4",
			TargetContainers: nil, // should mount only to postgres
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
	}

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			TeamID: "myapp", NumberOfInstances: 1,
			Resources: &acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			},
			Volume: acidv1.Volume{
				Size: "1G",
			},
			AdditionalVolumes: additionalVolumes,
			Sidecars: []acidv1.Sidecar{
				{
					Name: sidecarName,
				},
			},
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
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

	// create a statefulset
	sts, err := cluster.createStatefulSet()
	assert.NoError(t, err)

	tests := []struct {
		subTest        string
		container      string
		expectedMounts []string
	}{
		{
			subTest:        "checking volume mounts of postgres container",
			container:      constants.PostgresContainerName,
			expectedMounts: []string{"pgdata", "test1", "test3", "test4"},
		},
		{
			subTest:        "checking volume mounts of sidecar container",
			container:      "sidecar",
			expectedMounts: []string{"pgdata", "test1", "test2"},
		},
	}

	for _, tt := range tests {
		for _, container := range sts.Spec.Template.Spec.Containers {
			if container.Name != tt.container {
				continue
			}
			mounts := []string{}
			for _, volumeMounts := range container.VolumeMounts {
				mounts = append(mounts, volumeMounts.Name)
			}

			if !util.IsEqualIgnoreOrder(mounts, tt.expectedMounts) {
				t.Errorf("%s %s: different volume mounts: got %v, epxected %v",
					testName, tt.subTest, mounts, tt.expectedMounts)
			}
		}
	}
}

func TestVolumeSelector(t *testing.T) {
	testName := "TestVolumeSelector"
	makeSpec := func(volume acidv1.Volume) acidv1.PostgresSpec {
		return acidv1.PostgresSpec{
			TeamID:            "myapp",
			NumberOfInstances: 0,
			Resources: &acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			},
			Volume: volume,
		}
	}

	tests := []struct {
		subTest      string
		volume       acidv1.Volume
		wantSelector *metav1.LabelSelector
	}{
		{
			subTest: "PVC template has no selector",
			volume: acidv1.Volume{
				Size: "1G",
			},
			wantSelector: nil,
		},
		{
			subTest: "PVC template has simple label selector",
			volume: acidv1.Volume{
				Size: "1G",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"environment": "unittest"},
				},
			},
			wantSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"environment": "unittest"},
			},
		},
		{
			subTest: "PVC template has full selector",
			volume: acidv1.Volume{
				Size: "1G",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"environment": "unittest"},
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "flavour",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"banana", "chocolate"},
						},
					},
				},
			},
			wantSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"environment": "unittest"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "flavour",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"banana", "chocolate"},
					},
				},
			},
		},
	}

	cluster := New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				ProtectedRoles:      []string{"admin"},
				Auth: config.Auth{
					SuperUsername:       superUserName,
					ReplicationUsername: replicationUserName,
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	for _, tt := range tests {
		pgSpec := makeSpec(tt.volume)
		sts, err := cluster.generateStatefulSet(&pgSpec)
		if err != nil {
			t.Fatalf("%s %s: no statefulset created %v", testName, tt.subTest, err)
		}

		volIdx := len(sts.Spec.VolumeClaimTemplates)
		for i, ct := range sts.Spec.VolumeClaimTemplates {
			if ct.ObjectMeta.Name == constants.DataVolumeName {
				volIdx = i
				break
			}
		}
		if volIdx == len(sts.Spec.VolumeClaimTemplates) {
			t.Errorf("%s %s: no datavolume found in sts", testName, tt.subTest)
		}

		selector := sts.Spec.VolumeClaimTemplates[volIdx].Spec.Selector
		if !reflect.DeepEqual(selector, tt.wantSelector) {
			t.Errorf("%s %s: expected: %#v but got: %#v", testName, tt.subTest, tt.wantSelector, selector)
		}
	}
}

// inject sidecars through all available mechanisms and check the resulting container specs
func TestSidecars(t *testing.T) {
	var err error
	var spec acidv1.PostgresSpec
	var cluster *Cluster

	generateKubernetesResources := func(cpuRequest string, cpuLimit string, memoryRequest string, memoryLimit string) v1.ResourceRequirements {
		parsedCPURequest, err := resource.ParseQuantity(cpuRequest)
		assert.NoError(t, err)
		parsedCPULimit, err := resource.ParseQuantity(cpuLimit)
		assert.NoError(t, err)
		parsedMemoryRequest, err := resource.ParseQuantity(memoryRequest)
		assert.NoError(t, err)
		parsedMemoryLimit, err := resource.ParseQuantity(memoryLimit)
		assert.NoError(t, err)
		return v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:    parsedCPURequest,
				v1.ResourceMemory: parsedMemoryRequest,
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:    parsedCPULimit,
				v1.ResourceMemory: parsedMemoryLimit,
			},
		}
	}

	spec = acidv1.PostgresSpec{
		PostgresqlParam: acidv1.PostgresqlParam{
			PgVersion: "12.1",
			Parameters: map[string]string{
				"max_connections": "100",
			},
		},
		TeamID: "myapp", NumberOfInstances: 1,
		Resources: &acidv1.Resources{
			ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
		},
		Volume: acidv1.Volume{
			Size: "1G",
		},
		Sidecars: []acidv1.Sidecar{
			acidv1.Sidecar{
				Name: "cluster-specific-sidecar",
			},
			acidv1.Sidecar{
				Name: "cluster-specific-sidecar-with-resources",
				Resources: &acidv1.Resources{
					ResourceRequests: acidv1.ResourceDescription{CPU: "210m", Memory: "0.8Gi"},
					ResourceLimits:   acidv1.ResourceDescription{CPU: "510m", Memory: "1.4Gi"},
				},
			},
			acidv1.Sidecar{
				Name:        "replace-sidecar",
				DockerImage: "override-image",
			},
		},
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
				Resources: config.Resources{
					DefaultCPURequest:    "200m",
					MaxCPURequest:        "300m",
					DefaultCPULimit:      "500m",
					DefaultMemoryRequest: "0.7Gi",
					MaxMemoryRequest:     "1.0Gi",
					DefaultMemoryLimit:   "1.3Gi",
				},
				SidecarImages: map[string]string{
					"deprecated-global-sidecar": "image:123",
				},
				SidecarContainers: []v1.Container{
					v1.Container{
						Name: "global-sidecar",
					},
					// will be replaced by a cluster specific sidecar with the same name
					v1.Container{
						Name:  "replace-sidecar",
						Image: "replaced-image",
					},
				},
				Scalyr: config.Scalyr{
					ScalyrAPIKey:        "abc",
					ScalyrImage:         "scalyr-image",
					ScalyrCPURequest:    "220m",
					ScalyrCPULimit:      "520m",
					ScalyrMemoryRequest: "0.9Gi",
					// ise default memory limit
				},
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	s, err := cluster.generateStatefulSet(&spec)
	assert.NoError(t, err)

	env := []v1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name:  "POSTGRES_USER",
			Value: superUserName,
		},
		{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &v1.SecretKeySelector{
					LocalObjectReference: v1.LocalObjectReference{
						Name: "",
					},
					Key: "password",
				},
			},
		},
	}
	mounts := []v1.VolumeMount{
		v1.VolumeMount{
			Name:      "pgdata",
			MountPath: "/home/postgres/pgdata",
		},
	}

	// deduplicated sidecars and Patroni
	assert.Equal(t, 7, len(s.Spec.Template.Spec.Containers), "wrong number of containers")

	// cluster specific sidecar
	assert.Contains(t, s.Spec.Template.Spec.Containers, v1.Container{
		Name:            "cluster-specific-sidecar",
		Env:             env,
		Resources:       generateKubernetesResources("200m", "500m", "0.7Gi", "1.3Gi"),
		ImagePullPolicy: v1.PullIfNotPresent,
		VolumeMounts:    mounts,
	})

	// container specific resources
	expectedResources := generateKubernetesResources("210m", "510m", "0.8Gi", "1.4Gi")
	assert.Equal(t, expectedResources.Requests[v1.ResourceCPU], s.Spec.Template.Spec.Containers[2].Resources.Requests[v1.ResourceCPU])
	assert.Equal(t, expectedResources.Limits[v1.ResourceCPU], s.Spec.Template.Spec.Containers[2].Resources.Limits[v1.ResourceCPU])
	assert.Equal(t, expectedResources.Requests[v1.ResourceMemory], s.Spec.Template.Spec.Containers[2].Resources.Requests[v1.ResourceMemory])
	assert.Equal(t, expectedResources.Limits[v1.ResourceMemory], s.Spec.Template.Spec.Containers[2].Resources.Limits[v1.ResourceMemory])

	// deprecated global sidecar
	assert.Contains(t, s.Spec.Template.Spec.Containers, v1.Container{
		Name:            "deprecated-global-sidecar",
		Image:           "image:123",
		Env:             env,
		Resources:       generateKubernetesResources("200m", "500m", "0.7Gi", "1.3Gi"),
		ImagePullPolicy: v1.PullIfNotPresent,
		VolumeMounts:    mounts,
	})

	// global sidecar
	assert.Contains(t, s.Spec.Template.Spec.Containers, v1.Container{
		Name:         "global-sidecar",
		Env:          env,
		VolumeMounts: mounts,
	})

	// replaced sidecar
	assert.Contains(t, s.Spec.Template.Spec.Containers, v1.Container{
		Name:            "replace-sidecar",
		Image:           "override-image",
		Resources:       generateKubernetesResources("200m", "500m", "0.7Gi", "1.3Gi"),
		ImagePullPolicy: v1.PullIfNotPresent,
		Env:             env,
		VolumeMounts:    mounts,
	})

	// replaced sidecar
	// the order in env is important
	scalyrEnv := append(env, v1.EnvVar{Name: "SCALYR_API_KEY", Value: "abc"}, v1.EnvVar{Name: "SCALYR_SERVER_HOST", Value: ""})
	assert.Contains(t, s.Spec.Template.Spec.Containers, v1.Container{
		Name:            "scalyr-sidecar",
		Image:           "scalyr-image",
		Resources:       generateKubernetesResources("220m", "520m", "0.9Gi", "1.3Gi"),
		ImagePullPolicy: v1.PullIfNotPresent,
		Env:             scalyrEnv,
		VolumeMounts:    mounts,
	})

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
				logger,
				eventRecorder),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: util.ToIntStr(1),
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
				logger,
				eventRecorder),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: util.ToIntStr(0),
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
				logger,
				eventRecorder),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-pdb",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: util.ToIntStr(0),
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
				logger,
				eventRecorder),
			policyv1beta1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-myapp-database-databass-budget",
					Namespace: "myapp",
					Labels:    map[string]string{"team": "myapp", "cluster-name": "myapp-database"},
				},
				Spec: policyv1beta1.PodDisruptionBudgetSpec{
					MinAvailable: util.ToIntStr(1),
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

func TestGenerateService(t *testing.T) {
	var spec acidv1.PostgresSpec
	var cluster *Cluster
	var enableLB bool = true
	spec = acidv1.PostgresSpec{
		TeamID: "myapp", NumberOfInstances: 1,
		Resources: &acidv1.Resources{
			ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
		},
		Volume: acidv1.Volume{
			Size: "1G",
		},
		Sidecars: []acidv1.Sidecar{
			acidv1.Sidecar{
				Name: "cluster-specific-sidecar",
			},
			acidv1.Sidecar{
				Name: "cluster-specific-sidecar-with-resources",
				Resources: &acidv1.Resources{
					ResourceRequests: acidv1.ResourceDescription{CPU: "210m", Memory: "0.8Gi"},
					ResourceLimits:   acidv1.ResourceDescription{CPU: "510m", Memory: "1.4Gi"},
				},
			},
			acidv1.Sidecar{
				Name:        "replace-sidecar",
				DockerImage: "override-image",
			},
		},
		EnableMasterLoadBalancer: &enableLB,
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
				Resources: config.Resources{
					DefaultCPURequest:    "200m",
					MaxCPURequest:        "300m",
					DefaultCPULimit:      "500m",
					DefaultMemoryRequest: "0.7Gi",
					MaxMemoryRequest:     "1.0Gi",
					DefaultMemoryLimit:   "1.3Gi",
				},
				SidecarImages: map[string]string{
					"deprecated-global-sidecar": "image:123",
				},
				SidecarContainers: []v1.Container{
					v1.Container{
						Name: "global-sidecar",
					},
					// will be replaced by a cluster specific sidecar with the same name
					v1.Container{
						Name:  "replace-sidecar",
						Image: "replaced-image",
					},
				},
				Scalyr: config.Scalyr{
					ScalyrAPIKey:        "abc",
					ScalyrImage:         "scalyr-image",
					ScalyrCPURequest:    "220m",
					ScalyrCPULimit:      "520m",
					ScalyrMemoryRequest: "0.9Gi",
					// ise default memory limit
				},
				ExternalTrafficPolicy: "Cluster",
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	service := cluster.generateService(Master, &spec)
	assert.Equal(t, v1.ServiceExternalTrafficPolicyTypeCluster, service.Spec.ExternalTrafficPolicy)
	cluster.OpConfig.ExternalTrafficPolicy = "Local"
	service = cluster.generateService(Master, &spec)
	assert.Equal(t, v1.ServiceExternalTrafficPolicyTypeLocal, service.Spec.ExternalTrafficPolicy)

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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

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

func newLBFakeClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		DeploymentsGetter: clientSet.AppsV1(),
		PodsGetter:        clientSet.CoreV1(),
		ServicesGetter:    clientSet.CoreV1(),
	}, clientSet
}

func getServices(serviceType v1.ServiceType, sourceRanges []string, extTrafficPolicy, clusterName string) []v1.ServiceSpec {
	return []v1.ServiceSpec{
		v1.ServiceSpec{
			ExternalTrafficPolicy:    v1.ServiceExternalTrafficPolicyType(extTrafficPolicy),
			LoadBalancerSourceRanges: sourceRanges,
			Ports:                    []v1.ServicePort{{Name: "postgresql", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			Type:                     serviceType,
		},
		v1.ServiceSpec{
			ExternalTrafficPolicy:    v1.ServiceExternalTrafficPolicyType(extTrafficPolicy),
			LoadBalancerSourceRanges: sourceRanges,
			Ports:                    []v1.ServicePort{{Name: clusterName + "-pooler", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			Selector:                 map[string]string{"connection-pooler": clusterName + "-pooler"},
			Type:                     serviceType,
		},
		v1.ServiceSpec{
			ExternalTrafficPolicy:    v1.ServiceExternalTrafficPolicyType(extTrafficPolicy),
			LoadBalancerSourceRanges: sourceRanges,
			Ports:                    []v1.ServicePort{{Name: "postgresql", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			Selector:                 map[string]string{"spilo-role": "replica", "application": "spilo", "cluster-name": clusterName},
			Type:                     serviceType,
		},
		v1.ServiceSpec{
			ExternalTrafficPolicy:    v1.ServiceExternalTrafficPolicyType(extTrafficPolicy),
			LoadBalancerSourceRanges: sourceRanges,
			Ports:                    []v1.ServicePort{{Name: clusterName + "-pooler-repl", Port: 5432, TargetPort: intstr.IntOrString{IntVal: 5432}}},
			Selector:                 map[string]string{"connection-pooler": clusterName + "-pooler-repl"},
			Type:                     serviceType,
		},
	}
}

func TestEnableLoadBalancers(t *testing.T) {
	testName := "Test enabling LoadBalancers"
	client, _ := newLBFakeClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	clusterNameLabel := "cluster-name"
	roleLabel := "spilo-role"
	roles := []PostgresRole{Master, Replica}
	sourceRanges := []string{"192.186.1.2/22"}
	extTrafficPolicy := "Cluster"

	tests := []struct {
		subTest          string
		config           config.Config
		pgSpec           acidv1.Postgresql
		expectedServices []v1.ServiceSpec
	}{
		{
			subTest: "LBs enabled in config, disabled in manifest",
			config: config.Config{
				ConnectionPooler: config.ConnectionPooler{
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:                    k8sutil.Int32ToPointer(1),
				},
				EnableMasterLoadBalancer:        true,
				EnableMasterPoolerLoadBalancer:  true,
				EnableReplicaLoadBalancer:       true,
				EnableReplicaPoolerLoadBalancer: true,
				ExternalTrafficPolicy:           extTrafficPolicy,
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: clusterNameLabel,
					PodRoleLabel:     roleLabel,
				},
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					AllowedSourceRanges:             sourceRanges,
					EnableConnectionPooler:          util.True(),
					EnableReplicaConnectionPooler:   util.True(),
					EnableMasterLoadBalancer:        util.False(),
					EnableMasterPoolerLoadBalancer:  util.False(),
					EnableReplicaLoadBalancer:       util.False(),
					EnableReplicaPoolerLoadBalancer: util.False(),
					NumberOfInstances:               1,
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
						ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedServices: getServices(v1.ServiceTypeClusterIP, nil, "", clusterName),
		},
		{
			subTest: "LBs enabled in manifest, disabled in config",
			config: config.Config{
				ConnectionPooler: config.ConnectionPooler{
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:                    k8sutil.Int32ToPointer(1),
				},
				EnableMasterLoadBalancer:        false,
				EnableMasterPoolerLoadBalancer:  false,
				EnableReplicaLoadBalancer:       false,
				EnableReplicaPoolerLoadBalancer: false,
				ExternalTrafficPolicy:           extTrafficPolicy,
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: clusterNameLabel,
					PodRoleLabel:     roleLabel,
				},
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					AllowedSourceRanges:             sourceRanges,
					EnableConnectionPooler:          util.True(),
					EnableReplicaConnectionPooler:   util.True(),
					EnableMasterLoadBalancer:        util.True(),
					EnableMasterPoolerLoadBalancer:  util.True(),
					EnableReplicaLoadBalancer:       util.True(),
					EnableReplicaPoolerLoadBalancer: util.True(),
					NumberOfInstances:               1,
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
						ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedServices: getServices(v1.ServiceTypeLoadBalancer, sourceRanges, extTrafficPolicy, clusterName),
		},
	}

	for _, tt := range tests {
		var cluster = New(
			Config{
				OpConfig: tt.config,
			}, client, tt.pgSpec, logger, eventRecorder)

		cluster.Name = clusterName
		cluster.Namespace = namespace
		cluster.ConnectionPooler = map[PostgresRole]*ConnectionPoolerObjects{}
		generatedServices := make([]v1.ServiceSpec, 0)
		for _, role := range roles {
			cluster.syncService(role)
			cluster.ConnectionPooler[role] = &ConnectionPoolerObjects{
				Name:        cluster.connectionPoolerName(role),
				ClusterName: cluster.ClusterName,
				Namespace:   cluster.Namespace,
				Role:        role,
			}
			cluster.syncConnectionPoolerWorker(&tt.pgSpec, &tt.pgSpec, role)
			generatedServices = append(generatedServices, cluster.Services[role].Spec)
			generatedServices = append(generatedServices, cluster.ConnectionPooler[role].Service.Spec)
		}
		if !reflect.DeepEqual(tt.expectedServices, generatedServices) {
			t.Errorf("%s %s: expected %#v but got %#v", testName, tt.subTest, tt.expectedServices, generatedServices)
		}
	}
}

func TestGenerateResourceRequirements(t *testing.T) {
	testName := "TestGenerateResourceRequirements"
	client, _ := newFakeK8sTestClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	clusterNameLabel := "cluster-name"
	roleLabel := "spilo-role"
	sidecarName := "postgres-exporter"

	// enforceMinResourceLimits will be called 2 twice emitting 4 events (2x cpu, 2x memory raise)
	// enforceMaxResourceRequests will be called 4 times emitting 6 events (2x cpu, 4x memory cap)
	// hence event bufferSize of 10 is required
	newEventRecorder := record.NewFakeRecorder(10)

	configResources := config.Resources{
		ClusterLabels:        map[string]string{"application": "spilo"},
		ClusterNameLabel:     clusterNameLabel,
		DefaultCPURequest:    "100m",
		DefaultCPULimit:      "1",
		MaxCPURequest:        "500m",
		MinCPULimit:          "250m",
		DefaultMemoryRequest: "100Mi",
		DefaultMemoryLimit:   "500Mi",
		MaxMemoryRequest:     "1Gi",
		MinMemoryLimit:       "250Mi",
		PodRoleLabel:         roleLabel,
	}

	tests := []struct {
		subTest           string
		config            config.Config
		pgSpec            acidv1.Postgresql
		expectedResources acidv1.Resources
	}{
		{
			subTest: "test generation of default resources when empty in manifest",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "500Mi"},
			},
		},
		{
			subTest: "test generation of default resources for sidecar",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Sidecars: []acidv1.Sidecar{
						acidv1.Sidecar{
							Name: sidecarName,
						},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "500Mi"},
			},
		},
		{
			subTest: "test generation of resources when only requests are defined in manifest",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "50m", Memory: "50Mi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "50m", Memory: "50Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "500Mi"},
			},
		},
		{
			subTest: "test generation of resources when only memory is defined in manifest",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{Memory: "100Mi"},
						ResourceLimits:   acidv1.ResourceDescription{Memory: "1Gi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "1Gi"},
			},
		},
		{
			subTest: "test SetMemoryRequestToLimit flag",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: true,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{Memory: "200Mi"},
						ResourceLimits:   acidv1.ResourceDescription{Memory: "300Mi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "300Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "300Mi"},
			},
		},
		{
			subTest: "test SetMemoryRequestToLimit flag for sidecar container, too",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: true,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Sidecars: []acidv1.Sidecar{
						acidv1.Sidecar{
							Name: sidecarName,
							Resources: &acidv1.Resources{
								ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "10Mi"},
								ResourceLimits:   acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
							},
						},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "100Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
			},
		},
		{
			subTest: "test generating resources from manifest",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "250Mi"},
						ResourceLimits:   acidv1.ResourceDescription{CPU: "400m", Memory: "800Mi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "250Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "400m", Memory: "800Mi"},
			},
		},
		{
			subTest: "test enforcing min cpu and memory limit",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
						ResourceLimits:   acidv1.ResourceDescription{CPU: "200m", Memory: "200Mi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "250m", Memory: "250Mi"},
			},
		},
		{
			subTest: "test min cpu and memory limit are not enforced on sidecar",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Sidecars: []acidv1.Sidecar{
						acidv1.Sidecar{
							Name: sidecarName,
							Resources: &acidv1.Resources{
								ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "10Mi"},
								ResourceLimits:   acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
							},
						},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "10m", Memory: "10Mi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "100m", Memory: "100Mi"},
			},
		},
		{
			subTest: "test enforcing max cpu and memory requests",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: false,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "2Gi"},
						ResourceLimits:   acidv1.ResourceDescription{CPU: "2", Memory: "4Gi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "500m", Memory: "1Gi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "2", Memory: "4Gi"},
			},
		},
		{
			subTest: "test SetMemoryRequestToLimit flag but raise only until max memory request",
			config: config.Config{
				Resources:               configResources,
				PodManagementPolicy:     "ordered_ready",
				SetMemoryRequestToLimit: true,
			},
			pgSpec: acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: acidv1.PostgresSpec{
					Resources: &acidv1.Resources{
						ResourceRequests: acidv1.ResourceDescription{Memory: "500Mi"},
						ResourceLimits:   acidv1.ResourceDescription{Memory: "2Gi"},
					},
					TeamID: "acid",
					Volume: acidv1.Volume{
						Size: "1G",
					},
				},
			},
			expectedResources: acidv1.Resources{
				ResourceRequests: acidv1.ResourceDescription{CPU: "100m", Memory: "1Gi"},
				ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "2Gi"},
			},
		},
	}

	for _, tt := range tests {
		var cluster = New(
			Config{
				OpConfig: tt.config,
			}, client, tt.pgSpec, logger, newEventRecorder)

		cluster.Name = clusterName
		cluster.Namespace = namespace
		_, err := cluster.createStatefulSet()
		if k8sutil.ResourceAlreadyExists(err) {
			err = cluster.syncStatefulSet()
		}
		assert.NoError(t, err)

		containers := cluster.Statefulset.Spec.Template.Spec.Containers
		clusterResources, err := parseResourceRequirements(containers[0].Resources)
		if len(containers) > 1 {
			clusterResources, err = parseResourceRequirements(containers[1].Resources)
		}
		assert.NoError(t, err)
		if !reflect.DeepEqual(tt.expectedResources, clusterResources) {
			t.Errorf("%s - %s: expected %#v but got %#v", testName, tt.subTest, tt.expectedResources, clusterResources)
		}
	}
}

func TestGenerateCapabilities(t *testing.T) {

	testName := "TestGenerateCapabilities"
	tests := []struct {
		subTest      string
		configured   []string
		capabilities *v1.Capabilities
		err          error
	}{
		{
			subTest:      "no capabilities",
			configured:   nil,
			capabilities: nil,
			err:          fmt.Errorf("could not parse capabilities configuration of nil"),
		},
		{
			subTest:      "empty capabilities",
			configured:   []string{},
			capabilities: nil,
			err:          fmt.Errorf("could not parse empty capabilities configuration"),
		},
		{
			subTest:    "configured capability",
			configured: []string{"SYS_NICE"},
			capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_NICE"},
			},
			err: fmt.Errorf("could not generate one configured capability"),
		},
		{
			subTest:    "configured capabilities",
			configured: []string{"SYS_NICE", "CHOWN"},
			capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_NICE", "CHOWN"},
			},
			err: fmt.Errorf("could not generate multiple configured capabilities"),
		},
	}
	for _, tt := range tests {
		caps := generateCapabilities(tt.configured)
		if !reflect.DeepEqual(caps, tt.capabilities) {
			t.Errorf("%s %s: expected `%v` but got `%v`",
				testName, tt.subTest, tt.capabilities, caps)
		}
	}
}
