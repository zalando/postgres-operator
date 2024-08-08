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

	cluster.Spec.MaintenanceWindows = []acidv1.MaintenanceWindow{
		{
			Everyday:  true,
			StartTime: mustParseTime("00:00"),
			EndTime:   mustParseTime("23:59"),
		},
		{
			Weekday:   time.Monday,
			StartTime: mustParseTime("00:00"),
			EndTime:   mustParseTime("23:59"),
		},
	}
	if !cluster.isInMainternanceWindow() {
		t.Errorf("Expected isInMainternanceWindow to return true")
	}

	now := time.Now()
	futureTimeStart := now.Add(1 * time.Hour)
	futureTimeStartFormatted := futureTimeStart.Format("15:04")
	futureTimeEnd := now.Add(1 * time.Hour)
	futureTimeEndFormatted := futureTimeEnd.Format("15:04")

	cluster.Spec.MaintenanceWindows = []acidv1.MaintenanceWindow{
		{
			Weekday:   now.Weekday(),
			StartTime: mustParseTime(futureTimeStartFormatted),
			EndTime:   mustParseTime(futureTimeEndFormatted),
		},
	}
	if cluster.isInMainternanceWindow() {
		t.Errorf("Expected isInMainternanceWindow to return false")
	}

}
