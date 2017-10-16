package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
)

func (c *Cluster) listPods() ([]v1.Pod, error) {
	ns := c.Namespace
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pods, err := c.KubeClient.Pods(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not get list of pods: %v", err)
	}

	return pods.Items, nil
}

func (c *Cluster) deletePods() error {
	c.logger.Debugln("deleting pods")
	pods, err := c.listPods()
	if err != nil {
		return err
	}

	for _, obj := range pods {
		podName := util.NameFromMeta(obj.ObjectMeta)

		c.logger.Debugf("deleting pod %q", podName)
		if err := c.deletePod(podName); err != nil {
			c.logger.Errorf("could not delete pod %q: %v", podName, err)
		} else {
			c.logger.Infof("pod %q has been deleted", podName)
		}
	}
	if len(pods) > 0 {
		c.logger.Debugln("pods have been deleted")
	} else {
		c.logger.Debugln("no pods to delete")
	}

	return nil
}

func (c *Cluster) deletePod(podName spec.NamespacedName) error {
	c.setProcessName("deleting %q pod", podName)
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
		panic("subscriber for pod '" + podName.String() + "' is not found")
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

func (c *Cluster) recreatePod(podName spec.NamespacedName) error {
	c.setProcessName("recreating %q pod", podName)

	ch := c.registerPodSubscriber(podName)
	defer c.unregisterPodSubscriber(podName)

	if err := c.KubeClient.Pods(podName.Namespace).Delete(podName.Name, c.deleteOptions); err != nil {
		return fmt.Errorf("could not delete pod: %v", err)
	}

	if err := c.waitForPodDeletion(ch); err != nil {
		return err
	}
	if err := c.waitForPodLabel(ch, nil); err != nil {
		return err
	}
	c.logger.Infof("pod %q is ready", podName)

	return nil
}

func (c *Cluster) recreatePods() error {
	c.setProcessName("recreating pods")
	ls := c.labelsSet()
	namespace := c.Namespace

	listOptions := metav1.ListOptions{
		LabelSelector: ls.String(),
	}

	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get the list of pods: %v", err)
	}
	c.logger.Infof("there are %d pods in the cluster to recreate", len(pods.Items))

	var masterPod *v1.Pod
	replicas := make([]spec.NamespacedName, 0)
	for i, pod := range pods.Items {
		role := PostgresRole(pod.Labels[c.OpConfig.PodRoleLabel])

		if role == Master {
			masterPod = &pods.Items[i]
			continue
		}

		podName := util.NameFromMeta(pods.Items[i].ObjectMeta)
		if err := c.recreatePod(podName); err != nil {
			return fmt.Errorf("could not recreate replica pod %q: %v", util.NameFromMeta(pod.ObjectMeta), err)
		}

		replicas = append(replicas, util.NameFromMeta(pod.ObjectMeta))
	}

	if masterPod == nil {
		c.logger.Warningln("no master pod in the cluster")
	} else {
		if len(replicas) > 0 {
			err := c.ManualFailover(masterPod, masterCandidate(replicas))
			if err != nil {
				return fmt.Errorf("could not perform manual failover: %v", err)
			}
		}
		//TODO: specify master, leave new master empty
		c.logger.Infof("recreating master pod %q", util.NameFromMeta(masterPod.ObjectMeta))

		if err := c.recreatePod(util.NameFromMeta(masterPod.ObjectMeta)); err != nil {
			return fmt.Errorf("could not recreate master pod %q: %v", util.NameFromMeta(masterPod.ObjectMeta), err)
		}
	}

	return nil
}
