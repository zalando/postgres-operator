package apiserver

import (
	"testing"
)

const (
	clusterStatusTest        = "/clusters/test-namespace/testcluster/"
	clusterStatusNumericTest = "/clusters/test-namespace-1/testcluster/"
	clusterLogsTest          = "/clusters/test-namespace/testcluster/logs/"
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
