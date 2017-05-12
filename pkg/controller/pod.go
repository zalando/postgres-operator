package controller

import (
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Controller) podListFunc(options api.ListOptions) (runtime.Object, error) {
	var labelSelector string
	var fieldSelector string

	if options.LabelSelector != nil {
		labelSelector = options.LabelSelector.String()
	}

	if options.FieldSelector != nil {
		fieldSelector = options.FieldSelector.String()
	}
	opts := v1.ListOptions{
		LabelSelector:   labelSelector,
		FieldSelector:   fieldSelector,
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.CoreV1().Pods(c.opConfig.Namespace).List(opts)
}

func (c *Controller) podWatchFunc(options api.ListOptions) (watch.Interface, error) {
	var labelSelector string
	var fieldSelector string

	if options.LabelSelector != nil {
		labelSelector = options.LabelSelector.String()
	}

	if options.FieldSelector != nil {
		fieldSelector = options.FieldSelector.String()
	}

	opts := v1.ListOptions{
		LabelSelector:   labelSelector,
		FieldSelector:   fieldSelector,
		Watch:           options.Watch,
		ResourceVersion: options.ResourceVersion,
		TimeoutSeconds:  options.TimeoutSeconds,
	}

	return c.KubeClient.CoreV1Client.Pods(c.opConfig.Namespace).Watch(opts)
}

func (c *Controller) podAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName: c.PodClusterName(pod),
		PodName:     util.NameFromMeta(pod.ObjectMeta),
		CurPod:      pod,
		EventType:   spec.EventAdd,
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
		ClusterName: c.PodClusterName(curPod),
		PodName:     util.NameFromMeta(curPod.ObjectMeta),
		PrevPod:     prevPod,
		CurPod:      curPod,
		EventType:   spec.EventUpdate,
	}

	c.podCh <- podEvent
}

func (c *Controller) podDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName: c.PodClusterName(pod),
		PodName:     util.NameFromMeta(pod.ObjectMeta),
		CurPod:      pod,
		EventType:   spec.EventDelete,
	}

	c.podCh <- podEvent
}

func (c *Controller) podEventsDispatcher(stopCh <-chan struct{}) {
	c.logger.Infof("Watching all Pod events")
	for {
		select {
		case event := <-c.podCh:
			c.clustersMu.RLock()
			subscriber, ok := c.clusters[event.ClusterName]
			c.clustersMu.RUnlock()

			if ok {
				c.logger.Debugf("Sending %s event of Pod '%s' to the '%s' cluster channel", event.EventType, event.PodName, event.ClusterName)
				go subscriber.ReceivePodEvent(event)
			}
		case <-stopCh:
			return
		}
	}
}
