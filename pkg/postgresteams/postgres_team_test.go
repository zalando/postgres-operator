package postgresteams

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
func TestLoadinngPostgresTeamCRD(t *testing.T) {
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
					AdditionalTeams: map[additionalTeam]struct{}{
						additionalTeam{Name: "teamB", IsAdmin: True}:    {},
						additionalTeam{Name: "team24/7", IsAdmin: True}: {},
						additionalTeam{Name: "teamC", IsAdmin: False}:   {},
					},
					AdditionalMembers: map[string]struct{}{},
				},
				"teamB": {
					AdditionalTeams: map[additionalTeam]struct{}{
						additionalTeam{Name: "teamA", IsAdmin: True}:    {},
						additionalTeam{Name: "team24/7", IsAdmin: True}: {},
					},
					AdditionalMembers: map[string]struct{}{
						"drno": {},
					},
				},
				"teamC": {
					AdditionalTeams: map[additionalTeam]struct{}{
						additionalTeam{Name: "team24/7", IsAdmin: True}: {},
						additionalTeam{Name: "teamA", IsAdmin: False}:   {},
						additionalTeam{Name: "teamB", IsAdmin: False}:   {},
					},
					AdditionalMembers: map[string]struct{}{},
				},
				"team24/7": {
					AdditionalTeams: map[additionalTeam]struct{}{},
					AdditionalMembers: map[string]struct{}{
						"optimusprime": {},
					},
				},
				"acid": {
					AdditionalTeams: map[additionalTeam]struct{}{},
					AdditionalMembers: map[string]struct{}{
						"batman": {},
					},
				},
			},
			"Mismatch between PostgresTeam CRD and internal map",
		},
	}

	for _, tt := range tests {
		postgresTeamMap := PostgresTeamMap{}
		postgresTeamMap.Load(&tt.crd)
		if !reflect.DeepEqual(postgresTeamMap, tt.pgt) {
			t.Errorf("%s: %v", tt.name, tt.error)
		}
	}
}
