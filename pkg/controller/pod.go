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
	var labelSelector string
	var fieldSelector string

	opts := metav1.ListOptions{
		LabelSelector:   labelSelector,
		FieldSelector:   fieldSelector,
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Pods(c.opConfig.Namespace).List(opts)
}

func (c *Controller) podWatchFunc(options metav1.ListOptions) (watch.Interface, error) {
	var labelSelector string
	var fieldSelector string

	opts := metav1.ListOptions{
		LabelSelector:   labelSelector,
		FieldSelector:   fieldSelector,
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.Pods(c.opConfig.Namespace).Watch(opts)
}

func (c *Controller) podAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName:     c.podClusterName(pod),
		PodName:         util.NameFromMeta(pod.ObjectMeta),
		CurPod:          pod,
		EventType:       spec.EventAdd,
		ResourceVersion: pod.ResourceVersion,
	}

	c.podCh <- podEvent
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
		ClusterName:     c.podClusterName(curPod),
		PodName:         util.NameFromMeta(curPod.ObjectMeta),
		PrevPod:         prevPod,
		CurPod:          curPod,
		EventType:       spec.EventUpdate,
		ResourceVersion: curPod.ResourceVersion,
	}

	c.podCh <- podEvent
}

func (c *Controller) podDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName:     c.podClusterName(pod),
		PodName:         util.NameFromMeta(pod.ObjectMeta),
		CurPod:          pod,
		EventType:       spec.EventDelete,
		ResourceVersion: pod.ResourceVersion,
	}

	c.podCh <- podEvent
}

func (c *Controller) podEventsDispatcher(stopCh <-chan struct{}) {
	c.logger.Debugln("Watching all pod events")
	for {
		select {
		case event := <-c.podCh:
			c.clustersMu.RLock()
			cluster, ok := c.clusters[event.ClusterName]
			c.clustersMu.RUnlock()

			if ok {
				c.logger.Debugf("Sending %q event of pod %q to the %q cluster channel", event.EventType, event.PodName, event.ClusterName)
				cluster.ReceivePodEvent(event)
			}
		case <-stopCh:
			return
		}
	}
}
