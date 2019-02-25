package controller

import (
	"testing"

	"github.com/zalando/postgres-operator/pkg/spec"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	readyLabel = "lifecycle-status"
	readyValue = "ready"
)

func initializeController() *Controller {
	var c = NewController(&spec.ControllerConfig{})
	c.opConfig.NodeReadinessLabel = map[string]string{readyLabel: readyValue}
	return c
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

var c = initializeController()

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
		if isReady := c.nodeIsReady(tt.in); isReady != tt.out {
			t.Errorf("%s: expected response %t doesn't match the actual %t for the node %#v",
				testName, tt.out, isReady, tt.in)
		}
	}
}
