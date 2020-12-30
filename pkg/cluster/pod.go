package cluster

import (
	"context"
	"fmt"
	"math/rand"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
)

func (c *Cluster) listPods() ([]v1.Pod, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet(false).String(),
	}

	pods, err := c.KubeClient.Pods(c.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
	}

	return pods.Items, nil
}

func (c *Cluster) getRolePods(role PostgresRole) ([]v1.Pod, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: c.roleLabelsSet(false, role).String(),
	}

	pods, err := c.KubeClient.Pods(c.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
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

	if err := c.KubeClient.Pods(podName.Namespace).Delete(context.TODO(), podName.Name, c.deleteOptions); err != nil {
		return err
	}

	return c.waitForPodDeletion(ch)
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

func (c *Cluster) registerPodSubscriber(podName spec.NamespacedName) chan PodEvent {
	c.logger.Debugf("subscribing to pod %q", podName)
	c.podSubscribersMu.Lock()
	defer c.podSubscribersMu.Unlock()

	ch := make(chan PodEvent)
	if _, ok := c.podSubscribers[podName]; ok {
		panic("pod '" + podName.String() + "' is already subscribed")
	}
	c.podSubscribers[podName] = ch

	return ch
}

func (c *Cluster) movePodFromEndOfLifeNode(pod *v1.Pod) (*v1.Pod, error) {
	var (
		eol    bool
		err    error
		newPod *v1.Pod
	)
	podName := util.NameFromMeta(pod.ObjectMeta)

	if eol, err = c.podIsEndOfLife(pod); err != nil {
		return nil, fmt.Errorf("could not get node %q: %v", pod.Spec.NodeName, err)
	} else if !eol {
		c.logger.Infof("check failed: pod %q is already on a live node", podName)
		return pod, nil
	}

	c.setProcessName("moving pod %q out of end-of-life node %q", podName, pod.Spec.NodeName)
	c.logger.Infof("moving pod %q out of the end-of-life node %q", podName, pod.Spec.NodeName)

	if newPod, err = c.recreatePod(podName); err != nil {
		return nil, fmt.Errorf("could not move pod: %v", err)
	}

	if newPod.Spec.NodeName == pod.Spec.NodeName {
		return nil, fmt.Errorf("pod %q remained on the same node", podName)
	}

	if eol, err = c.podIsEndOfLife(newPod); err != nil {
		return nil, fmt.Errorf("could not get node %q: %v", pod.Spec.NodeName, err)
	} else if eol {
		c.logger.Warningf("pod %q moved to end-of-life node %q", podName, newPod.Spec.NodeName)
		return newPod, nil
	}

	c.logger.Infof("pod %q moved from node %q to node %q", podName, pod.Spec.NodeName, newPod.Spec.NodeName)

	return newPod, nil
}

func (c *Cluster) masterCandidate(oldNodeName string) (*v1.Pod, error) {

	// Wait until at least one replica pod will come up
	if err := c.waitForAnyReplicaLabelReady(); err != nil {
		c.logger.Warningf("could not find at least one ready replica: %v", err)
	}

	replicas, err := c.getRolePods(Replica)
	if err != nil {
		return nil, fmt.Errorf("could not get replica pods: %v", err)
	}

	if len(replicas) == 0 {
		c.logger.Warningf("no available master candidates, migration will cause longer downtime of Postgres cluster")
		return nil, nil
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
	c.logger.Warningf("no available master candidates on live nodes")
	return &replicas[rand.Intn(len(replicas))], nil
}

// MigrateMasterPod migrates master pod via failover to a replica
func (c *Cluster) MigrateMasterPod(podName spec.NamespacedName) error {
	var (
		masterCandidatePod *v1.Pod
		err                error
		eol                bool
	)

	oldMaster, err := c.KubeClient.Pods(podName.Namespace).Get(context.TODO(), podName.Name, metav1.GetOptions{})

	if err != nil {
		return fmt.Errorf("could not get pod: %v", err)
	}

	c.logger.Infof("starting process to migrate master pod %q", podName)

	if eol, err = c.podIsEndOfLife(oldMaster); err != nil {
		return fmt.Errorf("could not get node %q: %v", oldMaster.Spec.NodeName, err)
	}
	if !eol {
		c.logger.Debugf("no action needed: master pod is already on a live node")
		return nil
	}

	if role := PostgresRole(oldMaster.Labels[c.OpConfig.PodRoleLabel]); role != Master {
		c.logger.Warningf("no action needed: pod %q is not the master (anymore)", podName)
		return nil
	}
	// we must have a statefulset in the cluster for the migration to work
	if c.Statefulset == nil {
		var sset *appsv1.StatefulSet
		if sset, err = c.KubeClient.StatefulSets(c.Namespace).Get(
			context.TODO(),
			c.statefulSetName(),
			metav1.GetOptions{}); err != nil {
			return fmt.Errorf("could not retrieve cluster statefulset: %v", err)
		}
		c.Statefulset = sset
	}
	// We may not have a cached statefulset if the initial cluster sync has aborted, revert to the spec in that case.
	if *c.Statefulset.Spec.Replicas > 1 {
		if masterCandidatePod, err = c.masterCandidate(oldMaster.Spec.NodeName); err != nil {
			return fmt.Errorf("could not find suitable replica pod as candidate for failover: %v", err)
		}
	} else {
		c.logger.Warningf("migrating single pod cluster %q, this will cause downtime of the Postgres cluster until pod is back", c.clusterName())
	}

	// there are two cases for each postgres cluster that has its master pod on the node to migrate from:
	// - the cluster has some replicas - migrate one of those if necessary and failover to it
	// - there are no replicas - just terminate the master and wait until it respawns
	// in both cases the result is the new master up and running on a new node.

	if masterCandidatePod == nil {
		if _, err = c.movePodFromEndOfLifeNode(oldMaster); err != nil {
			return fmt.Errorf("could not move pod: %v", err)
		}
		return nil
	}

	if masterCandidatePod, err = c.movePodFromEndOfLifeNode(masterCandidatePod); err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	masterCandidateName := util.NameFromMeta(masterCandidatePod.ObjectMeta)
	if err := c.Switchover(oldMaster, masterCandidateName); err != nil {
		return fmt.Errorf("could not failover to pod %q: %v", masterCandidateName, err)
	}

	return nil
}

// MigrateReplicaPod recreates pod on a new node
func (c *Cluster) MigrateReplicaPod(podName spec.NamespacedName, fromNodeName string) error {
	replicaPod, err := c.KubeClient.Pods(podName.Namespace).Get(context.TODO(), podName.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get pod: %v", err)
	}

	c.logger.Infof("migrating replica pod %q to live node", podName)

	if replicaPod.Spec.NodeName != fromNodeName {
		c.logger.Infof("check failed: pod %q has already migrated to node %q", podName, replicaPod.Spec.NodeName)
		return nil
	}

	if role := PostgresRole(replicaPod.Labels[c.OpConfig.PodRoleLabel]); role != Replica {
		return fmt.Errorf("check failed: pod %q is not a replica", podName)
	}

	_, err = c.movePodFromEndOfLifeNode(replicaPod)
	if err != nil {
		return fmt.Errorf("could not move pod: %v", err)
	}

	return nil
}

func (c *Cluster) recreatePod(podName spec.NamespacedName) (*v1.Pod, error) {
	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)
	stopChan := make(chan struct{})

	if err := c.KubeClient.Pods(podName.Namespace).Delete(context.TODO(), podName.Name, c.deleteOptions); err != nil {
		return nil, fmt.Errorf("could not delete pod: %v", err)
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return nil, err
	}
	pod, err := c.waitForPodLabel(ch, stopChan, nil)
	if err != nil {
		return nil, err
	}
	c.logger.Infof("pod %q has been recreated", podName)
	return pod, nil
}

func (c *Cluster) isSafeToRecreatePods(pods *v1.PodList) bool {

	/*
	 Operator should not re-create pods if there is at least one replica being bootstrapped
	 because Patroni might use other replicas to take basebackup from (see Patroni's "clonefrom" tag).

	 XXX operator cannot forbid replica re-init, so we might still fail if re-init is started
	 after this check succeeds but before a pod is re-created
	*/

	for _, pod := range pods.Items {
		c.logger.Debugf("name=%s phase=%s ip=%s", pod.Name, pod.Status.Phase, pod.Status.PodIP)
	}

	for _, pod := range pods.Items {
		state, err := c.patroni.GetPatroniMemberState(&pod)
		if err != nil {
			c.logger.Errorf("failed to get Patroni state for pod: %s", err)
			return false
		} else if state == "creating replica" {
			c.logger.Warningf("cannot re-create replica %s: it is currently being initialized", pod.Name)
			return false
		}

	}
	return true
}

func (c *Cluster) recreatePods() error {
	c.setProcessName("starting to recreate pods")
	ls := c.labelsSet(false)
	namespace := c.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: ls.String(),
	}

	pods, err := c.KubeClient.Pods(namespace).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("could not get the list of pods: %v", err)
	}
	c.logger.Infof("there are %d pods in the cluster to recreate", len(pods.Items))

	if !c.isSafeToRecreatePods(pods) {
		return fmt.Errorf("postpone pod recreation until next Sync: recreation is unsafe because pods are being initialized")
	}

	var (
		masterPod, newMasterPod, newPod *v1.Pod
	)
	replicas := make([]spec.NamespacedName, 0)
	for i, pod := range pods.Items {
		role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])

		if role == Master {
			masterPod = &pods.Items[i]
			continue
		}

		podName := util.NameFromMeta(pods.Items[i].ObjectMeta)
		if newPod, err = c.recreatePod(podName); err != nil {
			return fmt.Errorf("could not recreate replica pod %q: %v", util.NameFromMeta(pod.ObjectMeta), err)
		}
		if newRole := PostgresRole(newPod.Labels[c.OpConfig.PodRoleLabel]); newRole == Replica {
			replicas = append(replicas, util.NameFromMeta(pod.ObjectMeta))
		} else if newRole == Master {
			newMasterPod = newPod
		}
	}

	if masterPod != nil {
		// failover if we have not observed a master pod when re-creating former replicas.
		if newMasterPod == nil && len(replicas) > 0 {
			if err := c.Switchover(masterPod, masterCandidate(replicas)); err != nil {
				c.logger.Warningf("could not perform switch over: %v", err)
			}
		} else if newMasterPod == nil && len(replicas) == 0 {
			c.logger.Warningf("cannot perform switch over before re-creating the pod: no replicas")
		}
		c.logger.Infof("recreating old master pod %q", util.NameFromMeta(masterPod.ObjectMeta))

		if _, err := c.recreatePod(util.NameFromMeta(masterPod.ObjectMeta)); err != nil {
			return fmt.Errorf("could not recreate old master pod %q: %v", util.NameFromMeta(masterPod.ObjectMeta), err)
		}
	}

	return nil
}

func (c *Cluster) podIsEndOfLife(pod *v1.Pod) (bool, error) {
	node, err := c.KubeClient.Nodes().Get(context.TODO(), pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return node.Spec.Unschedulable || !util.MapContains(node.Labels, c.OpConfig.NodeReadinessLabel), nil

}
