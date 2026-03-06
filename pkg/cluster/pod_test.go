func TestPodIsNotRunning(t *testing.T) {
	tests := []struct {
		subtest  string
		pod      v1.Pod
		expected bool
	}{
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
			subtest: "pod running with mixed container states",
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
