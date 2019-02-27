package cluster

import (
	"k8s.io/api/core/v1"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
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
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger)

	testName := "TestCreateLoadBalancerLogic"
	tests := []struct {
		subtest  string
		role     PostgresRole
		spec     *acidv1.PostgresSpec
		opConfig config.Config
		result   bool
	}{
		{
			subtest:  "new format, load balancer is enabled for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: True()},
			opConfig: config.Config{},
			result:   true,
		},
		{
			subtest:  "new format, load balancer is disabled for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: False()},
			opConfig: config.Config{},
			result:   false,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: true},
			result:   true,
		},
		{
			subtest:  "new format, load balancer isn't specified for replica",
			role:     Replica,
			spec:     &acidv1.PostgresSpec{EnableReplicaLoadBalancer: nil},
			opConfig: config.Config{EnableReplicaLoadBalancer: false},
			result:   false,
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

func TestShmVolume(t *testing.T) {
	testName := "TestShmVolume"
	tests := []struct {
		subTest string
		podSpec *v1.PodSpec
		shmPos  int
	}{
		{
			subTest: "empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{},
				Containers: []v1.Container{
					v1.Container{
						VolumeMounts: []v1.VolumeMount{},
					},
				},
			},
			shmPos: 0,
		},
		{
			subTest: "non empty PodSpec",
			podSpec: &v1.PodSpec{
				Volumes: []v1.Volume{v1.Volume{}},
				Containers: []v1.Container{
					v1.Container{
						VolumeMounts: []v1.VolumeMount{
							v1.VolumeMount{},
						},
					},
				},
			},
			shmPos: 1,
		},
	}
	for _, tt := range tests {
		addShmVolume(tt.podSpec)

		volumeName := tt.podSpec.Volumes[tt.shmPos].Name
		volumeMountName := tt.podSpec.Containers[0].VolumeMounts[tt.shmPos].Name

		if volumeName != constants.ShmVolumeName {
			t.Errorf("%s %s: Expected volume %s was not created, have %s instead",
				testName, tt.subTest, constants.ShmVolumeName, volumeName)
		}
		if volumeMountName != constants.ShmVolumeName {
			t.Errorf("%s %s: Expected mount %s was not created, have %s instead",
				testName, tt.subTest, constants.ShmVolumeName, volumeMountName)
		}
	}
}
