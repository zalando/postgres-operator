package spec

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
)

var pTests = []struct {
	s               string
	time            time.Time
	weekday         time.Weekday
	weekdayProvided bool
}{
	{"Mon:16:08", mustParseTime("16:08"), time.Monday, true},
	{"Sun:11:00", mustParseTime("11:00"), time.Sunday, true},
	{"23:59", mustParseTime("23:59"), time.Weekday(0), false},
}

var pErr = []string{"Thr:00:12", "26:09", "Std:26:09", "Saturday:00:00"}

var clusterNames = []struct {
	s           string
	team        string
	clusterName string
}{
	{"acid-test", "acid", "test"},
	{"test-my-name", "test", "my-name"},
	{"my-team-another-test", "my-team", "another-test"},
	{"------strange-team-cluster", "-----", "strange-team-cluster"},
}

var wrongClusterNames = []struct {
	s    string
	team string
}{
	{"acid-test", "test"},
	{"-test", ""},
	{"-test", "-"},
	{"", "-"},
	{"-", "-"},
}

var maintenanceWindows = []struct {
	s string
	m MaintenanceWindow
}{{`"10:00-20:00"`,
	MaintenanceWindow{
		StartTime:    mustParseTime("10:00"),
		StartWeekday: time.Monday,
		EndTime:      mustParseTime("20:00"),
		EndWeekday:   time.Sunday,
	}},
	{`"Tue:10:00-Sun:23:00"`,
		MaintenanceWindow{
			StartTime:    mustParseTime("10:00"),
			StartWeekday: time.Tuesday,
			EndTime:      mustParseTime("23:00"),
			EndWeekday:   time.Sunday,
		}},
	{`"Mon:10:00-Mon:10:00"`,
		MaintenanceWindow{
			StartTime:    mustParseTime("10:00"),
			StartWeekday: time.Monday,
			EndTime:      mustParseTime("10:00"),
			EndWeekday:   time.Monday,
		}},
	{`"Sun:00:00-Sun:00:00"`,
		MaintenanceWindow{
			StartTime:    mustParseTime("00:00"),
			StartWeekday: time.Sunday,
			EndTime:      mustParseTime("00:00"),
			EndWeekday:   time.Sunday,
		}},
	{`"00:00-10:00"`,
		MaintenanceWindow{
			StartTime:    mustParseTime("00:00"),
			StartWeekday: time.Monday,
			EndTime:      mustParseTime("10:00"),
			EndWeekday:   time.Sunday,
		}},
	{`"00:00-00:00"`,
		MaintenanceWindow{
			StartTime:    mustParseTime("00:00"),
			StartWeekday: time.Monday,
			EndTime:      mustParseTime("00:00"),
			EndWeekday:   time.Sunday,
		}},
}

var wrongMaintenanceWindows = [][]byte{
	[]byte(`"Mon:12:00-Sun:11:00"`),
	[]byte(`"Mon:12:00-Mon:11:00"`),
	[]byte(`"Wed:00:00-Tue:26:00"`),
	[]byte(`"Sun:00:00-Mon:00:00"`),
	[]byte(`"Wed:00:00-Mon:10:00"`),
	[]byte(`"10:00-00:00"`),
	[]byte(`"Mon:00:00:00-Tue:10:00:00"`),
	[]byte(`"Mon:00:00"`),
}

