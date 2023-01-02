package cluster

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/teams"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
)

const (
	superUserName       = "postgres"
	replicationUserName = "standby"
	exampleSpiloConfig  = `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"ttl":30,"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_connections":"100","max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`
	spiloConfigDiff     = `{"postgresql":{"bin_dir":"/usr/lib/postgresql/12/bin","parameters":{"autovacuum_analyze_scale_factor":"0.1"},"pg_hba":["hostssl all all 0.0.0.0/0 md5","host all all 0.0.0.0/0 md5"]},"bootstrap":{"initdb":[{"auth-host":"md5"},{"auth-local":"trust"},"data-checksums",{"encoding":"UTF8"},{"locale":"en_US.UTF-8"}],"users":{"test":{"password":"","options":["CREATEDB","NOLOGIN"]}},"dcs":{"loop_wait":10,"retry_timeout":10,"maximum_lag_on_failover":33554432,"postgresql":{"parameters":{"max_locks_per_transaction":"64","max_worker_processes":"4"}}}}}`
)

var logger = logrus.New().WithField("test", "cluster")
var eventRecorder = record.NewFakeRecorder(1)

var cl = New(
	Config{
		OpConfig: config.Config{
			PodManagementPolicy: "ordered_ready",
			ProtectedRoles:      []string{"admin", "cron_admin", "part_man"},
			Auth: config.Auth{
				SuperUsername:        superUserName,
				ReplicationUsername:  replicationUserName,
				AdditionalOwnerRoles: []string{"cron_admin", "part_man"},
			},
			Resources: config.Resources{
				DownscalerAnnotations: []string{"downscaler/*"},
			},
		},
	},
	k8sutil.NewMockKubernetesClient(),
	acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "acid-test",
			Namespace:   "test",
			Annotations: map[string]string{"downscaler/downtime_replicas": "0"},
		},
	},
	logger,
	eventRecorder,
)

func TestStatefulSetAnnotations(t *testing.T) {
	spec := acidv1.PostgresSpec{
		TeamID: "myapp", NumberOfInstances: 1,
		Resources: &acidv1.Resources{
			ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
		},
		Volume: acidv1.Volume{
			Size: "1G",
		},
	}
	ss, err := cl.generateStatefulSet(&spec)
	if err != nil {
		t.Errorf("in %s no statefulset created %v", t.Name(), err)
	}
	if ss != nil {
		annotation := ss.ObjectMeta.GetAnnotations()
		if _, ok := annotation["downscaler/downtime_replicas"]; !ok {
			t.Errorf("in %s respective annotation not found on sts", t.Name())
		}
	}
}

func TestStatefulSetUpdateWithEnv(t *testing.T) {
	oldSpec := &acidv1.PostgresSpec{
		TeamID: "myapp", NumberOfInstances: 1,
		Resources: &acidv1.Resources{
			ResourceRequests: acidv1.ResourceDescription{CPU: "1", Memory: "10"},
			ResourceLimits:   acidv1.ResourceDescription{CPU: "1", Memory: "10"},
		},
		Volume: acidv1.Volume{
			Size: "1G",
		},
	}
	oldSS, err := cl.generateStatefulSet(oldSpec)
	if err != nil {
		t.Errorf("in %s no StatefulSet created %v", t.Name(), err)
	}

	newSpec := oldSpec.DeepCopy()
	newSS, err := cl.generateStatefulSet(newSpec)
	if err != nil {
		t.Errorf("in %s no StatefulSet created %v", t.Name(), err)
	}

	if !reflect.DeepEqual(oldSS, newSS) {
		t.Errorf("in %s StatefulSet's must be equal", t.Name())
	}

	newSpec.Env = []v1.EnvVar{
		{
			Name:  "CUSTOM_ENV_VARIABLE",
			Value: "data",
		},
	}
	newSS, err = cl.generateStatefulSet(newSpec)
	if err != nil {
		t.Errorf("in %s no StatefulSet created %v", t.Name(), err)
	}

	if reflect.DeepEqual(oldSS, newSS) {
		t.Errorf("in %s StatefulSet's must be not equal", t.Name())
	}
}

