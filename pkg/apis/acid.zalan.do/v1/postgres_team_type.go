package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PostgresTeam defines Custom Resource Definition Object for team management.
// +k8s:deepcopy-gen=true
// +kubebuilder:resource:shortName=pgteam,categories=all
// +kubebuilder:subresource:status
type PostgresTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec PostgresTeamSpec `json:"spec"`
}

// List of users who will also be added to the Postgres cluster.
type Users []string

// List of teams whose members will also be added to the Postgres cluster.
type Teams []string

// List of teams to become Postgres superusers
type SuperUserTeams []string

// PostgresTeamSpec defines the specification for the PostgresTeam TPR.
type PostgresTeamSpec struct {
	// Map for teamId and associated additional superuser teams
	AdditionalSuperuserTeams map[string]SuperUserTeams `json:"additionalSuperuserTeams,omitempty"`
	// Map for teamId and associated additional teams
	AdditionalTeams map[string]Teams `json:"additionalTeams,omitempty"`
	// Map for teamId and associated additional users
	AdditionalMembers map[string]Users `json:"additionalMembers,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PostgresTeamList defines a list of PostgresTeam definitions.
type PostgresTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PostgresTeam `json:"items"`
}