var unmarshalCluster = []struct {
	in  []byte
	out Postgresql
}{{
	[]byte(`{
  "kind": "Postgresql","apiVersion": "acid.zalan.do/v1",
  "metadata": {"name": "acid-testcluster1"}, "spec": {"teamId": 100}}`),
	Postgresql{
		TypeMeta: unversioned.TypeMeta{
			Kind:       "Postgresql",
			APIVersion: "acid.zalan.do/v1",
		},
		Metadata: v1.ObjectMeta{
			Name: "acid-testcluster1",
		},
		Status: ClusterStatusInvalid,
		Error: &json.UnmarshalTypeError{
			Value:  "number",
			Type:   reflect.TypeOf(""),
			Offset: 126,
			Struct: "PostgresSpec",
			Field:  "teamId",
		},
	}},
	{[]byte(`{
  "kind": "Postgresql",
  "apiVersion": "acid.zalan.do/v1",
  "metadata": {
    "name": "acid-testcluster1"
  },
  "spec": {
    "teamId": "ACID",
    "volume": {
      "size": "5Gi",
      "storageClass": "SSD"
    },
    "numberOfInstances": 2,
    "users": {
      "zalando": [
        "superuser",
        "createdb"
      ]
    },
    "allowedSourceRanges": [
      "127.0.0.1/32"
    ],
    "postgresql": {
      "version": "9.6",
      "parameters": {
        "shared_buffers": "32MB",
        "max_connections": "10",
        "log_statement": "all"
      }
    },
    "resources": {
      "requests": {
        "cpu": "10m",
        "memory": "50Mi"
      },
      "limits": {
        "cpu": "300m",
        "memory": "3000Mi"
      }
    },
    "patroni": {
      "initdb": {
        "encoding": "UTF8",
        "locale": "en_US.UTF-8",
        "data-checksums": "true"
      },
      "pg_hba": [
        "hostssl all all 0.0.0.0/0 md5",
        "host    all all 0.0.0.0/0 md5"
      ],
      "ttl": 30,
      "loop_wait": 10,
      "retry_timeout": 10,
      "maximum_lag_on_failover": 33554432
    },
    "maintenanceWindows": [
      "01:00-06:00",
      "Sat:00:00-Sat:04:00"
    ]
  }
}`),
		Postgresql{
			TypeMeta: unversioned.TypeMeta{
				Kind:       "Postgresql",
				APIVersion: "acid.zalan.do/v1",
			},
			Metadata: v1.ObjectMeta{
				Name: "acid-testcluster1",
			},
			Spec: PostgresSpec{
				PostgresqlParam: PostgresqlParam{
					PgVersion: "9.6",
					Parameters: map[string]string{
						"shared_buffers":  "32MB",
						"max_connections": "10",
						"log_statement":   "all",
					},
				},
				Volume: Volume{
					Size:         "5Gi",
					StorageClass: "SSD",
				},
				Patroni: Patroni{
					InitDB: map[string]string{
						"encoding":       "UTF8",
						"locale":         "en_US.UTF-8",
						"data-checksums": "true",
					},
					PgHba:                []string{"hostssl all all 0.0.0.0/0 md5", "host    all all 0.0.0.0/0 md5"},
					TTL:                  30,
					LoopWait:             10,
					RetryTimeout:         10,
					MaximumLagOnFailover: 33554432,
				},
				Resources: Resources{
					ResourceRequest: ResourceDescription{CPU: "10m", Memory: "50Mi"},
					ResourceLimits:  ResourceDescription{CPU: "300m", Memory: "3000Mi"},
				},
				TeamID:              "ACID",
				AllowedSourceRanges: []string{"127.0.0.1/32"},
				NumberOfInstances:   2,
				Users:               map[string]UserFlags{"zalando": {"superuser", "createdb"}},
				MaintenanceWindows: []MaintenanceWindow{{
					StartWeekday: time.Monday,
					StartTime:    mustParseTime("01:00"),
					EndTime:      mustParseTime("06:00"),
					EndWeekday:   time.Sunday,
				},
					{
						StartWeekday: time.Saturday,
						StartTime:    mustParseTime("00:00"),
						EndTime:      mustParseTime("04:00"),
						EndWeekday:   time.Saturday,
					},
				},
				ClusterName: "testcluster1",
			},
			Error: nil,
		}},
	{
		[]byte(`{"kind": "Postgresql","apiVersion": "acid.zalan.do/v1","metadata": {"name": "teapot-testcluster1"}, "spec": {"teamId": "acid"}}`),
		Postgresql{
			TypeMeta: unversioned.TypeMeta{
				Kind:       "Postgresql",
				APIVersion: "acid.zalan.do/v1",
			},
			Metadata: v1.ObjectMeta{
				Name: "teapot-testcluster1",
			},
			Spec:   PostgresSpec{TeamID: "acid"},
			Status: ClusterStatusInvalid,
			Error:  errors.New("name must match {TEAM}-{NAME} format"),
		}},
}

