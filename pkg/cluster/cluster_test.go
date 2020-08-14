package cluster

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/sirupsen/logrus"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/teams"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

const (
	superUserName       = "postgres"
	replicationUserName = "standby"
)

var logger = logrus.New().WithField("test", "cluster")
var eventRecorder = record.NewFakeRecorder(1)

var cl = New(
	Config{
		OpConfig: config.Config{
			PodManagementPolicy: "ordered_ready",
			ProtectedRoles:      []string{"admin"},
			Auth: config.Auth{
				SuperUsername:       superUserName,
				ReplicationUsername: replicationUserName,
			},
			Resources: config.Resources{
				DownscalerAnnotations: []string{"downscaler/*"},
			},
		},
	},
	k8sutil.NewMockKubernetesClient(),
	acidv1.Postgresql{ObjectMeta: metav1.ObjectMeta{Name: "acid-test", Namespace: "test", Annotations: map[string]string{"downscaler/downtime_replicas": "0"}}},
	logger,
	eventRecorder,
)

func TestStatefulSetAnnotations(t *testing.T) {
	testName := "CheckStatefulsetAnnotations"
	spec := acidv1.PostgresSpec{
		TeamID: "myapp", NumberOfInstances: 1,
		Resources: acidv1.Resources{
			ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
		},
		Volume: acidv1.Volume{
			Size: "1G",
		},
	}
	ss, err := cl.generateStatefulSet(&spec)
	if err != nil {
		t.Errorf("in %s no statefulset created %v", testName, err)
	}
	if ss != nil {
		annotation := ss.ObjectMeta.GetAnnotations()
		if _, ok := annotation["downscaler/downtime_replicas"]; !ok {
			t.Errorf("in %s respective annotation not found on sts", testName)
		}
	}

}

func TestInitRobotUsers(t *testing.T) {
	testName := "TestInitRobotUsers"
	tests := []struct {
		manifestUsers map[string]acidv1.UserFlags
		infraRoles    map[string]spec.PgUser
		result        map[string]spec.PgUser
		err           error
	}{
		{
			manifestUsers: map[string]acidv1.UserFlags{"foo": {"superuser", "createdb"}},
			infraRoles:    map[string]spec.PgUser{"foo": {Origin: spec.RoleOriginInfrastructure, Name: "foo", Password: "bar"}},
			result:        map[string]spec.PgUser{"foo": {Origin: spec.RoleOriginInfrastructure, Name: "foo", Password: "bar"}},
			err:           nil,
		},
		{
			manifestUsers: map[string]acidv1.UserFlags{"!fooBar": {"superuser", "createdb"}},
			err:           fmt.Errorf(`invalid username: "!fooBar"`),
		},
		{
			manifestUsers: map[string]acidv1.UserFlags{"foobar": {"!superuser", "createdb"}},
			err: fmt.Errorf(`invalid flags for user "foobar": ` +
				`user flag "!superuser" is not alphanumeric`),
		},
		{
			manifestUsers: map[string]acidv1.UserFlags{"foobar": {"superuser1", "createdb"}},
			err: fmt.Errorf(`invalid flags for user "foobar": ` +
				`user flag "SUPERUSER1" is not valid`),
		},
		{
			manifestUsers: map[string]acidv1.UserFlags{"foobar": {"inherit", "noinherit"}},
			err: fmt.Errorf(`invalid flags for user "foobar": ` +
				`conflicting user flags: "NOINHERIT" and "INHERIT"`),
		},
		{
			manifestUsers: map[string]acidv1.UserFlags{"admin": {"superuser"}, superUserName: {"createdb"}},
			infraRoles:    map[string]spec.PgUser{},
			result:        map[string]spec.PgUser{},
			err:           nil,
		},
	}
	for _, tt := range tests {
		cl.Spec.Users = tt.manifestUsers
		cl.pgUsers = tt.infraRoles
		if err := cl.initRobotUsers(); err != nil {
			if tt.err == nil {
				t.Errorf("%s got an unexpected error: %v", testName, err)
			}
			if err.Error() != tt.err.Error() {
				t.Errorf("%s expected error %v, got %v", testName, tt.err, err)
			}
		} else {
			if !reflect.DeepEqual(cl.pgUsers, tt.result) {
				t.Errorf("%s expected: %#v, got %#v", testName, tt.result, cl.pgUsers)
			}
		}
	}
}

type mockOAuthTokenGetter struct {
}

