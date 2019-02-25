package controller

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/zalando/postgres-operator/pkg/cluster"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Controller) podListFunc(options metav1.ListOptions) (runtime.Object, error) {
	opts := metav1.ListOptions{
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Pods(c.opConfig.WatchedNamespace).List(opts)
}

func (c *Controller) podWatchFunc(options metav1.ListOptions) (watch.Interface, error) {
	opts := metav1.ListOptions{
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Pods(c.opConfig.WatchedNamespace).Watch(opts)
}

func (c *Controller) dispatchPodEvent(clusterName spec.NamespacedName, event cluster.PodEvent) {
	c.clustersMu.RLock()
	cluster, ok := c.clusters[clusterName]
	c.clustersMu.RUnlock()
	if ok {
		cluster.ReceivePodEvent(event)
	}
}

func (c *Controller) podAdd(obj interface{}) {
	if pod, ok := obj.(*v1.Pod); ok {
		c.preparePodEventForDispatch(pod, nil, cluster.PodEventAdd)
	}
}

func (c *Controller) podUpdate(prev, cur interface{}) {
	prevPod, ok := prev.(*v1.Pod)
	if !ok {
		return
	}

	curPod, ok := cur.(*v1.Pod)
	if !ok {
		return
	}

	c.preparePodEventForDispatch(curPod, prevPod, cluster.PodEventUpdate)
}

func (c *Controller) podDelete(obj interface{}) {

	if pod, ok := obj.(*v1.Pod); ok {
		c.preparePodEventForDispatch(pod, nil, cluster.PodEventDelete)
	}
}

func (c *Controller) preparePodEventForDispatch(curPod, prevPod *v1.Pod, event cluster.PodEventType) {
	podEvent := cluster.PodEvent{
		PodName:         types.NamespacedName(util.NameFromMeta(curPod.ObjectMeta)),
		CurPod:          curPod,
		PrevPod:         prevPod,
		EventType:       event,
		ResourceVersion: curPod.ResourceVersion,
	}

	c.dispatchPodEvent(c.podClusterName(curPod), podEvent)
}