var invalidClusterSpec = []struct {
	in  []byte
	err error
}{{[]byte(`{"kind": "Postgresql","apiVersion": "acid.zalan.do/v1"`),
	errors.New("unexpected end of JSON input"),
}}

func mustParseTime(s string) time.Time {
	v, err := time.Parse("15:04", s)
	if err != nil {
		panic(err)
	}

	return v
}

func TestParseTime(t *testing.T) {
	for _, tt := range pTests {
		aTime, weekday, weekdayProvided, err := ParseTime(tt.s)
		if err != nil {
			t.Errorf("ParseTime error: %v", err)
		}

		if aTime != tt.time {
			t.Errorf("Expected time: %v, got: %v", tt.time, aTime)
		}

		if weekday != tt.weekday {
			t.Errorf("Expected weekday: %v, got: %v", tt.weekday, weekday)
		}

		if weekdayProvided != tt.weekdayProvided {
			t.Errorf("Expected weekdayProvided: %t, got: %t", tt.weekdayProvided, weekdayProvided)
		}
	}
}

func TestParseTimeError(t *testing.T) {
	for _, tt := range pErr {
		_, _, _, err := ParseTime(tt)
		if err == nil {
			t.Errorf("Error expected for '%s'", tt)
		}
	}
}

func TestClusterName(t *testing.T) {
	for _, tt := range clusterNames {
		name, err := extractClusterName(tt.s, tt.team)
		if err != nil {
			t.Errorf("extractClusterName error: %v", err)
		}
		if name != tt.clusterName {
			t.Errorf("Expected cluserName: %s, got: %s", tt.clusterName, name)
		}
	}
}

func TestClusterNameError(t *testing.T) {
	for _, tt := range wrongClusterNames {
		_, err := extractClusterName(tt.s, tt.team)
		if err == nil {
			t.Errorf("Error expected for '%s'", tt)
		}
	}
}

func TestUnmarshalMaintenanceWindow(t *testing.T) {
	for _, tt := range maintenanceWindows {
		var m MaintenanceWindow
		err := m.UnmarshalJSON([]byte(tt.s))
		if err != nil {
			t.Errorf("Unmarshal Error: %v", err)
		}

		if !reflect.DeepEqual(m, tt.m) {
			t.Errorf("Expected maintenace window: %#v, got: %#v", tt.m, m)
		}
	}
}

func TestMarshalMaintenanceWindow(t *testing.T) {
	for _, tt := range maintenanceWindows {
		s, err := tt.m.MarshalJSON()
		if err != nil {
			t.Errorf("Marshal Error: %v", err)
		}

		if string(s) != tt.s {
			t.Errorf("Expected Marshal: %s, got: %s", tt.s, string(s))
		}
	}
}

func TestUnmarshalMaintWindowsErrs(t *testing.T) {
	for _, tt := range wrongMaintenanceWindows {
		var m MaintenanceWindow
		err := m.UnmarshalJSON(tt)
		if err == nil {
			t.Errorf("Error expected for '%s'", tt)
		}
	}
}

func TestPostgresUnmarshal(t *testing.T) {
	for _, tt := range unmarshalCluster {
		var cluster Postgresql
		err := cluster.UnmarshalJSON(tt.in)
		if err != nil {
			t.Errorf("Unmarshal Error: %v", err)
		}

		if !reflect.DeepEqual(cluster, tt.out) {
			t.Errorf("Expected Postgresql: %#v, got %#v", tt.out, cluster)
		}
	}
}

func TestInvalidPostgresUnmarshal(t *testing.T) {
	for _, tt := range invalidClusterSpec {
		var cluster Postgresql
		err := cluster.UnmarshalJSON(tt.in)
		if err == nil {
			t.Errorf("Error expected for %s", string(tt.in))
		}

		if err.Error() != tt.err.Error() {
			t.Errorf("Unmarshal error expected: %v, got: %v", tt.err, err)
		}
	}
}
