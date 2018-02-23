package spec

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	policyv1beta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
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

	fileWithNamespace = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
)

type RoleOrigin int

const (
	RoleOriginUnknown = iota
	RoleOriginInfrastructure
	RoleOriginManifest
	RoleOriginTeamsAPI
	RoleOriginSystem
)

// ClusterEvent carries the payload of the Cluster TPR events.
type ClusterEvent struct {
	EventTime time.Time
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
	PGSyncAlterSet // handle ALTER ROLE SET parameter = value
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
	Origin     RoleOrigin
	Name       string
	Password   string
	Flags      []string
	MemberOf   []string
	Parameters map[string]string
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

// LogEntry describes log entry in the RingLogger
type LogEntry struct {
	Time        time.Time
	Level       logrus.Level
	ClusterName *NamespacedName `json:",omitempty"`
	Worker      *uint32         `json:",omitempty"`
	Message     string
}

// Process describes process of the cluster
type Process struct {
	Name      string
	StartTime time.Time
}

// ClusterStatus describes status of the cluster
type ClusterStatus struct {
	Team                string
	Cluster             string
	MasterService       *v1.Service
	ReplicaService      *v1.Service
	MasterEndpoint      *v1.Endpoints
	ReplicaEndpoint     *v1.Endpoints
	StatefulSet         *v1beta1.StatefulSet
	PodDisruptionBudget *policyv1beta1.PodDisruptionBudget

	CurrentProcess Process
	Worker         uint32
	Status         PostgresStatus
	Spec           PostgresSpec
	Error          error
}

// WorkerStatus describes status of the worker
type WorkerStatus struct {
	CurrentCluster NamespacedName
	CurrentProcess Process
}

// Diff describes diff
type Diff struct {
	EventTime   time.Time
	ProcessTime time.Time
	Diff        []string
}

// ControllerStatus describes status of the controller
type ControllerStatus struct {
	LastSyncTime    int64
	Clusters        int
	WorkerQueueSize map[int]int
}

// QueueDump describes cache.FIFO queue
type QueueDump struct {
	Keys []string
	List []interface{}
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

// cached value for the GetOperatorNamespace
var operatorNamespace string

func (n NamespacedName) String() string {
	return types.NamespacedName(n).String()
}

// MarshalJSON defines marshaling rule for the namespaced name type.
func (n NamespacedName) MarshalJSON() ([]byte, error) {
	return []byte("\"" + n.String() + "\""), nil
}

// Decode converts a (possibly unqualified) string into the namespaced name object.
func (n *NamespacedName) Decode(value string) error {
	return n.DecodeWorker(value, GetOperatorNamespace())
}

// DecodeWorker separates the decode logic to (unit) test
// from obtaining the operator namespace that depends on k8s mounting files at runtime
func (n *NamespacedName) DecodeWorker(value, operatorNamespace string) error {
	name := types.NewNamespacedNameFromString(value)

	if strings.Trim(value, string(types.Separator)) != "" && name == (types.NamespacedName{}) {
		name.Name = value
		name.Namespace = operatorNamespace
	} else if name.Namespace == "" {
		name.Namespace = operatorNamespace
	}

	if name.Name == "" {
		return fmt.Errorf("incorrect namespaced name: %v", value)
	}

	*n = NamespacedName(name)

	return nil
}

// GetOperatorNamespace assumes serviceaccount secret is mounted by kubernetes
// Placing this func here instead of pgk/util avoids circular import
func GetOperatorNamespace() string {
	if operatorNamespace == "" {
		operatorNamespaceBytes, err := ioutil.ReadFile(fileWithNamespace)
		if err != nil {
			log.Fatalf("Unable to detect operator namespace from within its pod due to: %v", err)
		}
		operatorNamespace = string(operatorNamespaceBytes)
	}
	return operatorNamespace
}
