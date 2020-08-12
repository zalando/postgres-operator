package controller

import (
	"testing"

	"github.com/zalando/postgres-operator/pkg/spec"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	readyLabel = "lifecycle-status"
	readyValue = "ready"
)

func newNodeTestController() *Controller {
	var controller = NewController(&spec.ControllerConfig{}, "node-test")
	controller.opConfig.NodeReadinessLabel = map[string]string{readyLabel: readyValue}
	return controller
}

func makeNode(labels map[string]string, isSchedulable bool) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: v1.NamespaceDefault,
			Labels:    labels,
		},
		Spec: v1.NodeSpec{
			Unschedulable: !isSchedulable,
		},
	}
}

var nodeTestController = newNodeTestController()

func TestNodeIsReady(t *testing.T) {
	testName := "TestNodeIsReady"
	var testTable = []struct {
		in  *v1.Node
		out bool
	}{
		{
			in:  makeNode(map[string]string{"foo": "bar"}, true),
			out: true,
		},
		{
			in:  makeNode(map[string]string{"foo": "bar"}, false),
			out: false,
		},
		{
			in:  makeNode(map[string]string{readyLabel: readyValue}, false),
			out: true,
		},
		{
			in:  makeNode(map[string]string{"foo": "bar", "master": "true"}, false),
			out: true,
		},
	}
	for _, tt := range testTable {
		if isReady := nodeTestController.nodeIsReady(tt.in); isReady != tt.out {
			t.Errorf("%s: expected response %t doesn't match the actual %t for the node %#v",
				testName, tt.out, isReady, tt.in)
		}
	}
}
