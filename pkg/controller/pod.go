package controller

import (
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Controller) podListFunc(options meta_v1.ListOptions) (runtime.Object, error) {
	return c.KubeClient.CoreV1().Pods(c.opConfig.Namespace).List(options)
}

func (c *Controller) podWatchFunc(options meta_v1.ListOptions) (watch.Interface, error) {
	return c.KubeClient.CoreV1().Pods(c.opConfig.Namespace).Watch(options)
}

func (c *Controller) podAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName:     c.PodClusterName(pod),
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
		ClusterName:     c.PodClusterName(curPod),
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
		ClusterName:     c.PodClusterName(pod),
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
				c.logger.Debugf("Sending %s event of pod '%s' to the '%s' cluster channel", event.EventType, event.PodName, event.ClusterName)
				cluster.ReceivePodEvent(event)
			}
		case <-stopCh:
			return
		}
	}
}
