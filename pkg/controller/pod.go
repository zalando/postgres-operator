package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
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

func (c *Controller) dispatchPodEvent(clusterName spec.NamespacedName, event spec.PodEvent) {
	c.clustersMu.RLock()
	cluster, ok := c.clusters[clusterName]
	c.clustersMu.RUnlock()
	if ok {
		cluster.ReceivePodEvent(event)
	}
}

func (c *Controller) podAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		PodName:         util.NameFromMeta(pod.ObjectMeta),
		CurPod:          pod,
		EventType:       spec.EventAdd,
		ResourceVersion: pod.ResourceVersion,
	}

	c.dispatchPodEvent(c.podClusterName(pod), podEvent)
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

	podEvent := spec.PodEvent{
		PodName:         util.NameFromMeta(curPod.ObjectMeta),
		PrevPod:         prevPod,
		CurPod:          curPod,
		EventType:       spec.EventUpdate,
		ResourceVersion: curPod.ResourceVersion,
	}

	c.dispatchPodEvent(c.podClusterName(curPod), podEvent)
}

func (c *Controller) podDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		PodName:         util.NameFromMeta(pod.ObjectMeta),
		CurPod:          pod,
		EventType:       spec.EventDelete,
		ResourceVersion: pod.ResourceVersion,
	}

	c.dispatchPodEvent(c.podClusterName(pod), podEvent)
}
