package controller

import (
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
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

	return c.KubeClient.CoreV1().Pods(c.PodNamespace).List(opts)
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

	return c.KubeClient.CoreV1Client.Pods(c.PodNamespace).Watch(opts)
}

func PodNameFromMeta(meta v1.ObjectMeta) spec.PodName {
	return spec.PodName{
		Namespace: meta.Namespace,
		Name:      meta.Name,
	}
}

func (c *Controller) podAdd(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName: util.PodClusterName(pod),
		PodName:     PodNameFromMeta(pod.ObjectMeta),
		CurPod:      pod,
		EventType:   spec.PodEventAdd,
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
		ClusterName: util.PodClusterName(curPod),
		PodName:     PodNameFromMeta(curPod.ObjectMeta),
		PrevPod:     prevPod,
		CurPod:      curPod,
		EventType:   spec.PodEventUpdate,
	}

	c.podCh <- podEvent
}

func (c *Controller) podDelete(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		return
	}

	podEvent := spec.PodEvent{
		ClusterName: util.PodClusterName(pod),
		PodName:     PodNameFromMeta(pod.ObjectMeta),
		CurPod:      pod,
		EventType:   spec.PodEventDelete,
	}

	c.podCh <- podEvent
}

func (c *Controller) podEventsDispatcher(stopCh <-chan struct{}) {
	c.logger.Infof("Watching all Pod events")
	for {
		select {
		case event := <-c.podCh:
			if subscriber, ok := c.clusters[event.ClusterName]; ok {
				c.logger.Debugf("Sending %s event of Pod '%s' to the '%s' cluster channel", event.EventType, event.PodName, event.ClusterName)
				go subscriber.ReceivePodEvent(event)
			} else {
				c.logger.Debugf("Skipping %s event of the '%s' pod", event.EventType, event.PodName)
			}
		case <-stopCh:
			return
		}
	}
}
