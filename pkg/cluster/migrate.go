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
	var err error

	allPods, err := c.listPods()
	if err != nil {
		return fmt.Errorf("could not get list of cluster pods: %v", err)
	}

	if len(allPods) == 1 {
		pods, err := c.getRolePods(master)
		if err != nil {
			return fmt.Errorf("could not get pod to move")
		}

		if err = c.movePod(&pods[0]); err != nil {
			return fmt.Errorf("could not move pod: %v", err)
		}

		masterPods, err := c.getRolePods(master)
		if err != nil {
			return fmt.Errorf("could not get master pod: %v", err)
		}
		if len(masterPods) == 0 {
			return fmt.Errorf("no master running in the cluster")
		}

		return nil
	}

	replicaPods := make([]*v1.Pod, 0)
	var curMasterPod *v1.Pod
	for i, pod := range allPods {
		role := c.podPostgresRole(&pod)

		switch role {
		case master:
			curMasterPod = &allPods[i]
		case replica:
			replicaPods = append(replicaPods, &allPods[i])
		default:
			c.logger.Warnf("pod %q has no role", util.NameFromMeta(pod.ObjectMeta))
		}
	}

	if curMasterPod == nil {
		return fmt.Errorf("could not find master pod in the cluster")
	}

	futureMaster := replicaKillCandidate(replicaPods)
	if err = c.movePod(futureMaster); err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	err = c.ManualFailover(curMasterPod, util.NameFromMeta(futureMaster.ObjectMeta))
	if err != nil {
		return fmt.Errorf("could not failover: %v", err)
	}

	masterPods, err := c.getRolePods(master)
	if err != nil {
		return fmt.Errorf("could not get master pod: %v", err)
	}
	if len(masterPods) > 1 {
		return fmt.Errorf("too many masters")
	}
	masterPodName := util.NameFromMeta(masterPods[0].ObjectMeta)
	c.logger.Debugf("master pod is there: %q", masterPodName)

	if masterPodName != util.NameFromMeta(futureMaster.ObjectMeta) {
		return fmt.Errorf("master supposed to be on the new node")
	}

	for _, pod := range allPods {
		if masterPodName == util.NameFromMeta(pod.ObjectMeta) {
			c.logger.Debugf("skipping master pod: %q", util.NameFromMeta(pod.ObjectMeta))
			continue
		}

		c.logger.Debugf("moving %q pod", util.NameFromMeta(pod.ObjectMeta))

		if err := c.movePod(&pod); err != nil {
			c.logger.Warnf("could not move pod %q: %v", util.NameFromMeta(pod.ObjectMeta), err)
			continue
		}

		c.logger.Infof("pod %q has been moved", util.NameFromMeta(pod.ObjectMeta))
	}

	return nil
}
