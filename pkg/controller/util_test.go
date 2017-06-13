package controller

import (
	"testing"

	"k8s.io/client-go/pkg/api/v1"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
)

func newMockController() *Controller {
	controller := NewController(&Config{}, &config.Config{})
	controller.opConfig.ClusterNameLabel = "cluster-name"
	controller.opConfig.Workers = 4
	return controller
}

var mockController = newMockController()

func TestPodClusterName(t *testing.T) {
	var testTable = []struct {
		in *v1.Pod
		expected spec.NamespacedName
	}{
		{
			&v1.Pod{},
			spec.NamespacedName{},
		},
		{
			&v1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Namespace: v1.NamespaceDefault,
					Labels:  map[string]string{
							mockController.opConfig.ClusterNameLabel: "testcluster",
						},
					},
			},
			spec.NamespacedName{v1.NamespaceDefault, "testcluster"},
		},
	}
	for _, test:= range(testTable) {
		resp := mockController.podClusterName(test.in)
		if resp != test.expected {
			t.Errorf("expected response %v does not match the actual %v", test.expected, resp)
		}
	}
}

func TestClusterWorkerID(t *testing.T) {
	var testTable = []struct {
		in spec.NamespacedName
		expected uint32
	}{
		{
			in: spec.NamespacedName{"foo", "bar"},
			expected: 2,
		},
		{
			in: spec.NamespacedName{"default", "testcluster"},
			expected: 3,
		},
	}
	for _, test := range(testTable) {
		resp := mockController.clusterWorkerID(test.in)
		if resp != test.expected {
			t.Errorf("expected response %v does not match the actual %v", test.expected, resp)
		}
	}
}
