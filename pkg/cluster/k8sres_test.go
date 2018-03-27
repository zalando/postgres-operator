package cluster

import (
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"testing"
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
		}, k8sutil.KubernetesClient{}, spec.Postgresql{}, logger)

	testName := "TestCreateLoadBalancerLogic"
	tests := []struct {
		subtest  string
		role     PostgresRole
		spec     *spec.PostgresSpec
		opConfig config.Config
		result   bool
	}{
		{
			subtest:  "new format, load balancer is enabled for replica",
			role:     Replica,
			spec:     &spec.PostgresSpec{EnableReplicaLoadBalancer: True()},
			opConfig: config.Config{},
			result:   true,
		},
		{
			subtest:  "new format, load balancer is disabled for replica",
			role:     Replica,
			spec:     &spec.PostgresSpec{EnableReplicaLoadBalancer: False()},
			opConfig: config.Config{},
			result:   false,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &spec.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: true},
			result:   true,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &spec.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: false},
			result:   false,
		},
		{
			subtest:  "old format, load balancer is enabled for replica",
			role:     Replica,
			spec:     &spec.PostgresSpec{ReplicaLoadBalancer: True()},
			opConfig: config.Config{},
			result:   true,
		},
		{
			subtest: "old format has priority",
			role:    Replica,
			spec: &spec.PostgresSpec{
				ReplicaLoadBalancer:       True(),
				EnableReplicaLoadBalancer: False(),
			},
			opConfig: config.Config{},
			result:   true,
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
