package spec

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
)

type MaintenanceWindow struct {
	Everyday  bool
	Weekday   time.Weekday
	StartTime time.Time // Start time
	EndTime   time.Time // End time
}

type Volume struct {
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
}

type PostgresqlParam struct {
	PgVersion  string            `json:"version"`
	Parameters map[string]string `json:"parameters"`
}

type ResourceDescription struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type Resources struct {
	ResourceRequest ResourceDescription `json:"requests,omitempty"`
	ResourceLimits  ResourceDescription `json:"limits,omitempty"`
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

type PostgresStatus string

const (
	ClusterStatusUnknown      PostgresStatus = ""
	ClusterStatusCreating     PostgresStatus = "Creating"
	ClusterStatusUpdating     PostgresStatus = "Updating"
	ClusterStatusUpdateFailed PostgresStatus = "UpdateFailed"
	ClusterStatusAddFailed    PostgresStatus = "CreateFailed"
	ClusterStatusRunning      PostgresStatus = "Running"
	ClusterStatusInvalid      PostgresStatus = "Invalid"
)

// PostgreSQL Third Party (resource) Object
type Postgresql struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             v1.ObjectMeta `json:"metadata"`

	Spec   PostgresSpec   `json:"spec"`
	Status PostgresStatus `json:"status,omitempty"`
	Error  error          `json:"-"`
}

type PostgresSpec struct {
	PostgresqlParam `json:"postgresql"`
	Volume          `json:"volume,omitempty"`
	Patroni         `json:"patroni,omitempty"`
	Resources       `json:"resources,omitempty"`

	TeamID              string               `json:"teamId"`
	AllowedSourceRanges []string             `json:"allowedSourceRanges"`
	NumberOfInstances   int32                `json:"numberOfInstances"`
	Users               map[string]UserFlags `json:"users"`
	MaintenanceWindows  []MaintenanceWindow  `json:"maintenanceWindows,omitempty"`
	ClusterName         string               `json:"-"`
}

type PostgresqlList struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             unversioned.ListMeta `json:"metadata"`

	Items []Postgresql `json:"items"`
}

var weekdays = map[string]int{"Sun": 0, "Mon": 1, "Tue": 2, "Wed": 3, "Thu": 4, "Fri": 5, "Sat": 6}

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

func (m *MaintenanceWindow) MarshalJSON() ([]byte, error) {
	if m.Everyday {
		return []byte(fmt.Sprintf("\"%s-%s\"",
			m.StartTime.Format("15:04"),
			m.EndTime.Format("15:04"))), nil
	} else {
		return []byte(fmt.Sprintf("\"%s:%s-%s\"",
			m.Weekday.String()[:3],
			m.StartTime.Format("15:04"),
			m.EndTime.Format("15:04"))), nil
	}
}

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

	return clusterName[teamNameLen+1:], nil
}

// The code below is used only to work around a known problem with third-party
// resources and ugorji. If/when these issues are resolved, the code below
// should no longer be required.
//
type PostgresqlListCopy PostgresqlList
type PostgresqlCopy Postgresql

func (p *Postgresql) UnmarshalJSON(data []byte) error {
	var tmp PostgresqlCopy

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		metaErr := json.Unmarshal(data, &tmp.Metadata)
		if metaErr != nil {
			return err
		}

		tmp.Error = err
		tmp.Status = ClusterStatusInvalid

		*p = Postgresql(tmp)

		return nil
	}
	tmp2 := Postgresql(tmp)

	clusterName, err := extractClusterName(tmp2.Metadata.Name, tmp2.Spec.TeamID)
	if err == nil {
		tmp2.Spec.ClusterName = clusterName
	} else {
		tmp2.Error = err
		tmp2.Status = ClusterStatusInvalid
	}
	*p = tmp2

	return nil
}

func (pl *PostgresqlList) UnmarshalJSON(data []byte) error {
	var tmp PostgresqlListCopy

	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := PostgresqlList(tmp)
	*pl = tmp2

	return nil
}
