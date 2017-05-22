package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

func (c *Cluster) listPods() ([]v1.Pod, error) {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pods, err := c.KubeClient.Pods(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
	}

	return pods.Items, nil
}

func (c *Cluster) listPersistentVolumeClaims() ([]v1.PersistentVolumeClaim, error) {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pvcs, err := c.KubeClient.PersistentVolumeClaims(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of PersistentVolumeClaims: %v", err)
	}
	return pvcs.Items, nil
}

func (c *Cluster) deletePods() error {
	c.logger.Debugln("Deleting pods")
	pods, err := c.listPods()
	if err != nil {
		return err
	}

	for _, obj := range pods {
		podName := util.NameFromMeta(obj.ObjectMeta)

		c.logger.Debugf("Deleting pod '%s'", podName)
		if err := c.deletePod(podName); err != nil {
			c.logger.Errorf("could not delete pod '%s': %s", podName, err)
		} else {
			c.logger.Infof("pod '%s' has been deleted", podName)
		}
	}
	if len(pods) > 0 {
		c.logger.Debugln("pods have been deleted")
	} else {
		c.logger.Debugln("No pods to delete")
	}

	return nil
}

func (c *Cluster) deletePersistenVolumeClaims() error {
	c.logger.Debugln("Deleting PVCs")
	ns := c.Metadata.Namespace
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	for _, pvc := range pvcs {
		c.logger.Debugf("Deleting PVC '%s'", util.NameFromMeta(pvc.ObjectMeta))
		if err := c.KubeClient.PersistentVolumeClaims(ns).Delete(pvc.Name, c.deleteOptions); err != nil {
			c.logger.Warningf("could not delete PersistentVolumeClaim: %v", err)
		}
	}
	if len(pvcs) > 0 {
		c.logger.Debugln("PVCs have been deleted")
	} else {
		c.logger.Debugln("No PVCs to delete")
	}

	return nil
}

func (c *Cluster) deletePod(podName spec.NamespacedName) error {
	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)

	if err := c.KubeClient.Pods(podName.Namespace).Delete(podName.Name, c.deleteOptions); err != nil {
		return err
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) unregisterPodSubscriber(podName spec.NamespacedName) {
	c.podSubscribersMu.Lock()
	defer c.podSubscribersMu.Unlock()

	if _, ok := c.podSubscribers[podName]; !ok {
		panic("Subscriber for pod '" + podName.String() + "' is not found")
	}

	close(c.podSubscribers[podName])
	delete(c.podSubscribers, podName)
}

func (c *Cluster) registerPodSubscriber(podName spec.NamespacedName) chan spec.PodEvent {
	c.podSubscribersMu.Lock()
	defer c.podSubscribersMu.Unlock()

	ch := make(chan spec.PodEvent)
	if _, ok := c.podSubscribers[podName]; ok {
		panic("pod '" + podName.String() + "' is already subscribed")
	}
	c.podSubscribers[podName] = ch

	return ch
}

func (c *Cluster) recreatePod(pod v1.Pod) error {
	podName := util.NameFromMeta(pod.ObjectMeta)

	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)

	if err := c.KubeClient.Pods(pod.Namespace).Delete(pod.Name, c.deleteOptions); err != nil {
		return fmt.Errorf("could not delete pod: %v", err)
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}
	if err := c.waitForPodLabel(ch); err != nil {
		return err
	}
	c.logger.Infof("pod '%s' is ready", podName)

	return nil
}

func (c *Cluster) podEventsDispatcher(stopCh <-chan struct{}) {
	c.logger.Infof("Watching '%s' cluster", c.ClusterName())
	for {
		select {
		case event := <-c.podEvents:
			c.podSubscribersMu.RLock()
			subscriber, ok := c.podSubscribers[event.PodName]
			c.podSubscribersMu.RUnlock()
			if ok {
				go func() { subscriber <- event }() //TODO: is it a right way to do nonblocking send to the channel?
			}
		case <-stopCh:
			return
		}
	}
}

func (c *Cluster) recreatePods() error {
	ls := c.labelsSet()
	namespace := c.Metadata.Namespace

	listOptions := v1.ListOptions{
		LabelSelector: ls.String(),
	}

	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get the list of pods: %v", err)
	}
	c.logger.Infof("There are %d pods in the cluster to recreate", len(pods.Items))

	var masterPod v1.Pod
	for _, pod := range pods.Items {
		role := c.podSpiloRole(&pod)

		if role == constants.PodRoleMaster {
			masterPod = pod
			continue
		}

		if err := c.recreatePod(pod); err != nil {
			return fmt.Errorf("could not recreate replica pod '%s': %v", util.NameFromMeta(pod.ObjectMeta), err)
		}
	}
	if masterPod.Name == "" {
		c.logger.Warningln("No master pod in the cluster")
	}

	//TODO: do manual failover
	//TODO: specify master, leave new master empty
	c.logger.Infof("Recreating master pod '%s'", util.NameFromMeta(masterPod.ObjectMeta))

	if err := c.recreatePod(masterPod); err != nil {
		return fmt.Errorf("could not recreate master pod '%s': %v", util.NameFromMeta(masterPod.ObjectMeta), err)
	}

	return nil
}
