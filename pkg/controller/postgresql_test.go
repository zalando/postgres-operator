package controller

import (
	"reflect"
	"testing"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"

	acidv1 "github.com/zalando-incubator/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	True   = true
	False  = false
	logger = logrus.New().WithField("test", "cluster")
)

const (
	superUserName       = "postgres"
	replicationUserName = "standby"
)

var mockTeamsAPI mockTeamsAPIClient

type mockOAuthTokenGetter struct {
	cluster.OAuthTokenGetter
}

type mockClusterGenerator struct {
	ClusterGenerator
}

type NotFoundError struct{}

func (e *NotFoundError) Error() string {
	return "error"
}

func (e *NotFoundError) Status() metav1.Status {
	return metav1.Status{
		Reason: metav1.StatusReasonNotFound,
	}
}

func (m *mockClusterGenerator) addCluster(ctrl *Controller,
	lg *logrus.Entry, clusterName spec.NamespacedName, pgSpec *acidv1.Postgresql) *cluster.Cluster {

	mockTeamsAPI.setMembers([]string{"test-user"})
	cl := cluster.New(ctrl.makeClusterConfig(), ctrl.KubeClient, *pgSpec, lg)
	cl.SetTeamsAPIClient(&mockTeamsAPI)
	cl.SetOAuthTokenGetter(&mockOAuthTokenGetter{})
	cl.Spec.TeamID = "test-team"
	return cl
}

type mockTeamsAPIClient struct {
	members []string
}

func (m *mockTeamsAPIClient) TeamInfo(teamID, token string) (tm *teams.Team, err error) {
	return &teams.Team{Members: m.members}, nil
}

func (m *mockTeamsAPIClient) setMembers(members []string) {
	m.members = members
}

type mockNoSecretGetter struct {
}

type testNoSecret struct {
	v1core.SecretInterface
}

func (c *testNoSecret) Get(name string, options metav1.GetOptions) (*v1.Secret, error) {
	return nil, &NotFoundError{}

}

func (c *mockNoSecretGetter) Secrets(namespace string) v1core.SecretInterface {
	return &testNoSecret{}
}

func mockKubernetesClient() k8sutil.KubernetesClient {
	return k8sutil.KubernetesClient{
		SecretsGetter: &mockNoSecretGetter{},
	}
}

func TestMergeDeprecatedPostgreSQLSpecParameters(t *testing.T) {
	c := NewController(&spec.ControllerConfig{})
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
		result := c.mergeDeprecatedPostgreSQLSpecParameters(tt.in)
		if !reflect.DeepEqual(result, tt.out) {
			t.Errorf("%s: %v", tt.name, tt.error)
		}
	}
}

func TestGeneratePlan(t *testing.T) {
	c := NewController(&spec.ControllerConfig{})
	c.opConfig.SuperUsername = "test-superuser"
	c.opConfig.SecretNameTemplate = "secret"
	c.clusterFactory = &mockClusterGenerator{}
	c.KubeClient = mockKubernetesClient()

	tests := []struct {
		name     string
		in       ClusterEvent
		contains cluster.Plan
		error    string
	}{
		{
			"Cluster add produces plan with a CreateSecret",
			ClusterEvent{
				EventType: EventAdd,
				NewSpec: &acidv1.Postgresql{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "TestCluster",
						Namespace: "TestNamespace",
					},
				},
			},
			[]cluster.Action{cluster.CreateSecret{}, cluster.CreateSecret{}},
			"A plan for a new cluster should create secrets",
		},
	}
	for _, tt := range tests {
		result := c.generatePlan(tt.in)
		for idx, output := range result {
			output := reflect.TypeOf(output)
			expected := reflect.TypeOf(tt.contains[idx])
			if output != expected {
				t.Errorf("%s: %v", tt.name, tt.error)
			}
		}
	}
}
