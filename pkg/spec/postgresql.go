package spec

import (
	"encoding/json"
	"fmt"
	"github.com/mohae/deepcopy"
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

// MaintenanceWindow describes the time window when the operator is allowed to do maintenance on a cluster.
type MaintenanceWindow struct {
	Everyday  bool
	Weekday   time.Weekday
	StartTime time.Time // Start time
	EndTime   time.Time // End time
}

// Volume describes a single volume in the manifest.
type Volume struct {
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
}

// PostgresqlParam describes PostgreSQL version and pairs of configuration parameter name - values.
type PostgresqlParam struct {
	PgVersion  string            `json:"version"`
	Parameters map[string]string `json:"parameters"`
}

// ResourceDescription describes CPU and memory resources defined for a cluster.
type ResourceDescription struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// Resources describes requests and limits for the cluster resouces.
type Resources struct {
	ResourceRequest ResourceDescription `json:"requests,omitempty"`
	ResourceLimits  ResourceDescription `json:"limits,omitempty"`
}

// Patroni contains Patroni-specific configuration
type Patroni struct {
	InitDB               map[string]string `json:"initdb"`
	PgHba                []string          `json:"pg_hba"`
	TTL                  uint32            `json:"ttl"`
	LoopWait             uint32            `json:"loop_wait"`
	RetryTimeout         uint32            `json:"retry_timeout"`
	MaximumLagOnFailover float32           `json:"maximum_lag_on_failover"` // float32 because https://github.com/kubernetes/kubernetes/issues/30213
}

// CloneDescription describes which cluster the new should clone and up to which point in time
type CloneDescription struct {
	ClusterName  string `json:"cluster,omitempty"`
	Uid          string `json:"uid,omitempty"`
	EndTimestamp string `json:"timestamp,omitempty"`
}

type UserFlags []string

// PostgresStatus contains status of the PostgreSQL cluster (running, creation failed etc.)
type PostgresStatus string

// possible values for PostgreSQL cluster statuses
const (
	ClusterStatusUnknown      PostgresStatus = ""
	ClusterStatusCreating     PostgresStatus = "Creating"
	ClusterStatusUpdating     PostgresStatus = "Updating"
	ClusterStatusUpdateFailed PostgresStatus = "UpdateFailed"
	ClusterStatusSyncFailed   PostgresStatus = "SyncFailed"
	ClusterStatusAddFailed    PostgresStatus = "CreateFailed"
	ClusterStatusRunning      PostgresStatus = "Running"
	ClusterStatusInvalid      PostgresStatus = "Invalid"
)

const (
	serviceNameMaxLength   = 63
	clusterNameMaxLength   = serviceNameMaxLength - len("-repl")
	serviceNameRegexString = `^[a-z]([-a-z0-9]*[a-z0-9])?$`
)

// Postgresql defines PostgreSQL Custom Resource Definition Object.
type Postgresql struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PostgresSpec   `json:"spec"`
	Status PostgresStatus `json:"status,omitempty"`
	Error  error          `json:"-"`
}

// PostgresSpec defines the specification for the PostgreSQL TPR.
type PostgresSpec struct {
	PostgresqlParam `json:"postgresql"`
	Volume          `json:"volume,omitempty"`
	Patroni         `json:"patroni,omitempty"`
	Resources       `json:"resources,omitempty"`

	TeamID      string `json:"teamId"`
	DockerImage string `json:"dockerImage,omitempty"`

	// vars that enable load balancers are pointers because it is important to know if any of them is omitted from the Postgres manifest
	// in that case the var evaluates to nil and the value is taken from the operator config
	EnableMasterLoadBalancer  *bool `json:"enableMasterLoadBalancer,omitempty"`
	EnableReplicaLoadBalancer *bool `json:"enableReplicaLoadBalancer,omitempty"`

	// deprecated load balancer settings mantained for backward compatibility
	// see "Load balancers" operator docs
	UseLoadBalancer     *bool `json:"useLoadBalancer,omitempty"`
	ReplicaLoadBalancer *bool `json:"replicaLoadBalancer,omitempty"`

	// load balancers' source ranges are the same for master and replica services
	AllowedSourceRanges []string `json:"allowedSourceRanges"`

	NumberOfInstances  int32                `json:"numberOfInstances"`
	Users              map[string]UserFlags `json:"users"`
	MaintenanceWindows []MaintenanceWindow  `json:"maintenanceWindows,omitempty"`
	Clone              CloneDescription     `json:"clone"`
	ClusterName        string               `json:"-"`
	Databases          map[string]string    `json:"databases,omitempty"`
	Tolerations        []v1.Toleration      `json:"tolerations,omitempty"`
}

// PostgresqlList defines a list of PostgreSQL clusters.
type PostgresqlList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Postgresql `json:"items"`
}

var (
	weekdays         = map[string]int{"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6}
	serviceNameRegex = regexp.MustCompile(serviceNameRegexString)
)

// Clone makes a deepcopy of the Postgresql structure. The Error field is nulled-out,
// as there is no guaratee that the actual implementation of the error interface
// will not contain any private fields not-reachable to deepcopy. This should be ok,
// since Error is never read from a Kubernetes object.
func (p *Postgresql) Clone() *Postgresql {
	if p == nil {
		return nil
	}
	c := deepcopy.Copy(p).(*Postgresql)
	c.Error = nil
	return c
}

func parseTime(s string) (time.Time, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("incorrect time format")
	}
	timeLayout := "15:04"

	tp, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, err
	}

	return tp.UTC(), nil
}

