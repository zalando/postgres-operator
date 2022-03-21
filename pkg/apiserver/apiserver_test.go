package apiserver

import (
	"testing"
)

const (
	clusterStatusTest        = "/clusters/test-id/test_namespace/testcluster/"
	clusterStatusNumericTest = "/clusters/test-id-1/test_namespace/testcluster/"
	clusterLogsTest          = "/clusters/test-id/test_namespace/testcluster/logs/"
	teamTest                 = "/clusters/test-id/"
)

func TestUrlRegexps(t *testing.T) {
	if clusterStatusURL.FindStringSubmatch(clusterStatusTest) == nil {
		t.Errorf("clusterStatusURL can't match %s", clusterStatusTest)
	}

	if clusterStatusURL.FindStringSubmatch(clusterStatusNumericTest) == nil {
		t.Errorf("clusterStatusURL can't match %s", clusterStatusNumericTest)
	}

	if clusterLogsURL.FindStringSubmatch(clusterLogsTest) == nil {
		t.Errorf("clusterLogsURL can't match %s", clusterLogsTest)
	}

	if teamURL.FindStringSubmatch(teamTest) == nil {
		t.Errorf("teamURL can't match %s", teamTest)
	}
}
