package types

import (
	"database/sql"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

// EvenType contains type of the events for the TPRs and Pods received from Kubernetes
type EventType string

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
	OldSpec   *spec.Postgresql
	NewSpec   *spec.Postgresql
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

// UserSyncer defines an interface for the implementations to sync users from the manifest to the DB.
type UserSyncer interface {
	ProduceSyncRequests(dbUsers PgUserMap, newUsers PgUserMap) (req []PgSyncUserRequest)
	ExecuteSyncRequests(req []PgSyncUserRequest, db *sql.DB) error
}

type OperatorCluster interface {
	Create() error
	Delete() error
	ExecCommand(podName *NamespacedName, command ...string) (string, error)
	ReceivePodEvent(event PodEvent)
	Run(stopCh <-chan struct{})
	Sync() error
	Update(newSpec *spec.Postgresql) error
	SetFailed(err error)
}