func TestInitRobotUsers(t *testing.T) {
	tests := []struct {
		manifestUsers map[string]acidv1.UserFlags
		infraRoles    map[string]spec.PgUser
		result        map[string]spec.PgUser
		err           error
	}{
		{
			manifestUsers: map[string]acidv1.UserFlags{"foo": {"superuser", "createdb"}},
			infraRoles:    map[string]spec.PgUser{"foo": {Origin: spec.RoleOriginInfrastructure, Name: "foo", Namespace: cl.Namespace, Password: "bar"}},
			result:        map[string]spec.PgUser{"foo": {Origin: spec.RoleOriginInfrastructure, Name: "foo", Namespace: cl.Namespace, Password: "bar"}},
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
				t.Errorf("%s got an unexpected error: %v", t.Name(), err)
			}
			if err.Error() != tt.err.Error() {
				t.Errorf("%s expected error %v, got %v", t.Name(), tt.err, err)
			}
		} else {
			if !reflect.DeepEqual(cl.pgUsers, tt.result) {
				t.Errorf("%s expected: %#v, got %#v", t.Name(), tt.result, cl.pgUsers)
			}
		}
	}
}

func TestInitAdditionalOwnerRoles(t *testing.T) {
	manifestUsers := map[string]acidv1.UserFlags{"foo_owner": {}, "bar_owner": {}, "app_user": {}}
	expectedUsers := map[string]spec.PgUser{
		"foo_owner": {Origin: spec.RoleOriginManifest, Name: "foo_owner", Namespace: cl.Namespace, Password: "f123", Flags: []string{"LOGIN"}, IsDbOwner: true, MemberOf: []string{"cron_admin", "part_man"}},
		"bar_owner": {Origin: spec.RoleOriginManifest, Name: "bar_owner", Namespace: cl.Namespace, Password: "b123", Flags: []string{"LOGIN"}, IsDbOwner: true, MemberOf: []string{"cron_admin", "part_man"}},
		"app_user":  {Origin: spec.RoleOriginManifest, Name: "app_user", Namespace: cl.Namespace, Password: "a123", Flags: []string{"LOGIN"}, IsDbOwner: false},
	}

	cl.Spec.Databases = map[string]string{"foo_db": "foo_owner", "bar_db": "bar_owner"}
	cl.Spec.Users = manifestUsers

	// this should set IsDbOwner field for manifest users
	if err := cl.initRobotUsers(); err != nil {
		t.Errorf("%s could not init manifest users", t.Name())
	}

	// now assign additional roles to owners
	cl.initAdditionalOwnerRoles()

	// update passwords to compare with result
	for username, existingPgUser := range cl.pgUsers {
		expectedPgUser := expectedUsers[username]
		if !util.IsEqualIgnoreOrder(expectedPgUser.MemberOf, existingPgUser.MemberOf) {
			t.Errorf("%s unexpected membership of user %q: expected member of %#v, got member of %#v",
				t.Name(), username, expectedPgUser.MemberOf, existingPgUser.MemberOf)
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

func (m *mockTeamsAPIClient) TeamInfo(teamID, token string) (tm *teams.Team, statusCode int, err error) {
	if len(m.members) > 0 {
		return &teams.Team{Members: m.members}, http.StatusOK, nil
	}

	// when members are not set handle this as an error for this mock API
	// makes it easier to test behavior when teams API is unavailable
	return nil, http.StatusInternalServerError,
		fmt.Errorf("mocked %d error of mock Teams API for team %q", http.StatusInternalServerError, teamID)
}

func (m *mockTeamsAPIClient) setMembers(members []string) {
	m.members = members
}

// Test adding a member of a product team owning a particular DB cluster
func TestInitHumanUsers(t *testing.T) {
	var mockTeamsAPI mockTeamsAPIClient
	cl.oauthTokenGetter = &mockOAuthTokenGetter{}
	cl.teamsAPIClient = &mockTeamsAPI

	// members of a product team are granted superuser rights for DBs of their team
	cl.OpConfig.EnableTeamSuperuser = true
	cl.OpConfig.EnableTeamsAPI = true
	cl.OpConfig.EnableTeamMemberDeprecation = true
	cl.OpConfig.PamRoleName = "zalandos"
	cl.Spec.TeamID = "test"
	cl.Spec.Users = map[string]acidv1.UserFlags{"bar": []string{}}

	tests := []struct {
		existingRoles map[string]spec.PgUser
		teamRoles     []string
		result        map[string]spec.PgUser
		err           error
	}{
		{
			existingRoles: map[string]spec.PgUser{"foo": {Name: "foo", Origin: spec.RoleOriginTeamsAPI,
				Flags: []string{"LOGIN"}}, "bar": {Name: "bar", Flags: []string{"LOGIN"}}},
			teamRoles: []string{"foo"},
			result: map[string]spec.PgUser{"foo": {Name: "foo", Origin: spec.RoleOriginTeamsAPI,
				MemberOf: []string{cl.OpConfig.PamRoleName}, Flags: []string{"LOGIN", "SUPERUSER"}},
				"bar": {Name: "bar", Flags: []string{"LOGIN"}}},
			err: fmt.Errorf("could not init human users: cannot initialize members for team %q who owns the Postgres cluster: could not get list of team members for team %q: could not get team info for team %q: mocked %d error of mock Teams API for team %q",
				cl.Spec.TeamID, cl.Spec.TeamID, cl.Spec.TeamID, http.StatusInternalServerError, cl.Spec.TeamID),
		},
		{
			existingRoles: map[string]spec.PgUser{},
			teamRoles:     []string{"admin", replicationUserName},
			result:        map[string]spec.PgUser{},
			err:           nil,
		},
	}

	for _, tt := range tests {
		// set pgUsers so that initUsers sets up pgUsersCache with team roles
		cl.pgUsers = tt.existingRoles

		// initUsers calls initHumanUsers which should fail
		// because no members are set for mocked teams API
		if err := cl.initUsers(); err != nil {
			// check that at least team roles are remembered in c.pgUsers
			if len(cl.pgUsers) < len(tt.teamRoles) {
				t.Errorf("%s unexpected size of pgUsers: expected at least %d, got %d", t.Name(), len(tt.teamRoles), len(cl.pgUsers))
			}
			if err.Error() != tt.err.Error() {
				t.Errorf("%s expected error %v, got %v", t.Name(), err, tt.err)
			}
		}

		// set pgUsers again to test initHumanUsers with working teams API
		cl.pgUsers = tt.existingRoles
		mockTeamsAPI.setMembers(tt.teamRoles)
		if err := cl.initHumanUsers(); err != nil {
			t.Errorf("%s got an unexpected error %v", t.Name(), err)
		}

		if !reflect.DeepEqual(cl.pgUsers, tt.result) {
			t.Errorf("%s expects %#v, got %#v", t.Name(), tt.result, cl.pgUsers)
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

func (m *mockTeamsAPIClientMultipleTeams) TeamInfo(teamID, token string) (tm *teams.Team, statusCode int, err error) {
	for _, team := range m.teams {
		if team.teamID == teamID {
			return &teams.Team{Members: team.members}, http.StatusOK, nil
		}
	}

	// when given teamId is not found in teams return StatusNotFound
	// the operator should only return a warning in this case and not error out (#1842)
	return nil, http.StatusNotFound,
		fmt.Errorf("mocked %d error of mock Teams API for team %q", http.StatusNotFound, teamID)
}

// Test adding members of maintenance teams that get superuser rights for all PG databases
func TestInitHumanUsersWithSuperuserTeams(t *testing.T) {
	var mockTeamsAPI mockTeamsAPIClientMultipleTeams
	cl.oauthTokenGetter = &mockOAuthTokenGetter{}
	cl.teamsAPIClient = &mockTeamsAPI
	cl.OpConfig.EnableTeamSuperuser = false

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
		// case 4: the team does not exist which should not return an error
		{
			ownerTeam:      "acid",
			existingRoles:  map[string]spec.PgUser{},
			superuserTeams: []string{"postgres_superusers"},
			teams:          []mockTeam{teamA, teamB, teamTest},
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
			t.Errorf("%s got an unexpected error %v", t.Name(), err)
		}

		if !reflect.DeepEqual(cl.pgUsers, tt.result) {
			t.Errorf("%s expects %#v, got %#v", t.Name(), tt.result, cl.pgUsers)
		}
	}
}

func TestPodAnnotations(t *testing.T) {
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
					t.Name()+"/"+tt.subTest, expected, observed, k)
			}
		}
		for k, v := range tt.merged {
			if observed, expected := annotations[k], v; observed != expected {
				t.Errorf("%v expects annotation value %v for key %v, but found %v",
					t.Name()+"/"+tt.subTest, expected, observed, k)
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
		enableTeamIdClusterPrefix     bool
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
			enableTeamIdClusterPrefix:    false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect:                       make(map[string]string),
		},
		{
			about:                        "Master with no annotations and EnableMasterLoadBalancer enabled on spec",
			role:                         "master",
			enableMasterLoadBalancerSpec: &enabled,
			enableMasterLoadBalancerOC:   false,
			enableTeamIdClusterPrefix:    false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                        "Master with no annotations and EnableMasterLoadBalancer enabled only on operator config",
			role:                         "master",
			enableMasterLoadBalancerSpec: &disabled,
			enableMasterLoadBalancerOC:   true,
			enableTeamIdClusterPrefix:    false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           make(map[string]string),
			expect:                       make(map[string]string),
		},
		{
			about:                      "Master with no annotations and EnableMasterLoadBalancer defined only on operator config",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations:         make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                      "Master with cluster annotations and load balancer enabled",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations:         map[string]string{"foo": "bar"},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                        "Master with cluster annotations and load balancer disabled",
			role:                         "master",
			enableMasterLoadBalancerSpec: &disabled,
			enableMasterLoadBalancerOC:   true,
			enableTeamIdClusterPrefix:    false,
			operatorAnnotations:          make(map[string]string),
			clusterAnnotations:           map[string]string{"foo": "bar"},
			expect:                       map[string]string{"foo": "bar"},
		},
		{
			about:                      "Master with operator annotations and load balancer enabled",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations:        map[string]string{"foo": "bar"},
			clusterAnnotations:         make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                      "Master with operator annotations override default annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			clusterAnnotations: make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                      "Master with cluster annotations override default annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                      "Master with cluster annotations do not override external-dns annotations",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  false,
			operatorAnnotations:        make(map[string]string),
			clusterAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                      "Master with cluster name teamId prefix enabled",
			role:                       "master",
			enableMasterLoadBalancerOC: true,
			enableTeamIdClusterPrefix:  true,
			clusterAnnotations:         make(map[string]string),
			operatorAnnotations:        make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test.test.db.example.com,test.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		// REPLICA
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer disabled on spec and OperatorConfig",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   false,
			enableTeamIdClusterPrefix:     false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect:                        make(map[string]string),
		},
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer enabled on spec",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &enabled,
			enableReplicaLoadBalancerOC:   false,
			enableTeamIdClusterPrefix:     false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                         "Replica with no annotations and EnableReplicaLoadBalancer enabled only on operator config",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   true,
			enableTeamIdClusterPrefix:     false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            make(map[string]string),
			expect:                        make(map[string]string),
		},
		{
			about:                       "Replica with no annotations and EnableReplicaLoadBalancer defined only on operator config",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations:          make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                       "Replica with cluster annotations and load balancer enabled",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations:          map[string]string{"foo": "bar"},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                         "Replica with cluster annotations and load balancer disabled",
			role:                          "replica",
			enableReplicaLoadBalancerSpec: &disabled,
			enableReplicaLoadBalancerOC:   true,
			enableTeamIdClusterPrefix:     false,
			operatorAnnotations:           make(map[string]string),
			clusterAnnotations:            map[string]string{"foo": "bar"},
			expect:                        map[string]string{"foo": "bar"},
		},
		{
			about:                       "Replica with operator annotations and load balancer enabled",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         map[string]string{"foo": "bar"},
			clusterAnnotations:          make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
				"foo": "bar",
			},
		},
		{
			about:                       "Replica with operator annotations override default annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			clusterAnnotations: make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                       "Replica with cluster annotations override default annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations: map[string]string{
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "1800",
			},
		},
		{
			about:                       "Replica with cluster annotations do not override external-dns annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         make(map[string]string),
			clusterAnnotations: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname": "wrong.external-dns-name.example.com",
			},
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		{
			about:                       "Replica with cluster name teamId prefix enabled",
			role:                        "replica",
			enableReplicaLoadBalancerOC: true,
			enableTeamIdClusterPrefix:   true,
			clusterAnnotations:          make(map[string]string),
			operatorAnnotations:         make(map[string]string),
			expect: map[string]string{
				"external-dns.alpha.kubernetes.io/hostname":                            "acid-test-repl.test.db.example.com,test-repl.acid.db.example.com",
				"service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout": "3600",
			},
		},
		// COMMON
		{
			about:                       "cluster annotations append to operator annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: false,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         map[string]string{"foo": "bar"},
			clusterAnnotations:          map[string]string{"post": "gres"},
			expect:                      map[string]string{"foo": "bar", "post": "gres"},
		},
		{
			about:                       "cluster annotations override operator annotations",
			role:                        "replica",
			enableReplicaLoadBalancerOC: false,
			enableTeamIdClusterPrefix:   false,
			operatorAnnotations:         map[string]string{"foo": "bar", "post": "gres"},
			clusterAnnotations:          map[string]string{"post": "greSQL"},
			expect:                      map[string]string{"foo": "bar", "post": "greSQL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.about, func(t *testing.T) {
			cl.OpConfig.EnableTeamIdClusternamePrefix = tt.enableTeamIdClusterPrefix

			cl.OpConfig.CustomServiceAnnotations = tt.operatorAnnotations
			cl.OpConfig.EnableMasterLoadBalancer = tt.enableMasterLoadBalancerOC
			cl.OpConfig.EnableReplicaLoadBalancer = tt.enableReplicaLoadBalancerOC
			cl.OpConfig.MasterDNSNameFormat = "{cluster}.{namespace}.{hostedzone}"
			cl.OpConfig.ReplicaDNSNameFormat = "{cluster}-repl.{namespace}.{hostedzone}"
			cl.OpConfig.DbHostedZone = "db.example.com"

			cl.Postgresql.Spec.ClusterName = ""
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
	// default cluster without connection pooler and event streams
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.ConnectionPoolerUserKeyName]; exist {
		t.Errorf("%s, connection pooler user is present", t.Name())
	}
	if _, exist := cl.systemUsers[constants.EventStreamUserKeyName]; exist {
		t.Errorf("%s, stream user is present", t.Name())
	}

	// cluster with connection pooler
	cl.Spec.EnableConnectionPooler = boolToPointer(true)
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.ConnectionPoolerUserKeyName]; !exist {
		t.Errorf("%s, connection pooler user is not present", t.Name())
	}

	// superuser is not allowed as connection pool user
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "postgres",
	}
	cl.OpConfig.SuperUsername = "postgres"
	cl.OpConfig.ConnectionPooler.User = "pooler"

	cl.initSystemUsers()
	if _, exist := cl.systemUsers["pooler"]; !exist {
		t.Errorf("%s, Superuser is not allowed to be a connection pool user", t.Name())
	}

	// neither protected users are
	delete(cl.systemUsers, "pooler")
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "admin",
	}
	cl.OpConfig.ProtectedRoles = []string{"admin"}

	cl.initSystemUsers()
	if _, exist := cl.systemUsers["pooler"]; !exist {
		t.Errorf("%s, Protected user are not allowed to be a connection pool user", t.Name())
	}

	delete(cl.systemUsers, "pooler")
	cl.Spec.ConnectionPooler = &acidv1.ConnectionPooler{
		User: "standby",
	}

	cl.initSystemUsers()
	if _, exist := cl.systemUsers["pooler"]; !exist {
		t.Errorf("%s, System users are not allowed to be a connection pool user", t.Name())
	}

	// using stream user in manifest but no streams defined should be treated like normal robot user
	streamUser := fmt.Sprintf("%s%s", constants.EventStreamSourceSlotPrefix, constants.UserRoleNameSuffix)
	cl.Spec.Users = map[string]acidv1.UserFlags{streamUser: []string{}}
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.EventStreamUserKeyName]; exist {
		t.Errorf("%s, stream user is present", t.Name())
	}

	// cluster with streams
	cl.Spec.Streams = []acidv1.Stream{
		{
			ApplicationId: "test-app",
			Database:      "test_db",
			Tables: map[string]acidv1.StreamTable{
				"data.test_table": {
					EventType: "test_event",
				},
			},
		},
	}
	cl.initSystemUsers()
	if _, exist := cl.systemUsers[constants.EventStreamUserKeyName]; !exist {
		t.Errorf("%s, stream user is not present", t.Name())
	}
}