func parseWeekday(s string) (time.Weekday, error) {
	weekday, ok := weekdays[s]
	if !ok {
		return time.Weekday(0), fmt.Errorf("incorrect weekday")
	}

	return time.Weekday(weekday), nil
}

// MarshalJSON converts a maintenance window definition to JSON.
func (m *MaintenanceWindow) MarshalJSON() ([]byte, error) {
	if m.Everyday {
		return []byte(fmt.Sprintf("\"%s-%s\"",
			m.StartTime.Format("15:04"),
			m.EndTime.Format("15:04"))), nil
	}

	return []byte(fmt.Sprintf("\"%s:%s-%s\"",
		m.Weekday.String()[:3],
		m.StartTime.Format("15:04"),
		m.EndTime.Format("15:04"))), nil
}

// UnmarshalJSON convets a JSON to the maintenance window definition.
func (m *MaintenanceWindow) UnmarshalJSON(data []byte) error {
	var (
		got MaintenanceWindow
		err error
	)

	parts := strings.Split(string(data[1:len(data)-1]), "-")
	if len(parts) != 2 {
		return fmt.Errorf("incorrect maintenance window format")
	}

	fromParts := strings.Split(parts[0], ":")
	switch len(fromParts) {
	case 3:
		got.Everyday = false
		got.Weekday, err = parseWeekday(fromParts[0])
		if err != nil {
			return fmt.Errorf("could not parse weekday: %v", err)
		}

		got.StartTime, err = parseTime(fromParts[1] + ":" + fromParts[2])
	case 2:
		got.Everyday = true
		got.StartTime, err = parseTime(fromParts[0] + ":" + fromParts[1])
	default:
		return fmt.Errorf("incorrect maintenance window format")
	}
	if err != nil {
		return fmt.Errorf("could not parse start time: %v", err)
	}

	got.EndTime, err = parseTime(parts[1])
	if err != nil {
		return fmt.Errorf("could not parse end time: %v", err)
	}

	if got.EndTime.Before(got.StartTime) {
		return fmt.Errorf("'From' time must be prior to the 'To' time")
	}

	*m = got

	return nil
}

func extractClusterName(clusterName string, teamName string) (string, error) {
	teamNameLen := len(teamName)
	if len(clusterName) < teamNameLen+2 {
		return "", fmt.Errorf("name is too short")
	}

	if teamNameLen == 0 {
		return "", fmt.Errorf("team name is empty")
	}

	if strings.ToLower(clusterName[:teamNameLen+1]) != strings.ToLower(teamName)+"-" {
		return "", fmt.Errorf("name must match {TEAM}-{NAME} format")
	}
	if len(clusterName) > clusterNameMaxLength {
		return "", fmt.Errorf("name cannot be longer than %d characters", clusterNameMaxLength)
	}
	if !serviceNameRegex.MatchString(clusterName) {
		return "", fmt.Errorf("name must confirm to DNS-1035, regex used for validation is %q",
			serviceNameRegexString)
	}

	return clusterName[teamNameLen+1:], nil
}

func validateCloneClusterDescription(clone *CloneDescription) error {
	// when cloning from the basebackup (no end timestamp) check that the cluster name is a valid service name
	if clone.ClusterName != "" && clone.EndTimestamp == "" {
		if !serviceNameRegex.MatchString(clone.ClusterName) {
			return fmt.Errorf("clone cluster name must confirm to DNS-1035, regex used for validation is %q",
				serviceNameRegexString)
		}
		if len(clone.ClusterName) > serviceNameMaxLength {
			return fmt.Errorf("clone cluster name must be no longer than %d characters", serviceNameMaxLength)
		}
	}
	return nil
}

type postgresqlListCopy PostgresqlList
type postgresqlCopy Postgresql

// UnmarshalJSON converts a JSON into the PostgreSQL object.
func (p *Postgresql) UnmarshalJSON(data []byte) error {
	var tmp postgresqlCopy

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		metaErr := json.Unmarshal(data, &tmp.ObjectMeta)
		if metaErr != nil {
			return err
		}

		tmp.Error = err
		tmp.Status = ClusterStatusInvalid

		*p = Postgresql(tmp)

		return nil
	}
	tmp2 := Postgresql(tmp)

	if clusterName, err := extractClusterName(tmp2.ObjectMeta.Name, tmp2.Spec.TeamID); err != nil {
		tmp2.Error = err
		tmp2.Status = ClusterStatusInvalid
	} else if err := validateCloneClusterDescription(&tmp2.Spec.Clone); err != nil {
		tmp2.Error = err
		tmp2.Status = ClusterStatusInvalid
	} else {
		tmp2.Spec.ClusterName = clusterName
	}

	*p = tmp2

	return nil
}

// UnmarshalJSON converts a JSON into the PostgreSQL List object.
func (pl *PostgresqlList) UnmarshalJSON(data []byte) error {
	var tmp postgresqlListCopy

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PostgresqlList(tmp)
	*pl = tmp2

	return nil
}
