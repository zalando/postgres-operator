package cluster

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

// replicaKillCandidate chooses a pod to kill and later promote it to the master
func replicaKillCandidate(pods []*v1.Pod) *v1.Pod {
	return pods[rand.Intn(len(pods))]
}

// Migrate performs migration of the master pod to the new kubernetes node
func (c *Cluster) Migrate() error {
	allPods, err := c.listPods()
	if err != nil {
		return fmt.Errorf("could not get list of cluster pods: %v", err)
	}

	if len(allPods) == 1 {
		var pods []v1.Pod

		pods, err = c.getRolePods(Master)
		if err != nil {
			return fmt.Errorf("could not get pod to move")
		}

		if err = c.movePod(&pods[0]); err != nil {
			return fmt.Errorf("could not move pod: %v", err)
		}

		if _, err = c.getRolePods(Master); err != nil {
			return fmt.Errorf("could not get Master pod: %v", err)
		}

		return nil
	}

	replicaPods := make([]*v1.Pod, 0)
	var curMasterPod *v1.Pod
	for i, pod := range allPods {
		role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])

		switch role {
		case Master:
			curMasterPod = &allPods[i]
		case Replica:
			replicaPods = append(replicaPods, &allPods[i])
		default:
			c.logger.Warningf("pod %q has no role", util.NameFromMeta(pod.ObjectMeta))
		}
	}

	if curMasterPod == nil {
		return fmt.Errorf("could not find Master pod in the cluster")
	}

	futureMaster := replicaKillCandidate(replicaPods)
	if err = c.movePod(futureMaster); err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	err = c.ManualFailover(curMasterPod, util.NameFromMeta(futureMaster.ObjectMeta))
	if err != nil {
		return fmt.Errorf("could not failover: %v", err)
	}

	masterPods, err := c.getRolePods(Master)
	if err != nil {
		return fmt.Errorf("could not get Master pod: %v", err)
	}
	masterPodName := util.NameFromMeta(masterPods[0].ObjectMeta)

	if masterPodName != util.NameFromMeta(futureMaster.ObjectMeta) {
		return fmt.Errorf("Master supposed to be on the new node")
	}

	for _, pod := range allPods {
		if masterPodName == util.NameFromMeta(pod.ObjectMeta) {
			c.logger.Debugf("skipping Master pod: %q", util.NameFromMeta(pod.ObjectMeta))
			continue
		}

		c.logger.Debugf("moving %q pod", util.NameFromMeta(pod.ObjectMeta))

		if err := c.movePod(&pod); err != nil {
			c.logger.Warningf("could not move pod %q: %v", util.NameFromMeta(pod.ObjectMeta), err)
			continue
		}

		c.logger.Infof("pod %q has been moved", util.NameFromMeta(pod.ObjectMeta))
	}

	return nil
}
