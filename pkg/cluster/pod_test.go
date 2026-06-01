package cluster

import (
	"bytes"
	"fmt"
	"io"
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
	v1 "k8s.io/api/core/v1"
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
		syncModeEnabled   bool
		expectedCandidate spec.NamespacedName
		expectedError     error
	}{
		{
			subtest:           "choose sync_standby over replica",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "sync_standby", "state": "streaming", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 0}, {"name": "acid-test-cluster-2", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 0}]}`,
			syncModeEnabled:   true,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-1"},
			expectedError:     nil,
		},
		{
			subtest:           "no running sync_standby available",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 0}]}`,
			syncModeEnabled:   true,
			expectedCandidate: spec.NamespacedName{},
			expectedError:     fmt.Errorf("failed to get Patroni cluster members: unexpected end of JSON input"),
		},
		{
			subtest:           "choose replica with lowest lag",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 5}, {"name": "acid-test-cluster-2", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 2}]}`,
			syncModeEnabled:   false,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-2"},
			expectedError:     nil,
		},
		{
			subtest:           "choose first replica when lag is equal everywhere",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 5}, {"name": "acid-test-cluster-2", "role": "replica", "state": "running", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 5}]}`,
			syncModeEnabled:   false,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-1"},
			expectedError:     nil,
		},
		{
			subtest:           "no running replica available",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 2}, {"name": "acid-test-cluster-1", "role": "replica", "state": "starting", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 2}]}`,
			syncModeEnabled:   false,
			expectedCandidate: spec.NamespacedName{},
			expectedError:     fmt.Errorf("failed to get Patroni cluster members: unexpected end of JSON input"),
		},
		{
			subtest:           "replicas with different status",
			clusterJson:       `{"members": [{"name": "acid-test-cluster-0", "role": "leader", "state": "running", "api_url": "http://192.168.100.1:8008/patroni", "host": "192.168.100.1", "port": 5432, "timeline": 1}, {"name": "acid-test-cluster-1", "role": "replica", "state": "streaming", "api_url": "http://192.168.100.2:8008/patroni", "host": "192.168.100.2", "port": 5432, "timeline": 1, "lag": 5}, {"name": "acid-test-cluster-2", "role": "replica", "state": "in archive recovery", "api_url": "http://192.168.100.3:8008/patroni", "host": "192.168.100.3", "port": 5432, "timeline": 1, "lag": 2}]}`,
			syncModeEnabled:   false,
			expectedCandidate: spec.NamespacedName{Namespace: namespace, Name: "acid-test-cluster-2"},
			expectedError:     nil,
		},
	}

	for _, tt := range tests {
		// mocking cluster members
		r := io.NopCloser(bytes.NewReader([]byte(tt.clusterJson)))

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
		cluster.Spec.Patroni.SynchronousMode = tt.syncModeEnabled

		candidate, err := cluster.getSwitchoverCandidate(mockMasterPod)
		if err != nil && err.Error() != tt.expectedError.Error() {
			t.Errorf("%s - %s: unexpected error, %v", testName, tt.subtest, err)
		}

		if candidate != tt.expectedCandidate {
			t.Errorf("%s - %s: unexpect switchover candidate, got %s, expected %s", testName, tt.subtest, candidate, tt.expectedCandidate)
		}
	}
}

func TestPodIsNotRunning(t *testing.T) {
	tests := []struct {
		subtest  string
		pod      v1.Pod
		expected bool
	}{
		{
			subtest: "pod with no status reported yet",
			pod: v1.Pod{
				Status: v1.PodStatus{},
			},
			expected: false,
		},
		{
			subtest: "pod running with all containers ready",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			subtest: "pod in pending phase",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodPending,
				},
			},
			expected: true,
		},
		{
			subtest: "pod running but container in CreateContainerConfigError",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Waiting: &v1.ContainerStateWaiting{
									Reason:  "CreateContainerConfigError",
									Message: `secret "some-secret" not found`,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			subtest: "pod running but container in CrashLoopBackOff",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Waiting: &v1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			subtest: "pod running but container terminated",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Terminated: &v1.ContainerStateTerminated{
									ExitCode: 137,
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			subtest: "pod running with mixed container states - one healthy one broken",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
						{
							State: v1.ContainerState{
								Waiting: &v1.ContainerStateWaiting{
									Reason: "CreateContainerConfigError",
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			subtest: "pod in failed phase",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodFailed,
				},
			},
			expected: true,
		},
		{
			subtest: "pod running with multiple healthy containers",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
						{
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			subtest: "pod running with ImagePullBackOff",
			pod: v1.Pod{
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
					ContainerStatuses: []v1.ContainerStatus{
						{
							State: v1.ContainerState{
								Waiting: &v1.ContainerStateWaiting{
									Reason: "ImagePullBackOff",
								},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.subtest, func(t *testing.T) {
			result := podIsNotRunning(&tt.pod)
			if result != tt.expected {
				t.Errorf("podIsNotRunning() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestAllPodsRunning(t *testing.T) {
	client, _ := newFakeK8sSyncClient()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
					PodRoleLabel:     "spilo-role",
				},
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	tests := []struct {
		subtest  string
		pods     []v1.Pod
		expected bool
	}{
		{
			subtest: "all pods running",
			pods: []v1.Pod{
				{
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
						},
					},
				},
				{
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
						},
					},
				},
			},
			expected: true,
		},
		{
			subtest: "one pod not running",
			pods: []v1.Pod{
				{
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{State: v1.ContainerState{Running: &v1.ContainerStateRunning{}}},
						},
					},
				},
				{
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Waiting: &v1.ContainerStateWaiting{
										Reason: "CreateContainerConfigError",
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			subtest: "all pods not running",
			pods: []v1.Pod{
				{
					Status: v1.PodStatus{
						Phase: v1.PodPending,
					},
				},
				{
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
						ContainerStatuses: []v1.ContainerStatus{
							{
								State: v1.ContainerState{
									Waiting: &v1.ContainerStateWaiting{
										Reason: "CrashLoopBackOff",
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			subtest:  "empty pod list",
			pods:     []v1.Pod{},
			expected: true,
		},
		{
			subtest: "pods with no status reported yet",
			pods: []v1.Pod{
				{
					Status: v1.PodStatus{},
				},
				{
					Status: v1.PodStatus{},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.subtest, func(t *testing.T) {
			result := cluster.allPodsRunning(tt.pods)
			if result != tt.expected {
				t.Errorf("allPodsRunning() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
