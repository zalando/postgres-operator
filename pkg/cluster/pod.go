package cluster

import (
	"fmt"
	"math/rand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Cluster) listPods() ([]v1.Pod, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pods, err := c.KubeClient.Pods(c.Namespace).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
	}

	return pods.Items, nil
}

func (c *Cluster) getRolePods(role PostgresRole) ([]v1.Pod, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: c.roleLabelsSet(role).String(),
	}

	pods, err := c.KubeClient.Pods(c.Namespace).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no pods")
	}

	if role == Master && len(pods.Items) > 1 {
		return nil, fmt.Errorf("too many masters")
	}

	return pods.Items, nil
}

func (c *Cluster) deletePods() error {
	c.logger.Debugln("deleting pods")
	pods, err := c.listPods()
	if err != nil {
		return err
	}

	for _, obj := range pods {
		podName := util.NameFromMeta(obj.ObjectMeta)

		c.logger.Debugf("deleting pod %q", podName)
		if err := c.deletePod(podName); err != nil {
			c.logger.Errorf("could not delete pod %q: %v", podName, err)
		} else {
			c.logger.Infof("pod %q has been deleted", podName)
		}
	}
	if len(pods) > 0 {
		c.logger.Debugln("pods have been deleted")
	} else {
		c.logger.Debugln("no pods to delete")
	}

	return nil
}

func (c *Cluster) deletePod(podName spec.NamespacedName) error {
	c.setProcessName("deleting pod %q", podName)
	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)

	if err := c.KubeClient.Pods(podName.Namespace).Delete(podName.Name, c.deleteOptions); err != nil {
		return err
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) unregisterPodSubscriber(podName spec.NamespacedName) {
	c.logger.Debugf("unsubscribing from pod %q events", podName)
	c.podSubscribersMu.Lock()
	defer c.podSubscribersMu.Unlock()

	if _, ok := c.podSubscribers[podName]; !ok {
		panic("subscriber for pod '" + podName.String() + "' is not found")
	}

	close(c.podSubscribers[podName])
	delete(c.podSubscribers, podName)
}

func (c *Cluster) registerPodSubscriber(podName spec.NamespacedName) chan spec.PodEvent {
	c.logger.Debugf("subscribing to pod %q", podName)
	c.podSubscribersMu.Lock()
	defer c.podSubscribersMu.Unlock()

	ch := make(chan spec.PodEvent)
	if _, ok := c.podSubscribers[podName]; ok {
		panic("pod '" + podName.String() + "' is already subscribed")
	}
	c.podSubscribers[podName] = ch

	return ch
}

func (c *Cluster) movePodFromEndOfLifeNode(pod *v1.Pod) (*v1.Pod, error) {
	podName := util.NameFromMeta(pod.ObjectMeta)

	if eol, err := c.podIsEndOfLife(pod); err != nil {
		return nil, fmt.Errorf("could not get node %q: %v", pod.Spec.NodeName, err)
	} else if !eol {
		c.logger.Infof("pod %q is already on a live node", podName)
		return pod, nil
	}

	c.setProcessName("moving pod %q out of end-of-life node %q", podName, pod.Spec.NodeName)
	c.logger.Infof("moving pod %q out of the end-of-life node %q", podName, pod.Spec.NodeName)

	if err := c.recreatePod(podName); err != nil {
		return nil, fmt.Errorf("could not move pod: %v", err)
	}

	newPod, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get pod: %v", err)
	}

	if newPod.Spec.NodeName == pod.Spec.NodeName {
		return nil, fmt.Errorf("pod %q remained on the same node", podName)
	}

	if eol, err := c.podIsEndOfLife(newPod); err != nil {
		return nil, fmt.Errorf("could not get node %q: %v", pod.Spec.NodeName, err)
	} else if eol {
		c.logger.Warningf("pod %q moved to end-of-life node %q", podName, newPod.Spec.NodeName)
		return newPod, nil
	}

	c.logger.Infof("pod %q moved from node %q to node %q", podName, pod.Spec.NodeName, newPod.Spec.NodeName)

	return newPod, nil
}

func (c *Cluster) masterCandidate(oldNodeName string) (*v1.Pod, error) {
	replicas, err := c.getRolePods(Replica)
	if err != nil {
		return nil, fmt.Errorf("could not get replica pods: %v", err)
	}

	for i, pod := range replicas {
		// look for replicas running on live nodes. Ignore errors when querying the nodes.
		if pod.Spec.NodeName != oldNodeName {
			eol, err := c.podIsEndOfLife(&pod)
			if err == nil && !eol {
				return &replicas[i], nil
			}
		}
	}
	c.logger.Debug("no available master candidates on live nodes")
	return &replicas[rand.Intn(len(replicas))], nil
}

