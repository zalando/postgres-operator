package patroni

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/zalando/postgres-operator/mocks"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	v1 "k8s.io/api/core/v1"
)

var logger = logrus.New().WithField("test", "patroni")

func newMockPod(ip string) *v1.Pod {
	return &v1.Pod{
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func TestApiURL(t *testing.T) {
	var testTable = []struct {
		podIP            string
		expectedResponse string
		expectedError    error
	}{
		{
			"127.0.0.1",
			fmt.Sprintf("http://127.0.0.1:%d", ApiPort),
			nil,
		},
		{
			"0000:0000:0000:0000:0000:0000:0000:0001",
			fmt.Sprintf("http://[::1]:%d", ApiPort),
			nil,
		},
		{
			"::1",
			fmt.Sprintf("http://[::1]:%d", ApiPort),
			nil,
		},
		{
			"",
			"",
			errors.New(" is not a valid IP"),
		},
		{
			"foobar",
			"",
			errors.New("foobar is not a valid IP"),
		},
		{
			"127.0.1",
			"",
			errors.New("127.0.1 is not a valid IP"),
		},
		{
			":::",
			"",
			errors.New("::: is not a valid IP"),
		},
	}
	for _, test := range testTable {
		resp, err := apiURL(newMockPod(test.podIP))
		if resp != test.expectedResponse {
			t.Errorf("expected response %v does not match the actual %v", test.expectedResponse, resp)
		}
		if err != test.expectedError {
			if err == nil || test.expectedError == nil {
				t.Errorf("expected error '%v' does not match the actual error '%v'", test.expectedError, err)
			}
			if err != nil && test.expectedError != nil && err.Error() != test.expectedError.Error() {
				t.Errorf("expected error '%v' does not match the actual error '%v'", test.expectedError, err)
			}
		}
	}
}

func TestGetClusterMembers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expectedClusterMemberData := []ClusterMember{
		{
			Name:     "acid-test-cluster-0",
			Role:     "leader",
			State:    "running",
			Timeline: 1,
		}, {
			Name:     "acid-test-cluster-1",
			Role:     "sync_standby",
			State:    "running",
			Timeline: 1,
			Lag:      0,
		}, {
			Name:     "acid-test-cluster-2",
			Role:     "replica",
			State:    "running",
			Timeline: 1,
			Lag:      math.MaxUint64,
		}, {
			Name:     "acid-test-cluster-3",
			Role:     "replica",
			State:    "running",
			Timeline: 1,
			Lag:      3000000000,
		}}

	json := `{"members": [
		{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1},
		{"name": "acid-test-cluster-1", "role": "sync_standby", "state": "running", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 0},
		{"name": "acid-test-cluster-2", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": "unknown"},
		{"name": "acid-test-cluster-3", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 3000000000}
		]}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(json)))

	response := http.Response{
		StatusCode: 200,
		Body:       r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Get(gomock.Any()).Return(&response, nil)

	p := New(logger, mockClient)

	clusterMemberData, err := p.GetClusterMembers(newMockPod("192.168.100.1"))

	if !reflect.DeepEqual(expectedClusterMemberData, clusterMemberData) {
		t.Errorf("Patroni cluster members differ: expected: %#v, got: %#v", expectedClusterMemberData, clusterMemberData)
	}

	if err != nil {
		t.Errorf("Could not read Patroni data: %v", err)
	}
}

func TestGetMemberData(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expectedMemberData := MemberData{
		State:          "running",
		Role:           "master",
		ServerVersion:  130004,
		PendingRestart: true,
		Patroni: MemberDataPatroni{
			Version: "2.1.1",
			Scope:   "acid-test-cluster",
		},
	}

	json := `{"state": "running", "postmaster_start_time": "2021-02-19 14:31:50.053 CET", "role": "master", "server_version": 130004, "cluster_unlocked": false, "xlog": {"location": 123456789}, "timeline": 1, "database_system_identifier": "6462555844314089962", "pending_restart": true, "patroni": {"version": "2.1.1", "scope": "acid-test-cluster"}}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(json)))

	response := http.Response{
		StatusCode: 200,
		Body:       r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Get(gomock.Any()).Return(&response, nil)

	p := New(logger, mockClient)

	memberData, err := p.GetMemberData(newMockPod("192.168.100.1"))

	if !reflect.DeepEqual(expectedMemberData, memberData) {
		t.Errorf("Patroni member data differs: expected: %#v, got: %#v", expectedMemberData, memberData)
	}

	if err != nil {
		t.Errorf("Could not read Patroni data: %v", err)
	}
}

func TestGetConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expectedPatroniConfig := acidv1.Patroni{
		TTL:                  30,
		LoopWait:             10,
		RetryTimeout:         10,
		MaximumLagOnFailover: 33554432,
		Slots: map[string]map[string]string{
			"cdc": {
				"database": "foo",
				"plugin":   "pgoutput",
				"type":     "logical",
			},
		},
	}

	expectedPgParameters := map[string]string{
		"archive_mode":                    "on",
		"archive_timeout":                 "1800s",
		"autovacuum_analyze_scale_factor": "0.02",
		"autovacuum_max_workers":          "5",
		"autovacuum_vacuum_scale_factor":  "0.05",
		"checkpoint_completion_target":    "0.9",
		"hot_standby":                     "on",
		"log_autovacuum_min_duration":     "0",
		"log_checkpoints":                 "on",
		"log_connections":                 "on",
		"log_disconnections":              "on",
		"log_line_prefix":                 "%t [%p]: [%l-1] %c %x %d %u %a %h ",
		"log_lock_waits":                  "on",
		"log_min_duration_statement":      "500",
		"log_statement":                   "ddl",
		"log_temp_files":                  "0",
		"max_connections":                 "100",
		"max_replication_slots":           "10",
		"max_wal_senders":                 "10",
		"tcp_keepalives_idle":             "900",
		"tcp_keepalives_interval":         "100",
		"track_functions":                 "all",
		"wal_level":                       "hot_standby",
		"wal_log_hints":                   "on",
	}

	configJson := `{"loop_wait": 10, "maximum_lag_on_failover": 33554432, "postgresql": {"parameters": {"archive_mode": "on", "archive_timeout": "1800s", "autovacuum_analyze_scale_factor": 0.02, "autovacuum_max_workers": 5, "autovacuum_vacuum_scale_factor": 0.05, "checkpoint_completion_target": 0.9, "hot_standby": "on", "log_autovacuum_min_duration": 0, "log_checkpoints": "on", "log_connections": "on", "log_disconnections": "on", "log_line_prefix": "%t [%p]: [%l-1] %c %x %d %u %a %h ", "log_lock_waits": "on", "log_min_duration_statement": 500, "log_statement": "ddl", "log_temp_files": 0, "max_connections": 100, "max_replication_slots": 10, "max_wal_senders": 10, "tcp_keepalives_idle": 900, "tcp_keepalives_interval": 100, "track_functions": "all", "wal_level": "hot_standby", "wal_log_hints": "on"}, "use_pg_rewind": true, "use_slots": true}, "retry_timeout": 10, "slots": {"cdc": {"database": "foo", "plugin": "pgoutput", "type": "logical"}}, "ttl": 30}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(configJson)))

	response := http.Response{
		StatusCode: 200,
		Body:       r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Get(gomock.Any()).Return(&response, nil)

	p := New(logger, mockClient)

	patroniConfig, pgParameters, err := p.GetConfig(newMockPod("192.168.100.1"))
	if err != nil {
		t.Errorf("Could not read Patroni config endpoint: %v", err)
	}

	if !reflect.DeepEqual(expectedPatroniConfig, patroniConfig) {
		t.Errorf("Patroni config differs: expected: %#v, got: %#v", expectedPatroniConfig, patroniConfig)
	}
	if !reflect.DeepEqual(expectedPgParameters, pgParameters) {
		t.Errorf("Postgre parameters differ: expected: %#v, got: %#v", expectedPgParameters, pgParameters)
	}
}

func TestSetPostgresParameters(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	parametersToSet := map[string]string{
		"max_connections": "50",
		"wal_level":       "logical",
	}

	configJson := `{"loop_wait": 10, "maximum_lag_on_failover": 33554432, "postgresql": {"parameters": {"archive_mode": "on", "archive_timeout": "1800s", "autovacuum_analyze_scale_factor": 0.02, "autovacuum_max_workers": 5, "autovacuum_vacuum_scale_factor": 0.05, "checkpoint_completion_target": 0.9, "hot_standby": "on", "log_autovacuum_min_duration": 0, "log_checkpoints": "on", "log_connections": "on", "log_disconnections": "on", "log_line_prefix": "%t [%p]: [%l-1] %c %x %d %u %a %h ", "log_lock_waits": "on", "log_min_duration_statement": 500, "log_statement": "ddl", "log_temp_files": 0, "max_connections": 50, "max_replication_slots": 10, "max_wal_senders": 10, "tcp_keepalives_idle": 900, "tcp_keepalives_interval": 100, "track_functions": "all", "wal_level": "logical", "wal_log_hints": "on"}, "use_pg_rewind": true, "use_slots": true}, "retry_timeout": 10, "slots": {"cdc": {"database": "foo", "plugin": "pgoutput", "type": "logical"}}, "ttl": 30}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(configJson)))

	response := http.Response{
		StatusCode: 200,
		Body:       r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Do(gomock.Any()).Return(&response, nil)

	p := New(logger, mockClient)

	err := p.SetPostgresParameters(newMockPod("192.168.100.1"), parametersToSet)
	if err != nil {
		t.Errorf("could not call patch Patroni config: %v", err)
	}

}