func (m *mockOAuthTokenGetter) getOAuthToken() (string, error) {
	return "", nil
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

// Test adding a member of a product team owning a particular DB cluster
func TestInitHumanUsers(t *testing.T) {

	var mockTeamsAPI mockTeamsAPIClient
	cl.oauthTokenGetter = &mockOAuthTokenGetter{}
	cl.teamsAPIClient = &mockTeamsAPI
	testName := "TestInitHumanUsers"

	// members of a product team are granted superuser rights for DBs of their team
	cl.OpConfig.EnableTeamSuperuser = true

	cl.OpConfig.EnableTeamsAPI = true
	cl.OpConfig.PamRoleName = "zalandos"
	cl.Spec.TeamID = "test"

	tests := []struct {
		existingRoles map[string]spec.PgUser
		teamRoles     []string
		result        map[string]spec.PgUser
	}{
		{
			existingRoles: map[string]spec.PgUser{"foo": {Name: "foo", Origin: spec.RoleOriginTeamsAPI,
				Flags: []string{"NOLOGIN"}}, "bar": {Name: "bar", Flags: []string{"NOLOGIN"}}},
			teamRoles: []string{"foo"},
			result: map[string]spec.PgUser{"foo": {Name: "foo", Origin: spec.RoleOriginTeamsAPI,
				MemberOf: []string{cl.OpConfig.PamRoleName}, Flags: []string{"LOGIN", "SUPERUSER"}},
				"bar": {Name: "bar", Flags: []string{"NOLOGIN"}}},
		},
		{
			existingRoles: map[string]spec.PgUser{},
			teamRoles:     []string{"admin", replicationUserName},
			result:        map[string]spec.PgUser{},
		},
	}

	for _, tt := range tests {
		cl.pgUsers = tt.existingRoles
		mockTeamsAPI.setMembers(tt.teamRoles)
		if err := cl.initHumanUsers(); err != nil {
			t.Errorf("%s got an unexpected error %v", testName, err)
		}

		if !reflect.DeepEqual(cl.pgUsers, tt.result) {
			t.Errorf("%s expects %#v, got %#v", testName, tt.result, cl.pgUsers)
		}
	}
}

type mockTeam struct {
	teamID                  string
	members                 []string
	isPostgresSuperuserTeam bool
}

type mockTeamsAPIClientMultipleTeams struct {
	teams []mockTeam
}

func (m *mockTeamsAPIClientMultipleTeams) TeamInfo(teamID, token string) (tm *teams.Team, err error) {
	for _, team := range m.teams {
		if team.teamID == teamID {
			return &teams.Team{Members: team.members}, nil
		}
	}

	// should not be reached if a slice with teams is populated correctly
	return nil, nil
}

// Test adding members of maintenance teams that get superuser rights for all PG databases
func TestInitHumanUsersWithSuperuserTeams(t *testing.T) {

	var mockTeamsAPI mockTeamsAPIClientMultipleTeams
	cl.oauthTokenGetter = &mockOAuthTokenGetter{}
	cl.teamsAPIClient = &mockTeamsAPI
	cl.OpConfig.EnableTeamSuperuser = false
	testName := "TestInitHumanUsersWithSuperuserTeams"

	cl.OpConfig.EnableTeamsAPI = true
	cl.OpConfig.PamRoleName = "zalandos"

	teamA := mockTeam{
		teamID:                  "postgres_superusers",
		members:                 []string{"postgres_superuser"},
		isPostgresSuperuserTeam: true,
	}

	userA := spec.PgUser{
		Name:     "postgres_superuser",
		Origin:   spec.RoleOriginTeamsAPI,
		MemberOf: []string{cl.OpConfig.PamRoleName},
		Flags:    []string{"LOGIN", "SUPERUSER"},
	}

	teamB := mockTeam{
		teamID:                  "postgres_admins",
		members:                 []string{"postgres_admin"},
		isPostgresSuperuserTeam: true,
	}

	userB := spec.PgUser{
		Name:     "postgres_admin",
		Origin:   spec.RoleOriginTeamsAPI,
		MemberOf: []string{cl.OpConfig.PamRoleName},
		Flags:    []string{"LOGIN", "SUPERUSER"},
	}

	teamTest := mockTeam{
		teamID:                  "test",
		members:                 []string{"test_user"},
		isPostgresSuperuserTeam: false,
	}

	userTest := spec.PgUser{
		Name:     "test_user",
		Origin:   spec.RoleOriginTeamsAPI,
		MemberOf: []string{cl.OpConfig.PamRoleName},
		Flags:    []string{"LOGIN"},
	}

	tests := []struct {
		ownerTeam      string
		existingRoles  map[string]spec.PgUser
		superuserTeams []string
		teams          []mockTeam
		result         map[string]spec.PgUser
	}{
		// case 1: there are two different teams of PG maintainers and one product team
		{
			ownerTeam:      "test",
			existingRoles:  map[string]spec.PgUser{},
			superuserTeams: []string{"postgres_superusers", "postgres_admins"},
			teams:          []mockTeam{teamA, teamB, teamTest},
			result: map[string]spec.PgUser{
				"postgres_superuser": userA,
				"postgres_admin":     userB,
				"test_user":          userTest,
			},
		},
		// case 2: the team of superusers creates a new PG cluster
		{
			ownerTeam:      "postgres_superusers",
			existingRoles:  map[string]spec.PgUser{},
			superuserTeams: []string{"postgres_superusers"},
			teams:          []mockTeam{teamA},
			result: map[string]spec.PgUser{
				"postgres_superuser": userA,
			},
		},
		// case 3: the team owning the cluster is promoted to the maintainers' status
		{
			ownerTeam: "postgres_superusers",
			existingRoles: map[string]spec.PgUser{
				// role with the name exists before  w/o superuser privilege
				"postgres_superuser": {
					Origin:     spec.RoleOriginTeamsAPI,
					Name:       "postgres_superuser",
					Password:   "",
					Flags:      []string{"LOGIN"},
					MemberOf:   []string{cl.OpConfig.PamRoleName},
					Parameters: map[string]string(nil)}},
			superuserTeams: []string{"postgres_superusers"},
			teams:          []mockTeam{teamA},
			result: map[string]spec.PgUser{
				"postgres_superuser": userA,
			},
		},
	}

	for _, tt := range tests {

		mockTeamsAPI.teams = tt.teams

		cl.Spec.TeamID = tt.ownerTeam
		cl.pgUsers = tt.existingRoles
		cl.OpConfig.PostgresSuperuserTeams = tt.superuserTeams

		if err := cl.initHumanUsers(); err != nil {
			t.Errorf("%s got an unexpected error %v", testName, err)
		}

		if !reflect.DeepEqual(cl.pgUsers, tt.result) {
			t.Errorf("%s expects %#v, got %#v", testName, tt.result, cl.pgUsers)
		}
	}
}

func TestPodAnnotations(t *testing.T) {
	testName := "TestPodAnnotations"
	tests := []struct {
		subTest  string
		operator map[string]string
		database map[string]string
		merged   map[string]string
	}{
		{
			subTest:  "No Annotations",
			operator: make(map[string]string),
			database: make(map[string]string),
			merged:   make(map[string]string),
		},
		{
			subTest:  "Operator Config Annotations",
			operator: map[string]string{"foo": "bar"},
			database: make(map[string]string),
			merged:   map[string]string{"foo": "bar"},
		},
		{
			subTest:  "Database Config Annotations",
			operator: make(map[string]string),
			database: map[string]string{"foo": "bar"},
			merged:   map[string]string{"foo": "bar"},
		},
		{
			subTest:  "Both Annotations",
			operator: map[string]string{"foo": "bar"},
			database: map[string]string{"post": "gres"},
			merged:   map[string]string{"foo": "bar", "post": "gres"},
		},
		{
			subTest:  "Database Config overrides Operator Config Annotations",
			operator: map[string]string{"foo": "bar", "global": "foo"},
			database: map[string]string{"foo": "baz", "local": "foo"},
			merged:   map[string]string{"foo": "baz", "global": "foo", "local": "foo"},
		},
	}

	for _, tt := range tests {
		cl.OpConfig.CustomPodAnnotations = tt.operator
		cl.Postgresql.Spec.PodAnnotations = tt.database

		annotations := cl.generatePodAnnotations(&cl.Postgresql.Spec)
		for k, v := range annotations {
			if observed, expected := v, tt.merged[k]; observed != expected {
				t.Errorf("%v expects annotation value %v for key %v, but found %v",
					testName+"/"+tt.subTest, expected, observed, k)
			}
		}
		for k, v := range tt.merged {
			if observed, expected := annotations[k], v; observed != expected {
				t.Errorf("%v expects annotation value %v for key %v, but found %v",
					testName+"/"+tt.subTest, expected, observed, k)
			}
		}
	}
}

func TestServiceAnnotations(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		about                         string
		role                          PostgresRole
		enableMasterLoadBalancerSpec  *bool
		enableMasterLoadBalancerOC    bool
		enableReplicaLoadBalancerSpec *bool
		enableReplicaLoadBalancerOC   bool
		operatorAnnotations           map[string]string
		clusterAnnotations            map[string]string
		expect                        map[string]string
	}{
		//MASTER
		{
			about:                        "Master with no annotations and EnableMasterLoadBalancer disabled on spec and OperatorConfig",
			role:                         "master",
			enableMasterLoadBalancerSpec: &disabled,
			enableMasterLoadBalancerOC:   false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect:                       make(map[string]string),
		},
		{
			about:                        "Master with no annotations and EnableMasterLoadBalancer enabled on spec",
			role:                         "master",
			enableMasterLoadBalancerSpec: &enabled,
			enableMasterLoadBalancerOC:   false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                        "Master with no annotations and EnableMasterLoadBalancer enabled only on operator config",
			role:                         "master",
			enableMasterLoadBalancerSpec: &disabled,
			enableMasterLoadBalancerOC:   true,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect:                       make(map[string]string),
		},
		{
			about:                      "Master with no annotations and EnableMasterLoadBalancer defined only on operator config",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations:         make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                      "Master with cluster annotations and load balancer enabled",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations:         map[string]string{"foo": "bar"},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                        "Master with cluster annotations and load balancer disabled",
			role:                         "master",
			enableMasterLoadBalancerSpec: &disabled,
			enableMasterLoadBalancerOC:   true,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           map[string]string{"foo": "bar"},
			expect:                       map[string]string{"foo": "bar"},
		},
		{
			about:                      "Master with operator annotations and load balancer enabled",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations:        map[string]string{"foo": "bar"},
			clusterAnnotations:         make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                      "Master with operator annotations override default annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			clusterAnnotations: make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                      "Master with cluster annotations override default annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                      "Master with cluster annotations do not override external-dns annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                      "Master with operator annotations do not override external-dns annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			clusterAnnotations:         make(map[string]string),
			operatorAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		// REPLICA
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer disabled on spec and OperatorConfig",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect:                        make(map[string]string),
		},
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer enabled on spec",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &enabled,
			enableReplicaLoadBalancerOC:   false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer enabled only on operator config",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   true,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect:                        make(map[string]string),
		},
		{
			about:                       "Replica with no annotations and EnableReplicaLoadBalancer defined only on operator config",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations:          make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                       "Replica with cluster annotations and load balancer enabled",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations:          map[string]string{"foo": "bar"},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                         "Replica with cluster annotations and load balancer disabled",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   true,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            map[string]string{"foo": "bar"},
			expect:                        map[string]string{"foo": "bar"},
		},
		{
			about:                       "Replica with operator annotations and load balancer enabled",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations:         map[string]string{"foo": "bar"},
			clusterAnnotations:          make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                       "Replica with operator annotations override default annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			clusterAnnotations: make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                       "Replica with cluster annotations override default annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                       "Replica with cluster annotations do not override external-dns annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                       "Replica with operator annotations do not override external-dns annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			clusterAnnotations:          make(map[string]string),
			operatorAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		// COMMON
		{
			about:                       "cluster annotations append to operator annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: false,
			operatorAnnotations:         map[string]string{"foo": "bar"},
			clusterAnnotations:          map[string]string{"post": "gres"},
			expect:                      map[string]string{"foo": "bar", "post": "gres"},
		},
		{
			about:                       "cluster annotations override operator annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: false,
			operatorAnnotations:         map[string]string{"foo": "bar", "post": "gres"},
			clusterAnnotations:          map[string]string{"post": "greSQL"},
			expect:                      map[string]string{"foo": "bar", "post": "greSQL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.about, func(t *testing.T) {
			cl.OpConfig.CustomServiceAnnotations = tt.operatorAnnotations
			cl.OpConfig.EnableMasterLoadBalancer = tt.enableMasterLoadBalancerOC
			cl.OpConfig.EnableReplicaLoadBalancer = tt.enableReplicaLoadBalancerOC
			cl.OpConfig.MasterDNSNameFormat = "{cluster}.{team}.{hostedzone}"
			cl.OpConfig.ReplicaDNSNameFormat = "{cluster}-repl.{team}.{hostedzone}"
			cl.OpConfig.DbHostedZone = "db.example.com"

			cl.Postgresql.Spec.ClusterName = "test"
			cl.Postgresql.Spec.TeamID = "acid"
			cl.Postgresql.Spec.ServiceAnnotations = tt.clusterAnnotations
			cl.Postgresql.Spec.EnableMasterLoadBalancer = tt.enableMasterLoadBalancerSpec
			cl.Postgresql.Spec.EnableReplicaLoadBalancer = tt.enableReplicaLoadBalancerSpec

			got := cl.generateServiceAnnotations(tt.role, &cl.Postgresql.Spec)
			if len(tt.expect) != len(got) {
				t.Errorf("expected %d annotation(s), got %d", len(tt.expect), len(got))
				return
			}
			for k, v := range got {
				if tt.expect[k] != v {
					t.Errorf("expected annotation '%v' with value '%v', got value '%v'", k, tt.expect[k], v)
				}
			}
		})
	}
}

