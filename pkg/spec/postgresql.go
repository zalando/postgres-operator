package spec

import (
	"encoding/json"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/unversioned"
)

type MaintenanceWindow struct {
	StartTime string
	EndTime   string
	//StartTime     time.Time      // Start time
	//StartWeekday  time.Weekday   // Start weekday
	//
	//EndTime       time.Time      // End time
	//EndWeekday    time.Weekday   // End weekday
}

type Volume struct {
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
}

type PostgresqlParam struct {
	Version    string            `json:"version"`
	Parameters map[string]string `json:"parameters"`
}

type Resources struct {
	Cpu    string `json:"cpu"`
	Memory string `json:"memory"`
}

type Patroni struct {
	InitDB               map[string]string `json:"initdb"`
	PgHba                []string          `json:"pg_hba"`
	TTL                  uint32            `json:"ttl"`
	LoopWait             uint32            `json:"loop_wait"`
	RetryTimeout         uint32            `json:"retry_timeout"`
	MaximumLagOnFailover float32           `json:"maximum_lag_on_failover"` // float32 because https://github.com/kubernetes/kubernetes/issues/30213
}

type UserFlags []string

type PostgresSpec struct {
	Resources       `json:"resources,omitempty"`
	Patroni         `json:"patroni,omitempty"`
	PostgresqlParam `json:"postgresql"`
	Volume          `json:"volume,omitempty"`

	NumberOfInstances  int32                `json:"numberOfInstances"`
	Users              map[string]UserFlags `json:"users"`
	MaintenanceWindows []string             `json:"maintenanceWindows,omitempty"`

	EtcdHost    string
	DockerImage string
}

type PostgresStatus struct {
	// Phase is the cluster running phase
	Phase  string `json:"phase"`
	Reason string `json:"reason"`

	// ControlPuased indicates the operator pauses the control of the cluster.
	ControlPaused bool `json:"controlPaused"`

	// Size is the current size of the cluster
	Size int `json:"size"`
	// CurrentVersion is the current cluster version
	CurrentVersion string `json:"currentVersion"`
	// TargetVersion is the version the cluster upgrading to.
	// If the cluster is not upgrading, TargetVersion is empty.
	TargetVersion string `json:"targetVersion"`
}

// PostgreSQL Third Party (resource) Object
type Postgresql struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             api.ObjectMeta `json:"metadata"`

	Spec   *PostgresSpec   `json:"spec"`
	Status *PostgresStatus `json:"status"`
}

type PostgresqlList struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             unversioned.ListMeta `json:"metadata"`

	Items []Postgresql `json:"items"`
}

func (p *Postgresql) GetObjectKind() unversioned.ObjectKind {
	return &p.TypeMeta
}

func (p *Postgresql) GetObjectMeta() meta.Object {
	return &p.Metadata
}

func (pl *PostgresqlList) GetObjectKind() unversioned.ObjectKind {
	return &pl.TypeMeta
}

func (pl *PostgresqlList) GetListMeta() unversioned.List {
	return &pl.Metadata
}

// The code below is used only to work around a known problem with third-party
// resources and ugorji. If/when these issues are resolved, the code below
// should no longer be required.
//
type PostgresqlListCopy PostgresqlList
type PostgresqlCopy Postgresql

func (p *Postgresql) UnmarshalJSON(data []byte) error {
	tmp := PostgresqlCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := Postgresql(tmp)
	*p = tmp2

	return nil
}

func (pl *PostgresqlList) UnmarshalJSON(data []byte) error {
	tmp := PostgresqlListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PostgresqlList(tmp)
	*pl = tmp2

	return nil
}
