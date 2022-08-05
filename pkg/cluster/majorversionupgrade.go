package cluster

import (
	"fmt"
	"strings"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
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
	"14":  140000,
}

// IsBiggerPostgresVersion Compare two Postgres version numbers
func IsBiggerPostgresVersion(old string, new string) bool {
	oldN := VersionMap[old]
	newN := VersionMap[new]
	return newN > oldN
}

// GetDesiredMajorVersionAsInt Convert string to comparable integer of PG version
func (c *Cluster) GetDesiredMajorVersionAsInt() int {
	return VersionMap[c.GetDesiredMajorVersion()]
}

// GetDesiredMajorVersion returns major version to use, incl. potential auto upgrade
func (c *Cluster) GetDesiredMajorVersion() string {

	if c.Config.OpConfig.MajorVersionUpgradeMode == "full" {
		// e.g. current is 9.6, minimal is 11 allowing 11 to 14 clusters, everything below is upgraded
		if IsBiggerPostgresVersion(c.Spec.PgVersion, c.Config.OpConfig.MinimalMajorVersion) {
			c.logger.Infof("overwriting configured major version %s to %s", c.Spec.PgVersion, c.Config.OpConfig.TargetMajorVersion)
			return c.Config.OpConfig.TargetMajorVersion
		}
	}

	return c.Spec.PgVersion
}

func (c *Cluster) isUpgradeAllowedForTeam(owningTeam string) bool {
	allowedTeams := c.OpConfig.MajorVersionUpgradeTeamAllowList

	if len(allowedTeams) == 0 {
		return false
	}

	return util.SliceContains(allowedTeams, owningTeam)
}

/*
  Execute upgrade when mode is set to manual or full or when the owning team is allowed for upgrade (and mode is "off").

  Manual upgrade means, it is triggered by the user via manifest version change
  Full upgrade means, operator also determines the minimal version used accross all clusters and upgrades violators.
*/
func (c *Cluster) majorVersionUpgrade() error {

	if c.OpConfig.MajorVersionUpgradeMode == "off" && !c.isUpgradeAllowedForTeam(c.Spec.TeamID) {
		return nil
	}

	desiredVersion := c.GetDesiredMajorVersionAsInt()

	if c.currentMajorVersion >= desiredVersion {
		c.logger.Infof("cluster version up to date. current: %d, min desired: %d", c.currentMajorVersion, desiredVersion)
		return nil
	}

	pods, err := c.listPods()
	if err != nil {
		return err
	}

	allRunning := true

	var masterPod *v1.Pod

	for i, pod := range pods {
		ps, _ := c.patroni.GetMemberData(&pod)

		if ps.State != "running" {
			allRunning = false
			c.logger.Infof("identified non running pod, potentially skipping major version upgrade")
		}

		if ps.Role == "master" {
			masterPod = &pods[i]
			c.currentMajorVersion = ps.ServerVersion
		}
	}

	numberOfPods := len(pods)
	if allRunning && masterPod != nil {
		c.logger.Infof("healthy cluster ready to upgrade, current: %d desired: %d", c.currentMajorVersion, desiredVersion)
		if c.currentMajorVersion < desiredVersion {
			podName := &spec.NamespacedName{Namespace: masterPod.Namespace, Name: masterPod.Name}
			c.logger.Infof("triggering major version upgrade on pod %s of %d pods", masterPod.Name, numberOfPods)
			c.eventRecorder.Eventf(c.GetReference(), v1.EventTypeNormal, "Major Version Upgrade", "Starting major version upgrade on pod %s of %d pods", masterPod.Name, numberOfPods)
			upgradeCommand := fmt.Sprintf("/usr/bin/python3 /scripts/inplace_upgrade.py %d 2>&1 | tee last_upgrade.log", numberOfPods)
			
			c.logger.Debugf("checking if the spilo image runs with root or non-root (check for user id=0)")
			resultIdCheck, errIdCheck := c.ExecCommand(podName, "/bin/bash", "-c", "/usr/bin/id -u")
			if errIdCheck != nil {
				c.eventRecorder.Eventf(c.GetReference(), v1.EventTypeWarning, "Major Version Upgrade", "Checking user id to run upgrade from %d to %d FAILED: %v", c.currentMajorVersion, desiredVersion, errIdCheck)
			}

			resultIdCheck = strings.TrimSuffix(resultIdCheck, "\n")
			var result string
			if resultIdCheck != "0" {
				c.logger.Infof("User id was identified as: %s, hence default user is non-root already", resultIdCheck)
				result, err = c.ExecCommand(podName, "/bin/bash", "-c", upgradeCommand)
			} else {
				c.logger.Infof("User id was identified as: %s, using su to reach the postgres user", resultIdCheck)
				result, err = c.ExecCommand(podName, "/bin/su", "postgres", "-c", upgradeCommand)
			}
			if err != nil {
				c.eventRecorder.Eventf(c.GetReference(), v1.EventTypeWarning, "Major Version Upgrade", "Upgrade from %d to %d FAILED: %v", c.currentMajorVersion, desiredVersion, err)
				return err
			}
			c.logger.Infof("upgrade action triggered and command completed: %s", result[:100])

			c.eventRecorder.Eventf(c.GetReference(), v1.EventTypeNormal, "Major Version Upgrade", "Upgrade from %d to %d finished", c.currentMajorVersion, desiredVersion)
		}
	}

	return nil
}
