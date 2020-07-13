package controller

import (
	"reflect"
	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	True  = true
	False = false
)

func newPostgresqlTestController() *Controller {
	controller := NewController(&spec.ControllerConfig{}, "postgresql-test")
	return controller
}

var postgresqlTestController = newPostgresqlTestController()

func TestControllerOwnershipOnPostgresql(t *testing.T) {
	tests := []struct {
		name  string
		pg    *acidv1.Postgresql
		owned bool
		error string
	}{
		{
			"Postgres cluster with defined ownership of mocked controller",
			&acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"acid.zalan.do/controller": "postgresql-test"},
				},
			},
			True,
			"Postgres cluster should be owned by operator, but controller says no",
		},
		{
			"Postgres cluster with defined ownership of another controller",
			&acidv1.Postgresql{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"acid.zalan.do/controller": "stups-test"},
				},
			},
			False,
			"Postgres cluster should be owned by another operator, but controller say yes",
		},
		{
			"Test Postgres cluster without defined ownership",
			&acidv1.Postgresql{},
			False,
			"Postgres cluster should be owned by operator with empty controller ID, but controller says yes",
		},
	}
	for _, tt := range tests {
		if postgresqlTestController.hasOwnership(tt.pg) != tt.owned {
			t.Errorf("%s: %v", tt.name, tt.error)
		}
	}
}

func TestMergeDeprecatedPostgreSQLSpecParameters(t *testing.T) {
	tests := []struct {
		name  string
		in    *acidv1.PostgresSpec
		out   *acidv1.PostgresSpec
		error string
	}{
		{
			"Check that old parameters propagate values to the new ones",
			&acidv1.PostgresSpec{UseLoadBalancer: &True, ReplicaLoadBalancer: &True},
			&acidv1.PostgresSpec{UseLoadBalancer: nil, ReplicaLoadBalancer: nil,
				EnableMasterLoadBalancer: &True, EnableReplicaLoadBalancer: &True},
			"New parameters should be set from the values of old ones",
		},
		{
			"Check that new parameters are not set when both old and new ones are present",
			&acidv1.PostgresSpec{UseLoadBalancer: &True, EnableMasterLoadBalancer: &False},
			&acidv1.PostgresSpec{UseLoadBalancer: nil, EnableMasterLoadBalancer: &False},
			"New parameters should remain unchanged when both old and new are present",
		},
	}
	for _, tt := range tests {
		result := postgresqlTestController.mergeDeprecatedPostgreSQLSpecParameters(tt.in)
		if !reflect.DeepEqual(result, tt.out) {
			t.Errorf("%s: %v", tt.name, tt.error)
		}
	}
}
