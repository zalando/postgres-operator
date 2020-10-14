package teams

import (
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
)

// PostgresTeamMap is the operator's internal representation of all PostgresTeam CRDs
type PostgresTeamMap map[string]postgresTeamMembership

type postgresTeamMembership struct {
	AdditionalAdminTeams []string
	AdditionalTeams      []string
	AdditionalMembers    []string
}

type teamHashSet map[string]map[string]struct{}

func (ths *teamHashSet) has(team string) bool {
	_, ok := (*ths)[team]
	return ok
}

func (ths *teamHashSet) add(newTeam string, newSet []string) {
	set := make(map[string]struct{})
	if ths.has(newTeam) {
		set = (*ths)[newTeam]
	}
	for _, t := range newSet {
		set[t] = struct{}{}
	}
	(*ths)[newTeam] = set
}

func (ths *teamHashSet) toMap() map[string][]string {
	newTeamMap := make(map[string][]string)
	for team, items := range *ths {
		list := []string{}
		for item := range items {
			list = append(list, item)
		}
		newTeamMap[team] = list
	}
	return newTeamMap
}

func (ths *teamHashSet) mergeCrdMap(crdTeamMap map[string][]string) {
	for t, at := range crdTeamMap {
		ths.add(t, at)
	}
}

func fetchTeams(teamset *map[string]struct{}, set teamHashSet) {
	for key := range set {
		(*teamset)[key] = struct{}{}
	}
}

func (ptm *PostgresTeamMap) fetchAdditionalTeams(team string, adminTeams bool, transitive bool, exclude *[]string) []string {

	var teams, allTeams []string

	if adminTeams {
		teams = (*ptm)[team].AdditionalAdminTeams
		allTeams = teams
		for _, otherPrivilegedTeam := range (*ptm)[team].AdditionalTeams {
			allTeams = append(allTeams, otherPrivilegedTeam)
		}
	} else {
		teams = (*ptm)[team].AdditionalTeams
		allTeams = teams
		for _, otherPrivilegedTeam := range (*ptm)[team].AdditionalAdminTeams {
			allTeams = append(allTeams, otherPrivilegedTeam)
		}
	}
	if transitive {
		*exclude = append(*exclude, team)
		for _, additionalTeam := range allTeams {
			getTransitiveTeams := true
			for _, excludedTeam := range *exclude {
				if additionalTeam == excludedTeam {
					getTransitiveTeams = false
				}
			}
			if getTransitiveTeams {
				transitiveTeams := (*ptm).fetchAdditionalTeams(additionalTeam, adminTeams, transitive, exclude)

				if len(transitiveTeams) > 0 {
					for _, transitiveTeam := range transitiveTeams {
						teams = append(teams, transitiveTeam)
					}
				}
			}
		}
	}

	return teams
}

// GetAdditionalTeams function to retrieve list of additional teams
func (ptm *PostgresTeamMap) GetAdditionalTeams(team string, transitive bool) []string {
	return ptm.fetchAdditionalTeams(team, false, transitive, &[]string{})
}

// GetAdditionalTeams function to retrieve list of additional teams
func (ptm *PostgresTeamMap) GetAdditionalAdminTeams(team string, transitive bool) []string {
	return ptm.fetchAdditionalTeams(team, true, transitive, &[]string{})
}

// Load function to import data from PostgresTeam CRD
func (ptm *PostgresTeamMap) Load(pgTeams *acidv1.PostgresTeamList) {
	adminTeamSet := teamHashSet{}
	teamSet := teamHashSet{}
	teamMemberSet := teamHashSet{}
	teamIDs := make(map[string]struct{})

	for _, pgTeam := range pgTeams.Items {
		adminTeamSet.mergeCrdMap(pgTeam.Spec.AdditionalAdminTeams)
		teamSet.mergeCrdMap(pgTeam.Spec.AdditionalTeams)
		teamMemberSet.mergeCrdMap(pgTeam.Spec.AdditionalMembers)
	}
	fetchTeams(&teamIDs, adminTeamSet)
	fetchTeams(&teamIDs, teamSet)
	fetchTeams(&teamIDs, teamMemberSet)

	for teamID := range teamIDs {
		(*ptm)[teamID] = postgresTeamMembership{
			AdditionalAdminTeams: adminTeamSet.toMap()[teamID],
			AdditionalTeams:      teamSet.toMap()[teamID],
			AdditionalMembers:    teamMemberSet.toMap()[teamID],
		}
	}
}