func TestPreparedDatabases(t *testing.T) {
	cl.Spec.PreparedDatabases = map[string]acidv1.PreparedDatabase{}
	cl.initPreparedDatabaseRoles()

	for _, role := range []string{"acid_test_owner", "acid_test_reader", "acid_test_writer",
		"acid_test_data_owner", "acid_test_data_reader", "acid_test_data_writer"} {
		if _, exist := cl.pgUsers[role]; !exist {
			t.Errorf("%s, default role %q for prepared database not present", t.Name(), role)
		}
	}

	testName := "TestPreparedDatabaseWithSchema"

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
					Value: exampleSpiloConfig,
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
					Value: spiloConfigDiff,
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
					Value: exampleSpiloConfig,
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
					Value: exampleSpiloConfig,
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
					Value: exampleSpiloConfig,
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

func newService(ann map[string]string, svcT v1.ServiceType, lbSr []string) *v1.Service {
	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			Type:                     svcT,
			LoadBalancerSourceRanges: lbSr,
		},
	}
	svc.Annotations = ann
	return svc
}

func TestCompareServices(t *testing.T) {
	cluster := Cluster{
		Config: Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					IgnoredAnnotations: []string{
						"k8s.v1.cni.cncf.io/network-status",
					},
				},
			},
		},
	}

	tests := []struct {
		about   string
		current *v1.Service
		new     *v1.Service
		reason  string
		match   bool
	}{
		{
			about: "two equal services",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeClusterIP,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeClusterIP,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match: true,
		},
		{
			about: "services differ on service type",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeClusterIP,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's type "LoadBalancer" does not match the current one "ClusterIP"`,
		},
		{
			about: "services differ on lb source ranges",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"185.249.56.0/22"}),
			match:  false,
			reason: `new service's LoadBalancerSourceRange does not match the current one`,
		},
		{
			about: "new service doesn't have lb source ranges",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{}),
			match:  false,
			reason: `new service's LoadBalancerSourceRange does not match the current one`,
		},
		{
			about: "services differ on DNS annotation",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "new_clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: "external-dns.alpha.kubernetes.io/hostname" changed from "clstr.acid.zalan.do" to "new_clstr.acid.zalan.do".`,
		},
		{
			about: "services differ on AWS ELB annotation",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: "1800",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: "service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout" changed from "3600" to "1800".`,
		},
		{
			about: "service changes existing annotation",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "baz",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: "foo" changed from "bar" to "baz".`,
		},
		{
			about: "service changes multiple existing annotations",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
					"bar":                              "foo",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "baz",
					"bar":                              "fooz",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match: false,
			// Test just the prefix to avoid flakiness and map sorting
			reason: `new service's annotations does not match the current one:`,
		},
		{
			about: "service adds a new custom annotation",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: Added "foo" with value "bar".`,
		},
		{
			about: "service removes a custom annotation",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: Removed "foo".`,
		},
		{
			about: "service removes a custom annotation and adds a new one",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"bar":                              "foo",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match:  false,
			reason: `new service's annotations does not match the current one: Removed "foo". Added "bar" with value "foo".`,
		},
		{
			about: "service removes a custom annotation, adds a new one and change another",
			current: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"foo":                              "bar",
					"zalan":                            "do",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
					"bar":                              "foo",
					"zalan":                            "do.com",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match: false,
			// Test just the prefix to avoid flakiness and map sorting
			reason: `new service's annotations does not match the current one: Removed "foo".`,
		},
		{
			about: "service add annotations",
			current: newService(
				map[string]string{},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					constants.ZalandoDNSNameAnnotation: "clstr.acid.zalan.do",
					constants.ElbTimeoutAnnotationName: constants.ElbTimeoutAnnotationValue,
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match: false,
			// Test just the prefix to avoid flakiness and map sorting
			reason: `new service's annotations does not match the current one: Added `,
		},
		{
			about: "ignored annotations",
			current: newService(
				map[string]string{},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			new: newService(
				map[string]string{
					"k8s.v1.cni.cncf.io/network-status": "up",
				},
				v1.ServiceTypeLoadBalancer,
				[]string{"128.141.0.0/16", "137.138.0.0/16"}),
			match: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.about, func(t *testing.T) {
			match, reason := cluster.compareServices(tt.current, tt.new)
			if match && !tt.match {
				t.Logf("match=%v current=%v, old=%v reason=%s", match, tt.current.Annotations, tt.new.Annotations, reason)
				t.Errorf("%s - expected services to do not match: %q and %q", t.Name(), tt.current, tt.new)
				return
			}
			if !match && tt.match {
				t.Errorf("%s - expected services to be the same: %q and %q", t.Name(), tt.current, tt.new)
				return
			}
			if !match && !tt.match {
				if !strings.HasPrefix(reason, tt.reason) {
					t.Errorf("%s - expected reason prefix %s, found %s", t.Name(), tt.reason, reason)
					return
				}
			}
		})
	}
}

