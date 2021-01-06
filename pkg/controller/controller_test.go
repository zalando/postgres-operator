package controller

import (
	"github.com/zalando/postgres-operator/pkg/spec"
	"reflect"
	"testing"
)

var target = map[string]string{"max_instances": "10", "watched_namespace": "test-namespace-name", "pod_terminate_grace_period": "1h", "some_property": "foo=bar", "symlink_secret":"value_of_symlink_file"}

func newFileConfigTestController() *Controller {
	var controller = NewController(&spec.ControllerConfig{}, "fileconfig-test")
	controller.opConfig.OverrideConfigDirectory = []string{"../../test-resources/config-dir-1", "../../test-resources/config-dir-does-not-exist", "../../test-resources/config-dir-2"}
	return controller
}

// Test for success
func TestReadFileConfig(t *testing.T) {
	nodeTestController := newFileConfigTestController()

	actual := nodeTestController.readFileConfig(nodeTestController.opConfig.OverrideConfigDirectory)

	if actual == nil {
		t.Error("file config map is nil")
	}

	eq := reflect.DeepEqual(target, actual)

	if !eq {
		t.Errorf("actual != target; actual: %s; target: %s", actual, target)
	}
}

// TODO: Add tests for failure
