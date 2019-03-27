package cluster

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
)

func True() *bool {
	b := true
	return &b
}

func False() *bool {
	b := false
	return &b
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
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: True()},
			opConfig: config.Config{},
			result:   true,
		},
		{
			subtest:  "new format, load balancer is disabled for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: False()},
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

const (
	testPodEnvironmentConfigMapName = "pod_env_cm"
	testPodEnvironmentSecretName    = "pod_env_sc"
)

type mockSecret struct {
	v1core.SecretInterface
}

type mockConfigMap struct {
	v1core.ConfigMapInterface
}

func (c *mockSecret) Get(name string, options metav1.GetOptions) (*v1.Secret, error) {
	if name != testPodEnvironmentSecretName {
		return nil, fmt.Errorf("NotFound")
	}
	secret := &v1.Secret{}
	secret.Name = testPodEnvironmentSecretName
	secret.Data = map[string][]byte{
		"minio_access_key": []byte("alpha"),
		"minio_secret_key": []byte("beta"),
	}
	return secret, nil
}

func (c *mockConfigMap) Get(name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	if name != testPodEnvironmentConfigMapName {
		return nil, fmt.Errorf("NotFound")
	}
	configmap := &v1.ConfigMap{}
	configmap.Name = testPodEnvironmentConfigMapName
	configmap.Data = map[string]string{
		"foo": "bar",
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
					PodEnvironmentConfigMap: "unknown",
				},
			},
			err: fmt.Errorf("could not read ConfigMap PodEnvironmentConfigMap: NotFound"),
		},
		{
			subTest: "simple PodEnvironmentConfigMap",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentConfigMap: testPodEnvironmentConfigMapName,
				},
			},
			envVars: []v1.EnvVar{
				{
					Name:  "foo",
					Value: "bar",
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

func TestPodEnvironmentSecretVariables(t *testing.T) {
	testName := "TestPodEnvironmentSecretVariables"
	tests := []struct {
		subTest  string
		opConfig config.Config
		envVars  []v1.EnvVar
		hash     string
		err      error
	}{
		{
			subTest: "no PodEnvironmentSecretName",
			envVars: []v1.EnvVar{},
		},
		{
			subTest: "missing PodEnvironmentSecretName secret",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecretName: "unknown",
				},
			},
			err: fmt.Errorf("could not read Secret PodEnvironmentSecretName: NotFound"),
		},
		{
			subTest: "only PodEnvironmentSecretName",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecretName: testPodEnvironmentSecretName,
				},
			},
			envVars: []v1.EnvVar{
				{
					Name: "minio_access_key",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "minio_access_key",
						},
					},
				},
				{
					Name: "minio_secret_key",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "minio_secret_key",
						},
					},
				},
			},
			hash: "5d04eafffa6827bb7814cb67c75ab7428cd0dc3ffed5c89d5ae7ee40ca8ba0f3",
		},
		{
			subTest: "both PodEnvironmentSecretName and PodEnvironmentSecretKeys",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecretName: testPodEnvironmentSecretName,
					PodEnvironmentSecretKeys: map[string]string{
						"minio_access_key": "AWS_S3_ACCESS_KEY",
						"minio_secret_key": "AWS_S3_SECRET_KEY",
					},
				},
			},
			envVars: []v1.EnvVar{
				{
					Name: "AWS_S3_ACCESS_KEY",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "minio_access_key",
						},
					},
				},
				{
					Name: "AWS_S3_SECRET_KEY",
					ValueFrom: &v1.EnvVarSource{
						SecretKeyRef: &v1.SecretKeySelector{
							LocalObjectReference: v1.LocalObjectReference{
								Name: testPodEnvironmentSecretName,
							},
							Key: "minio_secret_key",
						},
					},
				},
			},
			hash: "94999cefc6ae2f6c8f715231ae9325251adc78403625308e4c2d6e375aa3ff7a",
		},
		{
			subTest: "secret key in PodEnvironmentSecretKeys missing in PodEnvironmentSecretName secret",
			opConfig: config.Config{
				Resources: config.Resources{
					PodEnvironmentSecretName: testPodEnvironmentSecretName,
					PodEnvironmentSecretKeys: map[string]string{
						"unknown": "BAILOUT",
					},
				},
			},
			err: fmt.Errorf("could not read Secret key unknown (present in PodEnvironmentSecretKeys)"),
		},
	}
	for _, tt := range tests {
		c := newMockCluster(tt.opConfig)
		vars, hash, err := c.getPodEnvironmentSecretVariables()
		if !reflect.DeepEqual(vars, tt.envVars) {
			t.Errorf("%s %s: expected `%v` but got `%v`",
				testName, tt.subTest, tt.envVars, vars)
		}
		if fmt.Sprintf("%x", hash) != tt.hash {
			t.Errorf("%s %s: hash differs `%x` from expected `%s`",
				testName, tt.subTest, hash, tt.hash)
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