func TestInitSystemUsers(t *testing.T) {
	testName := "Test system users initialization"

	// default cluster without connection pooler
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.ConnectionPoolerUserKeyName]; exist {
		t.Errorf("%s, connection pooler user is present", testName)
	}

	// cluster with connection pooler
	cl.Spec.EnableConnectionPooler = boolToPointer(true)
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.ConnectionPoolerUserKeyName]; !exist {
		t.Errorf("%s, connection pooler user is not present", testName)
	}

	// superuser is not allowed as connection pool user
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "postgres",
	}
	cl.OpConfig.SuperUsername = "postgres"
	cl.OpConfig.ConnectionPooler.User = "pooler"

	cl.initSystemUsers()
	if _, exist := cl.pgUsers["pooler"]; !exist {
		t.Errorf("%s, Superuser is not allowed to be a connection pool user", testName)
	}

	// neither protected users are
	delete(cl.pgUsers, "pooler")
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "admin",
	}
	cl.OpConfig.ProtectedRoles = []string{"admin"}

	cl.initSystemUsers()
	if _, exist := cl.pgUsers["pooler"]; !exist {
		t.Errorf("%s, Protected user are not allowed to be a connection pool user", testName)
	}

	delete(cl.pgUsers, "pooler")
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "standby",
	}

	cl.initSystemUsers()
	if _, exist := cl.pgUsers["pooler"]; !exist {
		t.Errorf("%s, System users are not allowed to be a connection pool user", testName)
	}
}

