package controller

import (
	"fmt"
	"strings"
	"testing"
	"time"

	logrustest "github.com/sirupsen/logrus/hooks/test"
	"github.com/zalando/postgres-operator/v2/pkg/spec"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	readyLabel = "lifecycle-status"
	readyValue = "ready"
)

func newNodeTestController() *Controller {
	var controller = NewController(&spec.ControllerConfig{}, "node-test")
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
		in             *v1.Node
		out            bool
		readinessLabel map[string]string
	}{
		{
			in:             makeNode(map[string]string{"foo": "bar"}, true),
			out:            true,
			readinessLabel: map[string]string{readyLabel: readyValue},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar"}, false),
			out:            false,
			readinessLabel: map[string]string{readyLabel: readyValue},
		},
		{
			in:             makeNode(map[string]string{readyLabel: readyValue}, false),
			out:            true,
			readinessLabel: map[string]string{readyLabel: readyValue},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar", "master": "true"}, false),
			out:            true,
			readinessLabel: map[string]string{readyLabel: readyValue},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar", "master": "true"}, false),
			out:            true,
			readinessLabel: map[string]string{readyLabel: readyValue},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar"}, true),
			out:            true,
			readinessLabel: map[string]string{},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar"}, false),
			out:            false,
			readinessLabel: map[string]string{},
		},
		{
			in:             makeNode(map[string]string{readyLabel: readyValue}, false),
			out:            false,
			readinessLabel: map[string]string{},
		},
		{
			in:             makeNode(map[string]string{"foo": "bar", "master": "true"}, false),
			out:            true,
			readinessLabel: map[string]string{},
		},
	}
	for _, tt := range testTable {
		nodeTestController.opConfig.NodeReadinessLabel = tt.readinessLabel
		if isReady := nodeTestController.nodeIsReady(tt.in); isReady != tt.out {
			t.Errorf("%s: expected response %t does not match the actual %t for the node %#v",
				testName, tt.out, isReady, tt.in)
		}
	}
}

// TestMoveMasterPodsOffNodeRetriesOnError ensures a failed attempt to move
// master pods off a node is retried rather than aborting the whole retry
// loop on the first error.
func TestMoveMasterPodsOffNodeRetriesOnError(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	clientSet.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("could not list pods")
	})

	controller := newNodeTestController()
	controller.KubeClient.PodsGetter = clientSet.CoreV1()
	// timeout == the retry interval hardcoded in moveMasterPodsOffNode, so
	// the single retry attempt resolves synchronously without a real sleep.
	controller.opConfig.MasterPodMoveTimeout = &metav1.Duration{Duration: 1 * time.Minute}

	logger, hook := logrustest.NewNullLogger()
	controller.logger = logger.WithField("pkg", "controller")

	controller.moveMasterPodsOffNode(makeNode(map[string]string{}, false))

	lastEntry := hook.LastEntry()
	if lastEntry == nil {
		t.Fatal("expected moveMasterPodsOffNode to log a warning")
	}
	if !strings.Contains(lastEntry.Message, "still failing after") {
		t.Errorf("expected the retry loop to run out of attempts instead of aborting on the first error, got log message: %q", lastEntry.Message)
	}
}
