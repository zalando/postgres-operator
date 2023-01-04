package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PostgresTeam defines Custom Resource Definition Object for team management.
type PostgresTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PostgresTeamSpec `json:"spec"`
}

// PostgresTeamSpec defines the specification for the PostgresTeam TPR.
type PostgresTeamSpec struct {
	AdditionalSuperuserTeams map[string][]string `json:"additionalSuperuserTeams,omitempty"`
	AdditionalTeams          map[string][]string `json:"additionalTeams,omitempty"`
	AdditionalMembers        map[string][]string `json:"additionalMembers,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PostgresTeamList defines a list of PostgresTeam definitions.
type PostgresTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PostgresTeam `json:"items"`
}
