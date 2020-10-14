package teams

import (
	"reflect"
	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	True  = true
	False = false
)

// PostgresTeamMap is the operator's internal representation of all PostgresTeam CRDs
func TestLoadingPostgresTeamCRD(t *testing.T) {
	tests := []struct {
		name  string
		crd   acidv1.PostgresTeamList
		pgt   PostgresTeamMap
		error string
	}{
		{
			"Check that CRD is imported correctly into the internal format",
			acidv1.PostgresTeamList{
				TypeMeta: metav1.TypeMeta{
					Kind:       "List",
					APIVersion: "v1",
				},
				Items: []acidv1.PostgresTeam{
					{
						TypeMeta: metav1.TypeMeta{
							Kind:       "PostgresTeam",
							APIVersion: "acid.zalan.do/v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "teamAB",
						},
						Spec: acidv1.PostgresTeamSpec{
							AdditionalAdminTeams: map[string][]string{"teamA": []string{"teamB", "team24/7"}, "teamB": []string{"teamA", "team24/7"}},
							AdditionalTeams:      map[string][]string{"teamA": []string{"teamC"}, "teamB": []string{}},
							AdditionalMembers:    map[string][]string{"team24/7": []string{"optimusprime"}, "teamB": []string{"drno"}},
						},
					}, {
						TypeMeta: metav1.TypeMeta{
							Kind:       "PostgresTeam",
							APIVersion: "acid.zalan.do/v1",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "teamC",
						},
						Spec: acidv1.PostgresTeamSpec{
							AdditionalAdminTeams: map[string][]string{"teamC": []string{"team24/7"}},
							AdditionalTeams:      map[string][]string{"teamA": []string{"teamC"}, "teamC": []string{"teamA", "teamB"}},
							AdditionalMembers:    map[string][]string{"acid": []string{"batman"}},
						},
					},
				},
			},
			PostgresTeamMap{
				"teamA": {
					AdditionalAdminTeams: []string{"teamB", "team24/7"},
					AdditionalTeams:      []string{"teamC"},
					AdditionalMembers:    nil,
				},
				"teamB": {
					AdditionalAdminTeams: []string{"teamA", "team24/7"},
					AdditionalTeams:      []string{},
					AdditionalMembers:    []string{"drno"},
				},
				"teamC": {
					AdditionalAdminTeams: []string{"team24/7"},
					AdditionalTeams:      []string{"teamA", "teamB"},
					AdditionalMembers:    nil,
				},
				"team24/7": {
					AdditionalAdminTeams: nil,
					AdditionalTeams:      nil,
					AdditionalMembers:    []string{"optimusprime"},
				},
				"acid": {
					AdditionalAdminTeams: nil,
					AdditionalTeams:      nil,
					AdditionalMembers:    []string{"batman"},
				},
			},
			"Mismatch between PostgresTeam CRD and internal map",
		},
	}

	for _, tt := range tests {
		postgresTeamMap := PostgresTeamMap{}
		postgresTeamMap.Load(&tt.crd)
		// TODO order in slice is not deterministic so choose other compare method
		if !reflect.DeepEqual(postgresTeamMap, tt.pgt) {
			t.Errorf("%s: %v: expected %#v, got %#v", tt.name, tt.error, tt.pgt, postgresTeamMap)
		}
	}
}
