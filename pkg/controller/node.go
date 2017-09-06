package controller

import (
	"fmt"
	"math/rand"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/api/v1"

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
}

func masterCandidate(replicas []*v1.Pod) *v1.Pod {
	return replicas[rand.Intn(len(replicas))]
}

func (c *Controller) migratePod(cl *cluster.Cluster, pod *v1.Pod) error {
	newNode, err := cl.MovePod(pod)
	if err != nil {
		return fmt.Errorf("could not migrate pod: %v", err)
	}
	if util.MapContains(newNode.Labels, c.opConfig.CordonedNodeLabel) {
		return fmt.Errorf("migrated to another cordoned node")
	}

	return nil
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

	if util.MapContains(nodeCur.Labels, map[string]string{"master": "true"}) {
		return
	}

	if !(nodePrev.Spec.Unschedulable != nodeCur.Spec.Unschedulable &&
		nodeCur.Spec.Unschedulable && util.MapContains(nodeCur.Labels, c.opConfig.CordonedNodeLabel)) {
		return
	}

	c.logger.Infof("node %q became unschedulable and has EOL labels: %q", util.NameFromMeta(nodeCur.ObjectMeta),
		c.opConfig.CordonedNodeLabel)

	opts := metav1.ListOptions{
		LabelSelector: labels.Set(c.opConfig.ClusterLabels).String(),
	}
	podList, err := c.KubeClient.Pods(c.opConfig.Namespace).List(opts)
	if err != nil {
		c.logger.Errorf("could not fetch list of the pods: %v", err)
		return
	}

	clusterReplicas := make(map[spec.NamespacedName][]*v1.Pod, 0)
	nodePods := make([]*v1.Pod, 0)
	for i, pod := range podList.Items {
		if pod.Spec.NodeName == nodeCur.Name {
			nodePods = append(nodePods, &podList.Items[i])
		}

		clusterName := c.podClusterName(&pod)
		role := cluster.PostgresRole(pod.Labels[c.opConfig.PodRoleLabel])
		if role != cluster.Replica {
			continue
		}

		if _, ok := clusterReplicas[clusterName]; !ok {
			clusterReplicas[clusterName] = []*v1.Pod{&podList.Items[i]}
		} else {
			clusterReplicas[clusterName] = append(clusterReplicas[clusterName], &podList.Items[i])
		}
	}

	c.movePods(nodePods, clusterReplicas)
}

func (c *Controller) movePods(nodePods []*v1.Pod, clusterReplicas map[spec.NamespacedName][]*v1.Pod) error {
	c.logger.Debugf("%d pods to migrate", len(nodePods))

	for _, pod := range nodePods {
		podName := util.NameFromMeta(pod.ObjectMeta)
		clusterName := c.podClusterName(pod)
		if clusterName == (spec.NamespacedName{}) {
			c.logger.Errorf("pod %q does not belong to any cluster: %q", podName, clusterName)
			continue
		}

		c.logger.Debugf("moving %q pod", podName)

		c.clustersMu.RLock()
		cl, ok := c.clusters[clusterName]
		c.clustersMu.RUnlock()
		if !ok {
			c.logger.Errorf("orphaned pod: %q", podName)
			continue
		}

		switch cluster.PostgresRole(pod.Labels[c.opConfig.PodRoleLabel]) {
		case cluster.Master:
			if cl.Spec.NumberOfInstances == 1 { // single node cluster
				if err := c.migratePod(cl, pod); err != nil {
					c.logger.Errorf("could not migrate pod: %v", err)
					continue
				}

				c.logger.Infof("replica-less cluster %q has been migrated", clusterName)
			} else { // migrate replica first
				newMasterPod := masterCandidate(clusterReplicas[clusterName])
				if err := c.migratePod(cl, newMasterPod); err != nil {
					c.logger.Errorf("could not migrate pod: %v", err)
				}
				if err := cl.ManualFailover(pod, util.NameFromMeta(newMasterPod.ObjectMeta)); err != nil {
					c.logger.Errorf("could not failover from %q to %q",
						podName, util.NameFromMeta(newMasterPod.ObjectMeta))
				}

				c.logger.Info("master of the %q cluster has been migrated to a new node", clusterName)
			}
		case cluster.Replica:
			if err := c.migratePod(cl, pod); err != nil {
				c.logger.Errorf("could not migrate replica pod: %v", err)
			}

			c.logger.Infof("replica pod %q of the %q cluster has been migrated", podName, clusterName)
		default:
			c.logger.Errorf("a %q cluster pod with no role: %q", clusterName, podName)
		}
	}

	return nil
}

func (c *Controller) nodeDelete(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	c.logger.Debugf("node has been deleted: %q (%s)", util.NameFromMeta(node.ObjectMeta), node.Spec.ProviderID)
}
