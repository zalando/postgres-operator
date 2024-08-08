package cluster

import (
	"testing"
	"time"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mustParseTime(s string) metav1.Time {
	v, err := time.Parse("15:04", s)
	if err != nil {
		panic(err)
	}

	return metav1.Time{Time: v.UTC()}
}

func TestIsInMaintenanceWindow(t *testing.T) {
	client, _ := newFakeK8sStreamClient()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					DefaultCPURequest:    "300m",
					DefaultCPULimit:      "300m",
					DefaultMemoryRequest: "300Mi",
					DefaultMemoryLimit:   "300Mi",
					PodRoleLabel:         "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	now := time.Now()
	futureTimeStart := now.Add(1 * time.Hour)
	futureTimeStartFormatted := futureTimeStart.Format("15:04")
	futureTimeEnd := now.Add(2 * time.Hour)
	futureTimeEndFormatted := futureTimeEnd.Format("15:04")

	tests := []struct {
		name     string
		windows  []acidv1.MaintenanceWindow
		expected bool
	}{
		{
			name:     "no maintenance windows",
			windows:  nil,
			expected: true,
		},
		{
			name: "maintenance windows with everyday",
			windows: []acidv1.MaintenanceWindow{
				{
					Everyday:  true,
					StartTime: mustParseTime("00:00"),
					EndTime:   mustParseTime("23:59"),
				},
			},
			expected: true,
		},
		{
			name: "maintenance windows with weekday",
			windows: []acidv1.MaintenanceWindow{
				{
					Weekday:   now.Weekday(),
					StartTime: mustParseTime("00:00"),
					EndTime:   mustParseTime("23:59"),
				},
			},
			expected: true,
		},
		{
			name: "maintenance windows with future interval time",
			windows: []acidv1.MaintenanceWindow{
				{
					Weekday:   now.Weekday(),
					StartTime: mustParseTime(futureTimeStartFormatted),
					EndTime:   mustParseTime(futureTimeEndFormatted),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster.Spec.MaintenanceWindows = tt.windows
			if cluster.isInMainternanceWindow() != tt.expected {
				t.Errorf("Expected isInMainternanceWindow to return %t", tt.expected)
			}
		})
	}
}