func TestPreparedDatabases(t *testing.T) {
	testName := "TestDefaultPreparedDatabase"

	cl.Spec.PreparedDatabases = map[string]acidv1.PreparedDatabase{}
	cl.initPreparedDatabaseRoles()

	for _, role := range []string{"acid_test_owner", "acid_test_reader", "acid_test_writer",
		"acid_test_data_owner", "acid_test_data_reader", "acid_test_data_writer"} {
		if _, exist := cl.pgUsers[role]; !exist {
			t.Errorf("%s, default role %q for prepared database not present", testName, role)
		}
	}

	testName = "TestPreparedDatabaseWithSchema"

	cl.Spec.PreparedDatabases = map[string]acidv1.PreparedDatabase{
		"foo": {
			DefaultUsers: true,
			PreparedSchemas: map[string]acidv1.PreparedSchema{
				"bar": {
					DefaultUsers: true,
				},
			},
		},
	}
	cl.initPreparedDatabaseRoles()

	for _, role := range []string{
		"foo_owner", "foo_reader", "foo_writer",
		"foo_owner_user", "foo_reader_user", "foo_writer_user",
		"foo_bar_owner", "foo_bar_reader", "foo_bar_writer",
		"foo_bar_owner_user", "foo_bar_reader_user", "foo_bar_writer_user"} {
		if _, exist := cl.pgUsers[role]; !exist {
			t.Errorf("%s, default role %q for prepared database not present", testName, role)
		}
	}

	roleTests := []struct {
		subTest  string
		role     string
		memberOf string
		admin    string
	}{
		{
			subTest:  "Test admin role of owner",
			role:     "foo_owner",
			memberOf: "",
			admin:    "admin",
		},
		{
			subTest:  "Test writer is a member of reader",
			role:     "foo_writer",
			memberOf: "foo_reader",
			admin:    "foo_owner",
		},
		{
			subTest:  "Test reader LOGIN role",
			role:     "foo_reader_user",
			memberOf: "foo_reader",
			admin:    "foo_owner",
		},
		{
			subTest:  "Test schema owner",
			role:     "foo_bar_owner",
			memberOf: "",
			admin:    "foo_owner",
		},
		{
			subTest:  "Test schema writer LOGIN role",
			role:     "foo_bar_writer_user",
			memberOf: "foo_bar_writer",
			admin:    "foo_bar_owner",
		},
	}

	for _, tt := range roleTests {
		user := cl.pgUsers[tt.role]
		if (tt.memberOf == "" && len(user.MemberOf) > 0) || (tt.memberOf != "" && user.MemberOf[0] != tt.memberOf) {
			t.Errorf("%s, incorrect membership for default role %q. Expected %q, got %q", tt.subTest, tt.role, tt.memberOf, user.MemberOf[0])
		}
		if user.AdminRole != tt.admin {
			t.Errorf("%s, incorrect admin role for default role %q. Expected %q, got %q", tt.subTest, tt.role, tt.admin, user.AdminRole)
		}
	}
}

