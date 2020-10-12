package postgresteams

import (
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
)

// PostgresTeamMap is the operator's internal representation of all PostgresTeam CRDs
type PostgresTeamMap map[string]postgresTeamMembership

type postgresTeamMembership struct {
	AdditionalTeams   map[additionalTeam]struct{}
	AdditionalMembers map[string]struct{}
}

type additionalTeam struct {
	Name    string
	IsAdmin bool
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

func (ths *teamHashSet) mergeCrdMap(crdTeamMap map[string][]string) {
	for t, at := range crdTeamMap {
		ths.add(t, at)
	}
}

func (pgt *PostgresTeamMap) mapTeams(set teamHashSet, isAdmin bool) {
	for team, items := range set {
		newAdditionalTeams := make(map[additionalTeam]struct{})
		newAdditionalMembers := make(map[string]struct{})
		if currentTeamMembership, exists := (*pgt)[team]; exists {
			newAdditionalTeams = currentTeamMembership.AdditionalTeams
			newAdditionalMembers = currentTeamMembership.AdditionalMembers
		}
		for newTeam := range items {
			newAdditionalTeams[additionalTeam{
				Name:    newTeam,
				IsAdmin: isAdmin,
			}] = struct{}{}
		}
		if len(newAdditionalTeams) > 0 {
			(*pgt)[team] = postgresTeamMembership{newAdditionalTeams, newAdditionalMembers}
		}
	}
}

func (pgt *PostgresTeamMap) mapMembers(set teamHashSet) {
	for team, items := range set {
		newAdditionalTeams := make(map[additionalTeam]struct{})
		newAdditionalMembers := make(map[string]struct{})
		if currentTeamMembership, exists := (*pgt)[team]; exists {
			newAdditionalTeams = currentTeamMembership.AdditionalTeams
			newAdditionalMembers = currentTeamMembership.AdditionalMembers
		}
		for additionalMember := range items {
			newAdditionalMembers[additionalMember] = struct{}{}
		}
		(*pgt)[team] = postgresTeamMembership{newAdditionalTeams, newAdditionalMembers}
	}
}

// Load function to import data from PostgresTeam CRD
func (pgt *PostgresTeamMap) Load(pgTeams *acidv1.PostgresTeamList) {

	adminTeamSet := teamHashSet{}
	teamSet := teamHashSet{}
	teamMemberSet := teamHashSet{}

	for _, pgTeam := range pgTeams.Items {
		adminTeamSet.mergeCrdMap(pgTeam.Spec.AdditionalAdminTeams)
		teamSet.mergeCrdMap(pgTeam.Spec.AdditionalTeams)
		teamMemberSet.mergeCrdMap(pgTeam.Spec.AdditionalMembers)
	}
	pgt.mapTeams(adminTeamSet, true)
	pgt.mapTeams(teamSet, false)
	pgt.mapMembers(teamMemberSet)
}

// MergeTeams function to add additional teams to internal team map
func (pgt *PostgresTeamMap) MergeTeams(teamID string, additionalTeams []string, isAdmin bool) {
	teamSet := teamHashSet{}
	if len(additionalTeams) > 0 {
		teamSet.add(teamID, additionalTeams)
		pgt.mapTeams(teamSet, isAdmin)
	}
}
