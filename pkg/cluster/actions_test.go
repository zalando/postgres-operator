package cluster

import (
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

var updateCalled = 0

type mockSecretGetter struct{}

type testSecret struct {
	v1core.SecretInterface
}

func (c *testSecret) Update(secret *v1.Secret) (*v1.Secret, error) {
	updateCalled += 1
	return secret, nil
}

func (c *mockSecretGetter) Secrets(namespace string) v1core.SecretInterface {
	return &testSecret{}
}

func getMockK8sClient() k8sutil.KubernetesClient {
	return k8sutil.KubernetesClient{
		SecretsGetter: &mockSecretGetter{},
	}
}

func generateMockCluster(origin spec.RoleOrigin) *Cluster {
	cluster := Cluster{
		KubeClient: getMockK8sClient(),
		pgUsers: map[string]spec.PgUser{
			"testuser": {
				Password: "123456",
				Origin:   origin,
			},
		},
	}

	cluster.logger = logger.
		WithField("pkg", "cluster").
		WithField("cluster-name", cluster.clusterName())

	return &cluster
}

func TestSecretValidation(t *testing.T) {
	secret := v1.Secret{Data: make(map[string][]byte)}

	tests := []struct {
		name     string
		action   Action
		checkErr func(error) bool
		errMsg   string
	}{
		{
			"Username for secret to update cannot be empty",
			NewUpdateSecret("", &secret, nil),
			func(err error) bool { return err == nil },
			"Empty username did not cause an error, expected %v, given %v",
		},
	}
	for _, tt := range tests {
		result := tt.action.Validate()
		if tt.checkErr(result) {
			t.Errorf("%s: %v", tt.name, tt.errMsg)
		}
	}
}

func TestSecretApply(t *testing.T) {
	tests := []struct {
		name     string
		action   Action
		checkErr func(error) bool
		errMsg   string
	}{
		{
			"Secret has been replaced with UpdateSecret for infrastructure role",
			NewUpdateSecret("testuser", &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testsecret",
				},
				Data: make(map[string][]byte),
			}, generateMockCluster(spec.RoleOriginInfrastructure)),
			func(err error) bool { return err != nil || updateCalled != 1 },
			"Update K8S client method was never called",
		},
		{
			"Secret has been synced with UpdateSecret for non infrastructure role",
			NewUpdateSecret("testuser", &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testsecret",
				},
				Data: make(map[string][]byte),
			}, generateMockCluster(spec.RoleOriginSystem)),
			func(err error) bool { return err != nil || updateCalled != 0 },
			"Update K8S client method was called, but should not be",
		},
	}
	for _, tt := range tests {
		result := tt.action.Apply()
		if tt.checkErr(result) {
			t.Errorf("%s: %v", tt.name, tt.errMsg)
		}

		// reset counters
		updateCalled = 0
	}
}
