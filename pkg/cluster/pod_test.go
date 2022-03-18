package cluster

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/zalando/postgres-operator/mocks"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/patroni"
)

func TestGetSwitchoverCandidate(t *testing.T) {
	testName := "test getting right switchover candidate"
	namespace := "default"

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PatroniAPICheckInterval: time.Duration(1),
				PatroniAPICheckTimeout:  time.Duration(5),
			},
		}, k8sutil.KubernetesClient{}, acidv1.Postgresql{}, logger, eventRecorder)

	// simulate different member scenarios
	tests := []struct {
		subtest           string
		clusterJson       string
		expectedCandidate spec.NamespacedName
		expectedError     error
	}{
		{
			subtest:           "choose sync_standby over replica",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "sync_standby", "state": "running", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 0}, {"name": "acid-test-cluster-2", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 0}]}`,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-1"},
			expectedError:     nil,
		},
		{
			subtest:           "choose replica with lowest lag",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "running", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 5}, {"name": "acid-test-cluster-2", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 2}]}`,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-2"},
			expectedError:     nil,
		},
		{
			subtest:           "choose first replica when lag is equal evrywhere",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "running", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 5}, {"name": "acid-test-cluster-2", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 5}]}`,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-1"},
			expectedError:     nil,
		},
		{
			subtest:           "no running replica available",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 2}, {"name": "acid-test-cluster-1", "role": "replica", "state": "starting", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 2}]}`,
			expectedCandidate: spec.NamespacedName{},
			expectedError:     fmt.Errorf("no switchover candidate found"),
		},
	}

	for _, tt := range tests {
		// mocking cluster members
		r := ioutil.NopCloser(bytes.NewReader([]byte(tt.clusterJson)))

		response := http.Response{
			StatusCode: 200,
			Body:       r,
		}

		mockClient := mocks.NewMockHTTPClient(ctrl)
		mockClient.EXPECT().Get(gomock.Any()).Return(&response, nil).AnyTimes()

		p := patroni.New(patroniLogger, mockClient)
		cluster.patroni = p
		mockMasterPod := newMockPod("192.168.100.1")
		mockMasterPod.Namespace = namespace

		candidate, err := cluster.getSwitchoverCandidate(mockMasterPod)
		if err != nil && err.Error() != tt.expectedError.Error() {
			t.Errorf("%s - %s: unexpected error, %v", testName, tt.subtest, err)
		}

		if candidate != tt.expectedCandidate {
			t.Errorf("%s - %s: unexpect switchover candidate, got %s, expected %s", testName, tt.subtest, candidate, tt.expectedCandidate)
		}
	}
}
