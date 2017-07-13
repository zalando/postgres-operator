package spec

import (
	"fmt"
	"strings"

	"database/sql"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
)

// EvenType contains type of the events for the TPRs and Pods received from Kubernetes
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
	ClusterName     NamespacedName
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

// Config contains operator-wide clients and configuration used from a cluster. TODO: remove struct duplication.
type ClusterConfig struct {
	KubeClient          *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient          *rest.RESTClient
	TeamsAPIClient      *teams.API
	RestConfig          *rest.Config
	InfrastructureRoles map[string]PgUser // inherited from the controller
}

// UserSyncer defines an interface for the implementations to sync users from the manifest to the DB.
type UserSyncer interface {
	ProduceSyncRequests(dbUsers PgUserMap, newUsers PgUserMap) (req []PgSyncUserRequest)
	ExecuteSyncRequests(req []PgSyncUserRequest, db *sql.DB) error
}

type ClusterEventHandler interface {
	Create() error
	Update(*Postgresql) error
	Delete() error
	Sync() error
}

type ClusterCommandExecutor interface {
	ExecCommand(*NamespacedName, ...string) (string, error)
}

type ClusterController interface {
	Run(<-chan struct{})
	ReceivePodEvent(PodEvent)
}

type Cluster interface {
	ClusterEventHandler
	ClusterCommandExecutor
	ClusterController
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
		return fmt.Errorf("Incorrect namespaced name")
	}

	*n = NamespacedName(name)

	return nil
}