func TestCrossNamespacedSecrets(t *testing.T) {
	testName := "test secrets in different namespace"
	clientSet := fake.NewSimpleClientset()
	acidClientSet := fakeacidv1.NewSimpleClientset()
	namespace := "default"

	client := k8sutil.KubernetesClient{
		StatefulSetsGetter: clientSet.AppsV1(),
		ServicesGetter:     clientSet.CoreV1(),
		DeploymentsGetter:  clientSet.AppsV1(),
		PostgresqlsGetter:  acidClientSet.AcidV1(),
		SecretsGetter:      clientSet.CoreV1(),
	}
	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "acid-fake-cluster",
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
			Users: map[string]acidv1.UserFlags{
				"appspace.db_user": {},
				"db_user":          {},
			},
		},
	}

	var cluster = New(
		Config{
			OpConfig: config.Config{
				ConnectionPooler: config.ConnectionPooler{
					ConnectionPoolerDefaultCPURequest:    "100m",
					ConnectionPoolerDefaultCPULimit:      "100m",
					ConnectionPoolerDefaultMemoryRequest: "100Mi",
					ConnectionPoolerDefaultMemoryLimit:   "100Mi",
					NumberOfInstances:                    k8sutil.Int32ToPointer(1),
				},
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					DefaultCPURequest:    "300m",
					DefaultCPULimit:      "300m",
					DefaultMemoryRequest: "300Mi",
					DefaultMemoryLimit:   "300Mi",
					PodRoleLabel:         "spilo-role",
				},
				EnableCrossNamespaceSecret: true,
			},
		}, client, pg, logger, eventRecorder)

	userNamespaceMap := map[string]string{
		cluster.Namespace: "db_user",
		"appspace":        "appspace.db_user",
	}

	err := cluster.initRobotUsers()
	if err != nil {
		t.Errorf("Could not create secret for namespaced users with error: %s", err)
	}

	for _, u := range cluster.pgUsers {
		if u.Name != userNamespaceMap[u.Namespace] {
			t.Errorf("%s: Could not create namespaced user in its correct namespaces for user %s in namespace %s", testName, u.Name, u.Namespace)
		}
	}
}

