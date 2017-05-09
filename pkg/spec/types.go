package spec

import (
	"database/sql"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
)

type EventType string

type NamespacedName types.NamespacedName

const (
	EventAdd    EventType = "ADD"
	EventUpdate EventType = "UPDATE"
	EventDelete EventType = "DELETE"
	EventSync   EventType = "SYNC"
)

type ClusterEvent struct {
	UID       types.UID
	EventType EventType
	OldSpec   *Postgresql
	NewSpec   *Postgresql
	WorkerID  uint32
}

type SyncUserOperation int

const (
	PGSyncUserAdd = iota
	PGsyncUserAlter
)

type PodEvent struct {
	ClusterName NamespacedName
	PodName     NamespacedName
	PrevPod     *v1.Pod
	CurPod      *v1.Pod
	EventType   EventType
}

type PgUser struct {
	Name     string
	Password string
	Flags    []string
	MemberOf []string
}

type PgUserMap map[string]PgUser

type PgSyncUserRequest struct {
	Kind SyncUserOperation
	User PgUser
}

type UserSyncer interface {
	ProduceSyncRequests(dbUsers PgUserMap, newUsers PgUserMap) (req []PgSyncUserRequest)
	ExecuteSyncRequests(req []PgSyncUserRequest, db *sql.DB) error
}

func (p NamespacedName) String() string {
	if p.Namespace == "" && p.Name == "" {
		return ""
	}

	return types.NamespacedName(p).String()
}

func (p NamespacedName) MarshalJSON() ([]byte, error) {
	return []byte("\"" + p.String() + "\""), nil
}

func (n *NamespacedName) Decode(value string) error {
	name := types.NewNamespacedNameFromString(value)
	if value != "" && name == (types.NamespacedName{}) {
		name.Name = value
		name.Namespace = v1.NamespaceDefault
	}

	*n = NamespacedName(name)

	return nil
}
