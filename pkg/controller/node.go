package controller

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"fmt"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Controller) nodeListFunc(options metav1.ListOptions) (runtime.Object, error) {
	opts := metav1.ListOptions{
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Nodes().List(opts)
}

func (c *Controller) nodeWatchFunc(options metav1.ListOptions) (watch.Interface, error) {
	opts := metav1.ListOptions{
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Nodes().Watch(opts)
}

func (c *Controller) nodeAdd(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	c.logger.Debugf("new node has been added: %q (%s)", util.NameFromMeta(node.ObjectMeta), node.Spec.ProviderID)
	// check if the node became not ready while the operator was down (otherwise we would have caught it in nodeUpdate)
	if !c.nodeIsReady(node) {
		c.moveMasterPodsOffNode(node)
	}
}

func (c *Controller) nodeUpdate(prev, cur interface{}) {
	nodePrev, ok := prev.(*v1.Node)
	if !ok {
		return
	}

	nodeCur, ok := cur.(*v1.Node)
	if !ok {
		return
	}

	if !c.nodeIsReady(nodePrev) {
		c.logger.Debugf("The decommissioned node %v should have already triggered master pod migration. Previous k8s-reported state of the node: %v", util.NameFromMeta(nodePrev.ObjectMeta), nodePrev)
		return
	}

	if c.nodeIsReady(nodeCur) {
		c.logger.Debugf("The decommissioned node %v become schedulable again. Current k8s-reported state of the node: %v", util.NameFromMeta(nodeCur.ObjectMeta), nodeCur)
		return
	}

	c.moveMasterPodsOffNode(nodeCur)
}

func (c *Controller) nodeIsReady(node *v1.Node) bool {
	return (!node.Spec.Unschedulable || util.MapContains(node.Labels, c.opConfig.NodeReadinessLabel) ||
		util.MapContains(node.Labels, map[string]string{"master": "true"}))
}

func (c *Controller) moveMasterPodsOffNode(node *v1.Node) {

	nodeName := util.NameFromMeta(node.ObjectMeta)
	c.logger.Infof("moving pods: node %q became unschedulable and does not have a ready label %q",
		nodeName, c.opConfig.NodeReadinessLabel)

	opts := metav1.ListOptions{
		LabelSelector: labels.Set(c.opConfig.ClusterLabels).String(),
	}
	podList, err := c.KubeClient.Pods(c.opConfig.WatchedNamespace).List(opts)
	if err != nil {
		c.logger.Errorf("could not fetch the list of Spilo pods: %v", err)
		return
	}

	nodePods := make([]*v1.Pod, 0)
	for i, pod := range podList.Items {
		if pod.Spec.NodeName == node.Name {
			nodePods = append(nodePods, &podList.Items[i])
		}
	}

	movedMasterPods := 0
	movableMasterPods := make(map[*v1.Pod]*cluster.Cluster)
	unmovablePods := make(map[spec.NamespacedName]string)

	clusters := make(map[*cluster.Cluster]bool)

	for _, pod := range nodePods {

		podName := util.NameFromMeta(pod.ObjectMeta)

		role, ok := pod.Labels[c.opConfig.PodRoleLabel]
		if !ok {
			// pods with an unknown role cannot be safely moved to another node
			unmovablePods[podName] = fmt.Sprintf("could not move pod %q from node %q: pod has no role label %q", podName, nodeName, c.opConfig.PodRoleLabel)
			continue
		}

		// deployments can transparently re-create replicas so we do not move away such pods
		if cluster.PostgresRole(role) == cluster.Replica {
			continue
		}

		clusterName := c.podClusterName(pod)

		c.clustersMu.RLock()
		cl, ok := c.clusters[clusterName]
		c.clustersMu.RUnlock()
		if !ok {
			unmovablePods[podName] = fmt.Sprintf("could not move master pod %q from node %q: pod belongs to an unknown Postgres cluster %q", podName, nodeName, clusterName)
			continue
		}

		if !clusters[cl] {
			clusters[cl] = true
		}

		movableMasterPods[pod] = cl
	}

	for cl := range clusters {
		cl.Lock()
	}

	for pod, cl := range movableMasterPods {

		podName := util.NameFromMeta(pod.ObjectMeta)
		if err := cl.MigrateMasterPod(podName); err == nil {
			movedMasterPods++
		} else {
			unmovablePods[podName] = fmt.Sprintf("could not move master pod %q from node %q: %v", podName, nodeName, err)
		}
	}

	for cl := range clusters {
		cl.Unlock()
	}

	if leftPods := len(unmovablePods); leftPods > 0 {
		c.logger.Warnf("could not move %d master or unknown role pods from the node %q, you may have to delete them manually",
			leftPods, nodeName)
		for _, reason := range unmovablePods {
			c.logger.Warning(reason)
		}
	}

	c.logger.Infof("%d master pods have been moved out from the node %q", movedMasterPods, nodeName)

}

func (c *Controller) nodeDelete(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	c.logger.Debugf("node has been deleted: %q (%s)", util.NameFromMeta(node.ObjectMeta), node.Spec.ProviderID)
}