func TestValidUsernames(t *testing.T) {
	testName := "test username validity"

	invalidUsernames := []string{"_", ".", ".user", "appspace.", "user_", "_user", "-user", "user-", ",", "-", ",user", "user,", "namespace,user"}
	validUsernames := []string{"user", "appspace.user", "appspace.dot.user", "user_name", "app_space.user_name"}
	for _, username := range invalidUsernames {
		if isValidUsername(username) {
			t.Errorf("%s Invalid username is allowed: %s", testName, username)
		}
	}
	for _, username := range validUsernames {
		if !isValidUsername(username) {
			t.Errorf("%s Valid username is not allowed: %s", testName, username)
		}
	}
}

func TestComparePorts(t *testing.T) {
	testCases := []struct {
		name     string
		setA     []v1.ContainerPort
		setB     []v1.ContainerPort
		expected bool
	}{
		{
			name: "different ports",
			setA: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
			},

			setB: []v1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 80,
					Protocol:      v1.ProtocolTCP,
				},
			},
			expected: false,
		},
		{
			name: "no difference",
			setA: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
			},
			setB: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
			},
			expected: true,
		},
		{
			name: "same ports, different order",
			setA: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
				{
					Name:          "http",
					ContainerPort: 80,
					Protocol:      v1.ProtocolTCP,
				},
			},
			setB: []v1.ContainerPort{
				{
					Name:          "http",
					ContainerPort: 80,
					Protocol:      v1.ProtocolTCP,
				},
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
			},
			expected: true,
		},
		{
			name: "same ports, but one with default protocol",
			setA: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
					Protocol:      v1.ProtocolTCP,
				},
			},
			setB: []v1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 9187,
				},
			},
			expected: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := comparePorts(testCase.setA, testCase.setB)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
