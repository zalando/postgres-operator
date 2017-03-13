package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/resources"
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

	desiredSvc := resources.Service(c.ClusterName(), c.Spec.TeamId, cSpec.AllowedSourceRanges)
	if servicesEqual(c.Service, desiredSvc) {
		return nil
	}
	c.logger.Infof("Service '%s' needs to be updated", util.NameFromMeta(desiredSvc.ObjectMeta))

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

	desiredSS := genStatefulSet(c.ClusterName(), cSpec, c.etcdHost, c.dockerImage)
	equalSS, rollUpdate := statefulsetsEqual(c.Statefulset, desiredSS)
	if equalSS {
		return nil
	}
	c.logger.Infof("StatefulSet '%s' is not in the desired state", util.NameFromMeta(c.Statefulset.ObjectMeta))

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
	pods, err := c.config.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of Pods: %s", err)
	}
	if int32(len(pods.Items)) != *curSs.Spec.Replicas {
		return fmt.Errorf("Number of existing Pods does not match number of replicas of the StatefulSet")
	}

	for _, pod := range pods.Items {
		if podMatchesTemplate(&pod, curSs) {
			c.logger.Infof("Pod '%s' matches StatefulSet pod template", util.NameFromMeta(pod.ObjectMeta))
			continue
		}

		c.logger.Infof("Pod '%s' does not match StatefulSet pod template and needs to be deleted.", util.NameFromMeta(pod.ObjectMeta))

		if util.PodSpiloRole(&pod) == "master" {
			//TODO: do manual failover first
		}
		err = c.recreatePod(pod, "replica") // newly created pods are always "replica"s

		c.logger.Infof("Pod '%s' has been successfully recreated", util.NameFromMeta(pod.ObjectMeta))
	}

	return nil
}
