package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/api/v1"

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

func (c *Controller) nodeUpdate(prev, cur interface{}) {
	nodePrev, ok := prev.(*v1.Node)
	if !ok {
		return
	}

	nodeCur, ok := cur.(*v1.Node)
	if !ok {
		return
	}

	if val, ok := nodeCur.Labels["master"]; ok && val == "true" {
		return
	}

	if nodePrev.Spec.Unschedulable != nodeCur.Spec.Unschedulable && nodeCur.Spec.Unschedulable == true {
		c.logger.Infof("node %q became unschedulable", util.NameFromMeta(nodeCur.ObjectMeta))

		clusters, err := c.getClustersToMigrate(nodeCur.Name)
		if err != nil {
			c.logger.Errorf("could not get list of postgresql clusters to migrate: %v", err)
		}

		for _, cl := range clusters {
			c.clustersMu.RLock()
			cl := c.clusters[cl]
			c.clustersMu.RUnlock()

			c.queueClusterEvent(nil, &cl.Postgresql, spec.EventMigration)
		}
	}
}

func (c *Controller) nodeDelete(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		return
	}

	c.logger.Debugf("node has been deleted: %q (%s)", util.NameFromMeta(node.ObjectMeta), node.Spec.ProviderID)
}
