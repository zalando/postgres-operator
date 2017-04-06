package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
)

func (c *Cluster) SyncCluster() {
	if err := c.syncSecrets(); err != nil {
		c.logger.Infof("Can't sync Secrets: %s", err)
	}

	if err := c.syncEndpoint(); err != nil {
		c.logger.Errorf("Can't sync Endpoints: %s", err)
	}

	if err := c.syncService(); err != nil {
		c.logger.Errorf("Can't sync Services: %s", err)
	}

	if err := c.syncStatefulSet(); err != nil {
		c.logger.Errorf("Can't sync StatefulSets: %s", err)
	}

	if err := c.syncPods(); err != nil {
		c.logger.Errorf("Can't sync Pods: %s", err)
	}
}

func (c *Cluster) syncSecrets() error {
	//TODO: mind the secrets of the deleted/new users
	err := c.applySecrets()
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) syncService() error {
	cSpec := c.Spec
	if c.Service == nil {
		c.logger.Infof("Can't find the cluster's Service")
		svc, err := c.createService()
		if err != nil {
			return fmt.Errorf("Can't create missing Service: %s", err)
		}
		c.logger.Infof("Created missing Service '%s'", util.NameFromMeta(svc.ObjectMeta))

		return nil
	}

	desiredSvc := c.genService(cSpec.AllowedSourceRanges)
	if match, reason := c.sameServiceWith(desiredSvc); match {
		return nil
	} else {
		c.logServiceChanges(c.Service, desiredSvc, false, reason)
	}

	if err := c.updateService(desiredSvc); err != nil {
		return fmt.Errorf("Can't update Service to match desired state: %s", err)
	}
	c.logger.Infof("Service '%s' is in the desired state now", util.NameFromMeta(desiredSvc.ObjectMeta))

	return nil
}

func (c *Cluster) syncEndpoint() error {
	if c.Endpoint == nil {
		c.logger.Infof("Can't find the cluster's Endpoint")
		ep, err := c.createEndpoint()
		if err != nil {
			return fmt.Errorf("Can't create missing Endpoint: %s", err)
		}
		c.logger.Infof("Created missing Endpoint '%s'", util.NameFromMeta(ep.ObjectMeta))
		return nil
	}

	return nil
}

func (c *Cluster) syncStatefulSet() error {
	cSpec := c.Spec
	if c.Statefulset == nil {
		c.logger.Infof("Can't find the cluster's StatefulSet")
		ss, err := c.createStatefulSet()
		if err != nil {
			return fmt.Errorf("Can't create missing StatefulSet: %s", err)
		}
		err = c.waitStatefulsetPodsReady()
		if err != nil {
			return fmt.Errorf("Cluster is not ready: %s", err)
		}
		c.logger.Infof("Created missing StatefulSet '%s'", util.NameFromMeta(ss.ObjectMeta))
		return nil
	}

	desiredSS := c.genStatefulSet(cSpec)
	match, rollUpdate, reason := c.compareStatefulSetWith(desiredSS)
	if match {
		return nil
	}
	c.logStatefulSetChanges(c.Statefulset, desiredSS, false, reason)

	if err := c.updateStatefulSet(desiredSS); err != nil {
		return fmt.Errorf("Can't update StatefulSet: %s", err)
	}

	if !rollUpdate {
		c.logger.Debugln("No rolling update is needed")
		return nil
	}
	c.logger.Debugln("Performing rolling update")
	if err := c.recreatePods(); err != nil {
		return fmt.Errorf("Can't recreate Pods: %s", err)
	}
	c.logger.Infof("Pods have been recreated")

	return nil
}

func (c *Cluster) syncPods() error {
	curSs := c.Statefulset

	ls := c.labelsSet()
	namespace := c.Metadata.Namespace

	listOptions := v1.ListOptions{
		LabelSelector: ls.String(),
	}
	pods, err := c.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of Pods: %s", err)
	}
	if int32(len(pods.Items)) != *curSs.Spec.Replicas {
		//TODO: wait for Pods being created by StatefulSet
		return fmt.Errorf("Number of existing Pods does not match number of replicas of the StatefulSet")
	}

	//First check if we have left overs from the previous rolling update
	for _, pod := range pods.Items {
		podRole := util.PodSpiloRole(&pod)
		podName := spec.PodName{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		}
		match, _ := podMatchesTemplate(&pod, curSs)
		if match && pod.Status.Phase == v1.PodPending {
			c.logger.Infof("Waiting for left over Pod '%s'", podName)
			ch := c.registerPodSubscriber(podName)
			c.waitForPodLabel(ch, podRole)
			c.unregisterPodSubscriber(podName)
		}
	}

	for _, pod := range pods.Items {
		if match, reason := podMatchesTemplate(&pod, curSs); match {
			c.logger.Infof("Pod '%s' matches StatefulSet pod template", util.NameFromMeta(pod.ObjectMeta))
			continue
		} else {
			c.logPodChanges(&pod, curSs, reason)
		}

		if util.PodSpiloRole(&pod) == "master" {
			//TODO: do manual failover first
		}
		err = c.recreatePod(pod, "replica") // newly created pods are always "replica"s

		c.logger.Infof("Pod '%s' has been successfully recreated", util.NameFromMeta(pod.ObjectMeta))
	}

	return nil
}
