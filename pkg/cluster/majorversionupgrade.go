package cluster

import (
	"fmt"

	"github.com/zalando/postgres-operator/pkg/spec"
	v1 "k8s.io/api/core/v1"
)

// VersionMap Map of version numbers
var VersionMap = map[string]int{
	"9.5": 95000,
	"9.6": 96000,
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
		if IsBiggerPostgresVersion(c.Spec.PgVersion, c.Config.OpConfig.TargetMajorVersion) {
			c.logger.Infof("Overwriting configured major version %s to %s", c.Spec.PgVersion, c.Config.OpConfig.TargetMajorVersion)
			return c.Config.OpConfig.TargetMajorVersion
		}
	}

	return c.Spec.PgVersion
}

func (c *Cluster) majorVersionUpgrade() error {

	if c.OpConfig.MajorVersionUpgradeMode == "off" {
		return nil
	}

	pods, _ := c.listPods()
	allRunning := true

	var masterPod *v1.Pod

	for _, pod := range pods {
		ps, _ := c.patroni.GetMemberData(&pod)

		if ps.State != "running" {
			allRunning = false
		}

		if ps.Role == "master" {
			masterPod = &pod
			c.currentMajorVersion = ps.ServerVersion
		}
	}

	numberOfPods := len(pods)
	if allRunning && masterPod != nil {
		desiredVersion := c.GetDesiredMajorVersionAsInt()
		c.logger.Infof("cluster healthy with version: %d desired: %d", c.currentMajorVersion, desiredVersion)
		if c.currentMajorVersion < desiredVersion {
			podName := &spec.NamespacedName{Namespace: masterPod.Namespace, Name: masterPod.Name}
			c.logger.Infof("triggering major version upgrade on pod %s", masterPod.Name)
			_, err := c.ExecCommand(podName, fmt.Sprintf("su postgres -c \"python3 /scripts/inplace_upgrade.py %d 2>&1 | tee last_upgrade.log\"", numberOfPods))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Cluster) getCurrentMajorVersion() error {
	return nil
}
