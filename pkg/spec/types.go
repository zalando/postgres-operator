package spec

import (
	"database/sql"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
)

// EventType contains type of the events for the TPRs and Pods received from Kubernetes
type EventType string

// NamespacedName describes the namespace/name pairs used in Kubernetes names.
type NamespacedName types.NamespacedName

// Possible values for the EventType
const (
	EventAdd    EventType = "ADD"
	EventUpdate EventType = "UPDATE"
	EventDelete EventType = "DELETE"
	EventSync   EventType = "SYNC"
)

// ClusterEvent carries the payload of the Cluster TPR events.
type ClusterEvent struct {
	UID       types.UID
	EventType EventType
	OldSpec   *Postgresql
	NewSpec   *Postgresql
	WorkerID  uint32
}

type syncUserOperation int

// Possible values for the sync user operation (removal of users is not supported yet)
const (
	PGSyncUserAdd = iota
	PGsyncUserAlter
)

// PodEvent describes the event for a single Pod
type PodEvent struct {
	ResourceVersion string
	PodName         NamespacedName
	PrevPod         *v1.Pod
	CurPod          *v1.Pod
	EventType       EventType
}

// PgUser contains information about a single user.
type PgUser struct {
	Name     string
	Password string
	Flags    []string
	MemberOf []string
}

// PgUserMap maps user names to the definitions.
type PgUserMap map[string]PgUser

// PgSyncUserRequest has information about a single request to sync a user.
type PgSyncUserRequest struct {
	Kind syncUserOperation
	User PgUser
}

// UserSyncer defines an interface for the implementations to sync users from the manifest to the DB.
type UserSyncer interface {
	ProduceSyncRequests(dbUsers PgUserMap, newUsers PgUserMap) (req []PgSyncUserRequest)
	ExecuteSyncRequests(req []PgSyncUserRequest, db *sql.DB) error
}

// ControllerConfig describes configuration of the controller
type ControllerConfig struct {
	RestConfig          *rest.Config `json:"-"`
	InfrastructureRoles map[string]PgUser

	NoDatabaseAccess bool
	NoTeamsAPI       bool
	ConfigMapName    NamespacedName
	Namespace        string
}

func (n NamespacedName) String() string {
	return types.NamespacedName(n).String()
}

// MarshalJSON defines marshaling rule for the namespaced name type.
func (n NamespacedName) MarshalJSON() ([]byte, error) {
	return []byte("\"" + n.String() + "\""), nil
}

// Decode converts a (possibly unqualified) string into the namespaced name object.
func (n *NamespacedName) Decode(value string) error {
	name := types.NewNamespacedNameFromString(value)

	if strings.Trim(value, string(types.Separator)) != "" && name == (types.NamespacedName{}) {
		name.Name = value
		name.Namespace = v1.NamespaceDefault
	} else if name.Namespace == "" {
		name.Namespace = v1.NamespaceDefault
	}

	if name.Name == "" {
		return fmt.Errorf("incorrect namespaced name")
	}

	*n = NamespacedName(name)

	return nil
}
