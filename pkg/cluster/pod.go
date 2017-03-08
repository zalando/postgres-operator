package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
)

func (c *Cluster) clusterPods() ([]v1.Pod, error) {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pods, err := c.config.KubeClient.Pods(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("Can't get list of pods: %s", err)
	}

	return pods.Items, nil
}

func (c *Cluster) deletePods() error {
	pods, err := c.clusterPods()
	if err != nil {
		return err
	}

	for _, obj := range pods {
		if err := c.deletePod(&obj); err != nil {
			c.logger.Errorf("Can't delete pod: %s", err)
		} else {
			c.logger.Infof("Pod '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	return nil
}

func (c *Cluster) listPersistentVolumeClaims() ([]v1.PersistentVolumeClaim, error) {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pvcs, err := c.config.KubeClient.PersistentVolumeClaims(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("Can't get list of PersistentVolumeClaims: %s", err)
	}
	return pvcs.Items, nil
}

func (c *Cluster) deletePersistenVolumeClaims() error {
	ns := c.Metadata.Namespace
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	for _, pvc := range pvcs {
		if err := c.config.KubeClient.PersistentVolumeClaims(ns).Delete(pvc.Name, deleteOptions); err != nil {
			c.logger.Warningf("Can't delete PersistentVolumeClaim: %s", err)
		}
	}
	return nil
}

func (c *Cluster) deletePod(pod *v1.Pod) error {
	podName := spec.PodName{
		Namespace: pod.Namespace,
		Name:      pod.Name,
	}

	ch := make(chan spec.PodEvent)
	if _, ok := c.podSubscribers[podName]; ok {
		panic("Pod '" + podName.String() + "' is already subscribed")
	}
	c.podSubscribers[podName] = ch
	defer func() {
		close(ch)
		delete(c.podSubscribers, podName)
	}()

	if err := c.config.KubeClient.Pods(pod.Namespace).Delete(pod.Name, deleteOptions); err != nil {
		return err
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}

	return nil
}

func (c *Cluster) recreatePod(pod v1.Pod, spiloRole string) error {
	podName := spec.PodName{
		Namespace: pod.Namespace,
		Name:      pod.Name,
	}

	orphanDependents := false
	deleteOptions := &v1.DeleteOptions{
		OrphanDependents: &orphanDependents,
	}

	ch := make(chan spec.PodEvent)
	if _, ok := c.podSubscribers[podName]; ok {
		panic("Pod '" + podName.String() + "' is already subscribed")
	}
	c.podSubscribers[podName] = ch
	defer func() {
		close(ch)
		delete(c.podSubscribers, podName)
	}()

	if err := c.config.KubeClient.Pods(pod.Namespace).Delete(pod.Name, deleteOptions); err != nil {
		return fmt.Errorf("Can't delete pod: %s", err)
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}
	if err := c.waitForPodLabel(ch, spiloRole); err != nil {
		return err
	}
	c.logger.Infof("Pod '%s' is ready", podName)

	return nil
}

func (c *Cluster) podEventsDispatcher(stopCh <-chan struct{}) {
	c.logger.Infof("Watching '%s' cluster", c.ClusterName())
	for {
		select {
		case event := <-c.podEvents:
			if subscriber, ok := c.podSubscribers[event.PodName]; ok {
				c.logger.Debugf("New event for '%s' pod", event.PodName)
				go func() { subscriber <- event }() //TODO: is it a right way to do nonblocking send to the channel?
			} else {
				c.logger.Debugf("Skipping event for an unwatched pod '%s'", event.PodName)
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
	pods, err := c.config.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get the list of the pods: %s", err)
	} else {
		c.logger.Infof("There are %d pods in the cluster to recreate", len(pods.Items))
	}

	var masterPod v1.Pod
	for _, pod := range pods.Items {
		role, ok := pod.Labels["spilo-role"]
		if !ok {
			continue
		}

		if role == "master" {
			masterPod = pod
			continue
		}

		err = c.recreatePod(pod, "replica")
		if err != nil {
			return fmt.Errorf("Can't recreate replica pod '%s': %s", util.NameFromMeta(pod.ObjectMeta), err)
		}
	}

	//TODO: do manual failover
	//TODO: specify master, leave new master empty
	c.logger.Infof("Recreating master pod '%s'", util.NameFromMeta(masterPod.ObjectMeta))

	if err := c.recreatePod(masterPod, "replica"); err != nil {
		return fmt.Errorf("Can't recreate master pod '%s': %s", util.NameFromMeta(masterPod.ObjectMeta), err)
	}

	return nil
}
