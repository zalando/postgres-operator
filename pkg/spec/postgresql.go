package spec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
)

var alphaRegexp = regexp.MustCompile("^[a-zA-Z]*$")

type MaintenanceWindow struct {
	StartTime    time.Time    // Start time
	StartWeekday time.Weekday // Start weekday

	EndTime    time.Time    // End time
	EndWeekday time.Weekday // End weekday
}

type Volume struct {
	Size         string `json:"size"`
	StorageClass string `json:"storageClass"`
}

type PostgresqlParam struct {
	PgVersion  string            `json:"version"`
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

type PostgresStatus string

const (
	ClusterStatusUnknown      PostgresStatus = ""
	ClusterStatusCreating     PostgresStatus = "Creating"
	ClusterStatusUpdating     PostgresStatus = "Updating"
	ClusterStatusUpdateFailed PostgresStatus = "UpdateFailed"
	ClusterStatusAddFailed    PostgresStatus = "CreateFailed"
	ClusterStatusRunning      PostgresStatus = "Running"
)

// PostgreSQL Third Party (resource) Object
type Postgresql struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             v1.ObjectMeta `json:"metadata"`

	Spec   PostgresSpec   `json:"spec"`
	Status PostgresStatus `json:"status"`
}

type PostgresSpec struct {
	PostgresqlParam `json:"postgresql"`
	Volume          `json:"volume,omitempty"`
	Patroni         `json:"patroni,omitempty"`
	Resources       `json:"resources,omitempty"`

	TeamId              string               `json:"teamId"`
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

func parseTime(s string) (t time.Time, wd time.Weekday, wdProvided bool, err error) {
	var timeLayout string

	parts := strings.Split(s, ":")
	if len(parts) == 3 {
		if len(parts[0]) != 3 || !alphaRegexp.MatchString(parts[0]) {
			err = fmt.Errorf("Weekday must be 3 characters length")
			return
		}
		timeLayout = "Mon:15:04"
		wdProvided = true
	} else {
		wdProvided = false
		timeLayout = "15:04"
	}

	tp, err := time.Parse(timeLayout, s)
	if err != nil {
		return
	}

	wd = tp.Weekday()
	t = tp.UTC()

	return
}

func (m *MaintenanceWindow) MarshalJSON() ([]byte, error) {
	var startWd, endWd string
	if m.StartWeekday == time.Sunday && m.EndWeekday == time.Saturday {
		startWd = ""
		endWd = ""
	} else {
		startWd = m.StartWeekday.String()[:3] + ":"
		endWd = m.EndWeekday.String()[:3] + ":"
	}

	return []byte(fmt.Sprintf("\"%s%s-%s%s\"",
		startWd, m.StartTime.Format("15:04"),
		endWd, m.EndTime.Format("15:04"))), nil
}

func (m *MaintenanceWindow) UnmarshalJSON(data []byte) error {
	var (
		got                 MaintenanceWindow
		weekdayProvidedFrom bool
		weekdayProvidedTo   bool
		err                 error
	)

	parts := strings.Split(string(data[1:len(data)-1]), "-")
	if len(parts) != 2 {
		return fmt.Errorf("Incorrect maintenance window format")
	}

	got.StartTime, got.StartWeekday, weekdayProvidedFrom, err = parseTime(parts[0])
	if err != nil {
		return err
	}

	got.EndTime, got.EndWeekday, weekdayProvidedTo, err = parseTime(parts[1])
	if err != nil {
		return err
	}

	if got.EndTime.Before(got.StartTime) {
		return fmt.Errorf("'From' time must be prior to the 'To' time.")
	}

	if !weekdayProvidedFrom || !weekdayProvidedTo {
		got.StartWeekday = time.Sunday
		got.EndWeekday = time.Saturday
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

// The code below is used only to work around a known problem with third-party
// resources and ugorji. If/when these issues are resolved, the code below
// should no longer be required.
//
type PostgresqlListCopy PostgresqlList
type PostgresqlCopy Postgresql

func clusterName(clusterName string, teamName string) (string, error) {
	teamNameLen := len(teamName)
	if len(clusterName) < teamNameLen+2 {
		return "", fmt.Errorf("Name is too short")
	}
	if strings.ToLower(clusterName[:teamNameLen+1]) != strings.ToLower(teamName) + "-" {
		return "", fmt.Errorf("Name must start with the team name and dash")
	}

	return clusterName[teamNameLen+1:], nil
}

func (p *Postgresql) UnmarshalJSON(data []byte) error {
	tmp := PostgresqlCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := Postgresql(tmp)

	clusterName, err := clusterName(tmp2.Metadata.Name, tmp2.Spec.TeamId)
	if err != nil {
		return err
	}
	tmp2.Spec.ClusterName = clusterName
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