func TestCompareSpiloConfiguration(t *testing.T) {
	testCases := []struct {
		Config         string
		ExpectedResult bool
	}{
		{
			`{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"100","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
			true,
		},
		{
			`{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"200","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
			true,
		},
		{
			`{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"200","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
			false,
		},
		{
			`{}`,
			false,
		},
		{
			`invalidjson`,
			false,
		},
	}
	refCase := testCases[0]
	for _, testCase := range testCases {
		if result := compareSpiloConfiguration(refCase.Config, testCase.Config); result != testCase.ExpectedResult {
			t.Errorf("expected %v got %v", testCase.ExpectedResult, result)
		}
	}
}

func TestCompareEnv(t *testing.T) {
	testCases := []struct {
		Envs           []v1.EnvVar
		ExpectedResult bool
	}{
		{
			Envs: []v1.EnvVar{
				{
					Name:  "VARIABLE1",
					Value: "value1",
				},
				{
					Name:  "VARIABLE2",
					Value: "value2",
				},
				{
					Name:  "VARIABLE3",
					Value: "value3",
				},
				{
					Name:  "SPILO_CONFIGURATION",
					Value: `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"100","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
				},
			},
			ExpectedResult: true,
		},
		{
			Envs: []v1.EnvVar{
				{
					Name:  "VARIABLE1",
					Value: "value1",
				},
				{
					Name:  "VARIABLE2",
					Value: "value2",
				},
				{
					Name:  "VARIABLE3",
					Value: "value3",
				},
				{
					Name:  "SPILO_CONFIGURATION",
					Value: `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
				},
			},
			ExpectedResult: true,
		},
		{
			Envs: []v1.EnvVar{
				{
					Name:  "VARIABLE4",
					Value: "value4",
				},
				{
					Name:  "VARIABLE2",
					Value: "value2",
				},
				{
					Name:  "VARIABLE3",
					Value: "value3",
				},
				{
					Name:  "SPILO_CONFIGURATION",
					Value: `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
				},
			},
			ExpectedResult: false,
		},
		{
			Envs: []v1.EnvVar{
				{
					Name:  "VARIABLE1",
					Value: "value1",
				},
				{
					Name:  "VARIABLE2",
					Value: "value2",
				},
				{
					Name:  "VARIABLE3",
					Value: "value3",
				},
				{
					Name:  "VARIABLE4",
					Value: "value4",
				},
				{
					Name:  "SPILO_CONFIGURATION",
					Value: `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"100","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
				},
			},
			ExpectedResult: false,
		},
		{
			Envs: []v1.EnvVar{
				{
					Name:  "VARIABLE1",
					Value: "value1",
				},
				{
					Name:  "VARIABLE2",
					Value: "value2",
				},
				{
					Name:  "SPILO_CONFIGURATION",
					Value: `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"100","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`,
				},
			},
			ExpectedResult: false,
		},
	}
	refCase := testCases[0]
	for _, testCase := range testCases {
		if result := compareEnv(refCase.Envs, testCase.Envs); result != testCase.ExpectedResult {
			t.Errorf("expected %v got %v", testCase.ExpectedResult, result)
		}
	}
}