// MigrateMasterPod migrates master pod via failover to a replica
func (c *Cluster) MigrateMasterPod(podName spec.NamespacedName) error {
	oldMaster, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name, metav1.GetOptions{})

	if err != nil {
		return fmt.Errorf("could not get pod: %v", err)
	}

	c.logger.Infof("migrating master pod %q", podName)

	if eol, err := c.podIsEndOfLife(oldMaster); err != nil {
		return fmt.Errorf("could not get node %q: %v", oldMaster.Spec.NodeName, err)
	} else if !eol {
		c.logger.Debugf("pod is already on a live node")
		return nil
	}

	if role := PostgresRole(oldMaster.Labels[c.OpConfig.PodRoleLabel]); role != Master {
		c.logger.Warningf("pod %q is not a master", podName)
		return nil
	}

	masterCandidatePod, err := c.masterCandidate(oldMaster.Spec.NodeName)
	if err != nil {
		return fmt.Errorf("could not get new master candidate: %v", err)
	}

	pod, err := c.movePodFromEndOfLifeNode(masterCandidatePod)
	if err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	masterCandidateName := util.NameFromMeta(pod.ObjectMeta)
	if err := c.ManualFailover(oldMaster, masterCandidateName); err != nil {
		return fmt.Errorf("could not failover to pod %q: %v", masterCandidateName, err)
	}

	_, err = c.movePodFromEndOfLifeNode(oldMaster)
	if err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	return nil
}

// MigrateReplicaPod recreates pod on a new node
func (c *Cluster) MigrateReplicaPod(podName spec.NamespacedName, fromNodeName string) error {
	replicaPod, err := c.KubeClient.Pods(podName.Namespace).Get(podName.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get pod: %v", err)
	}

	c.logger.Infof("migrating replica pod %q", podName)

	if replicaPod.Spec.NodeName != fromNodeName {
		c.logger.Infof("pod %q has already migrated to node %q", podName, replicaPod.Spec.NodeName)
		return nil
	}

	if role := PostgresRole(replicaPod.Labels[c.OpConfig.PodRoleLabel]); role != Replica {
		return fmt.Errorf("pod %q is not a replica", podName)
	}

	_, err = c.movePodFromEndOfLifeNode(replicaPod)
	if err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	return nil
}

func (c *Cluster) recreatePod(podName spec.NamespacedName) error {
	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)

	if err := c.KubeClient.Pods(podName.Namespace).Delete(podName.Name, c.deleteOptions); err != nil {
		return fmt.Errorf("could not delete pod: %v", err)
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}
	if err := c.waitForPodLabel(ch, nil); err != nil {
		return err
	}
	c.logger.Infof("pod %q has been recreated", podName)

	return nil
}

func (c *Cluster) recreatePods() error {
	c.setProcessName("recreating pods")
	ls := c.labelsSet()
	namespace := c.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: ls.String(),
	}

	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get the list of pods: %v", err)
	}
	c.logger.Infof("there are %d pods in the cluster to recreate", len(pods.Items))

	var masterPod *v1.Pod
	replicas := make([]spec.NamespacedName, 0)
	for i, pod := range pods.Items {
		role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])

		if role == Master {
			masterPod = &pods.Items[i]
			continue
		}

		podName := util.NameFromMeta(pods.Items[i].ObjectMeta)
		if err := c.recreatePod(podName); err != nil {
			return fmt.Errorf("could not recreate replica pod %q: %v", util.NameFromMeta(pod.ObjectMeta), err)
		}

		replicas = append(replicas, util.NameFromMeta(pod.ObjectMeta))
	}

	if masterPod == nil {
		c.logger.Warningln("no master pod in the cluster")
	} else {
		if len(replicas) > 0 {
			err := c.ManualFailover(masterPod, masterCandidate(replicas))
			if err != nil {
				return fmt.Errorf("could not perform manual failover: %v", err)
			}
		}
		//TODO: specify master, leave new master empty
		c.logger.Infof("recreating master pod %q", util.NameFromMeta(masterPod.ObjectMeta))

		if err := c.recreatePod(util.NameFromMeta(masterPod.ObjectMeta)); err != nil {
			return fmt.Errorf("could not recreate master pod %q: %v", util.NameFromMeta(masterPod.ObjectMeta), err)
		}
	}

	return nil
}

func (c *Cluster) podIsEndOfLife(pod *v1.Pod) (bool, error) {
	node, err := c.KubeClient.Nodes().Get(pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable || util.MapContains(node.Labels, c.OpConfig.NodeEOLLabel), nil

}
