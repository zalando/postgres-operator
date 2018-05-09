package controller

import (
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"reflect"
	"testing"
)

var (
	True  bool = true
	False bool = false
)

func TestMergeDeprecatedPostgreSQLSpecParameters(t *testing.T) {
	c := NewController(&spec.ControllerConfig{})

	tests := []struct {
		name  string
		in    *spec.PostgresSpec
		out   *spec.PostgresSpec
		error string
	}{
		{
			"Check that old parameters propagate values to the new ones",
			&spec.PostgresSpec{UseLoadBalancer: &True, ReplicaLoadBalancer: &True},
			&spec.PostgresSpec{UseLoadBalancer: &True, ReplicaLoadBalancer: &True,
				EnableMasterLoadBalancer: &True, EnableReplicaLoadBalancer: &True},
			"New parameters should be set from the values of old ones",
		},
		{
			"Check that new parameters are not set when both old and new ones are present",
			&spec.PostgresSpec{UseLoadBalancer: &True, EnableReplicaLoadBalancer: &True},
			&spec.PostgresSpec{UseLoadBalancer: &True, EnableReplicaLoadBalancer: &True},
			"New parameters should remain unchanged when both old and new are present",
		},
	}
	for _, tt := range tests {
		result := c.mergeDeprecatedPostgreSQLSpecParameters(tt.in)
		if !reflect.DeepEqual(result, tt.out) {
			t.Errorf("%s: %v", tt.name, tt.error)
		}
	}
}
