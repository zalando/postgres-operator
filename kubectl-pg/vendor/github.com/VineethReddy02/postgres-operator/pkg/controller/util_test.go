package controller

import (
	"fmt"
	"reflect"
	"testing"

	b64 "encoding/base64"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testInfrastructureRolesSecretName = "infrastructureroles-test"
)

func newMockController() *Controller {
	controller := NewController(&spec.ControllerConfig{})
	controller.opConfig.ClusterNameLabel = "cluster-name"
	controller.opConfig.InfrastructureRolesSecretName =
		spec.NamespacedName{Namespace: v1.NamespaceDefault, Name: testInfrastructureRolesSecretName}
	controller.opConfig.Workers = 4
	controller.KubeClient = k8sutil.NewMockKubernetesClient()
	return controller
}

var mockController = newMockController()

func TestPodClusterName(t *testing.T) {
	var testTable = []struct {
		in       *v1.Pod
		expected spec.NamespacedName
	}{
		{
			&v1.Pod{},
			spec.NamespacedName{},
		},
		{
			&v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Labels: map[string]string{
						mockController.opConfig.ClusterNameLabel: "testcluster",
					},
				},
			},
			spec.NamespacedName{Namespace: v1.NamespaceDefault, Name: "testcluster"},
		},
	}
	for _, test := range testTable {
		resp := mockController.podClusterName(test.in)
		if resp != test.expected {
			t.Errorf("expected response %v does not match the actual %v", test.expected, resp)
		}
	}
}

func TestClusterWorkerID(t *testing.T) {
	var testTable = []struct {
		in       spec.NamespacedName
		expected uint32
	}{
		{
			in:       spec.NamespacedName{Namespace: "foo", Name: "bar"},
			expected: 0,
		},
		{
			in:       spec.NamespacedName{Namespace: "default", Name: "testcluster"},
			expected: 1,
		},
	}
	for _, test := range testTable {
		resp := mockController.clusterWorkerID(test.in)
		if resp != test.expected {
			t.Errorf("expected response %v does not match the actual %v", test.expected, resp)
		}
	}
}

func TestGetInfrastructureRoles(t *testing.T) {
	var testTable = []struct {
		secretName    spec.NamespacedName
		expectedRoles map[string]spec.PgUser
		expectedError error
	}{
		{
			spec.NamespacedName{},
			nil,
			nil,
		},
		{
			spec.NamespacedName{Namespace: v1.NamespaceDefault, Name: "null"},
			nil,
			fmt.Errorf(`could not get infrastructure roles secret: NotFound`),
		},
		{
			spec.NamespacedName{Namespace: v1.NamespaceDefault, Name: testInfrastructureRolesSecretName},
			map[string]spec.PgUser{
				"testrole": {
					Name:     "testrole",
					Origin:   spec.RoleOriginInfrastructure,
					Password: "testpassword",
					MemberOf: []string{"testinrole"},
				},
				"foobar": {
					Name:     "foobar",
					Origin:   spec.RoleOriginInfrastructure,
					Password: b64.StdEncoding.EncodeToString([]byte("password")),
					MemberOf: nil,
				},
			},
			nil,
		},
	}
	for _, test := range testTable {
		roles, err := mockController.getInfrastructureRoles(&test.secretName)
		if err != test.expectedError {
			if err != nil && test.expectedError != nil && err.Error() == test.expectedError.Error() {
				continue
			}
			t.Errorf("expected error '%v' does not match the actual error '%v'", test.expectedError, err)
		}
		if !reflect.DeepEqual(roles, test.expectedRoles) {
			t.Errorf("expected roles output %v does not match the actual %v", test.expectedRoles, roles)
		}
	}
}
