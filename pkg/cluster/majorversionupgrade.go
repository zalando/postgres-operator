package cluster

import (
	"fmt"

	"github.com/zalando/postgres-operator/pkg/spec"
	v1 "k8s.io/api/core/v1"
)

// VersionMap Map of version numbers
var VersionMap = map[string]int{
	"9.5": 90500,
	"9.6": 90600,
	"10":  100000,
	"11":  110000,
	"12":  120000,
	"13":  130000,
}

// IsBiggerPostgresVersion Compare two Postgres version numbers
func IsBiggerPostgresVersion(old string, new string) bool {
	oldN, _ := VersionMap[old]
	newN, _ := VersionMap[new]
	return newN > oldN
}

// GetDesiredMajorVersionAsInt Convert string to comparable integer of PG version
func (c *Cluster) GetDesiredMajorVersionAsInt() int {
	return VersionMap[c.GetDesiredMajorVersion()]
}

// GetDesiredMajorVersion returns major version to use, incl. potential auto upgrade
func (c *Cluster) GetDesiredMajorVersion() string {

	if c.Config.OpConfig.MajorVersionUpgradeMode == "full" {
		// current is 9.5, minimal is 11 allowing 11 to 13 clusters, everything below is upgraded
		if IsBiggerPostgresVersion(c.Spec.PgVersion, c.Config.OpConfig.MinimalMajorVersion) {
			c.logger.Infof("overwriting configured major version %s to %s", c.Spec.PgVersion, c.Config.OpConfig.TargetMajorVersion)
			return c.Config.OpConfig.TargetMajorVersion
		}
	}

	return c.Spec.PgVersion
}

func (c *Cluster) majorVersionUpgrade() error {

	if c.OpConfig.MajorVersionUpgradeMode == "off" {
		return nil
	}

	desiredVersion := c.GetDesiredMajorVersionAsInt()

	if c.currentMajorVersion >= desiredVersion {
		c.logger.Infof("cluster version up to date. current: %d desired: %d", c.currentMajorVersion, desiredVersion)
		return nil
	}

	pods, err := c.listPods()
	if err != nil {
		return err
	}

	allRunning := true

	var masterPod *v1.Pod

	for _, pod := range pods {
		ps, _ := c.patroni.GetMemberData(&pod)

		if ps.State != "running" {
			allRunning = false
			c.logger.Infof("identified non running pod, potentially skipping major version upgrade")
		}

		if ps.Role == "master" {
			masterPod = &pod
			c.currentMajorVersion = ps.ServerVersion
		}
	}

	numberOfPods := len(pods)
	if allRunning && masterPod != nil {
		c.logger.Infof("healthy cluster ready to upgrade, current: %d desired: %d", c.currentMajorVersion, desiredVersion)
		if c.currentMajorVersion < desiredVersion {
			podName := &spec.NamespacedName{Namespace: masterPod.Namespace, Name: masterPod.Name}
			c.logger.Infof("triggering major version upgrade on pod %s of %d pods", masterPod.Name, numberOfPods)
			upgradeCommand := fmt.Sprintf("/usr/bin/python3 /scripts/inplace_upgrade.py %d 2>&1 | tee last_upgrade.log", numberOfPods)
			_, err := c.ExecCommand(podName, "/bin/su", "postgres", "-c", upgradeCommand)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
