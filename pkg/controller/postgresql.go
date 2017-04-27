package controller

import (
	"fmt"
	"reflect"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

func (c *Controller) clusterListFunc(options api.ListOptions) (runtime.Object, error) {
	c.logger.Info("Getting list of currently running clusters")
	object, err := c.RestClient.Get().
		Namespace(c.opConfig.Namespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything()).
		Do().
		Get()

	if err != nil {
		return nil, fmt.Errorf("Can't get list of postgresql objects: %s", err)
	}

	objList, err := meta.ExtractList(object)
	if err != nil {
		return nil, fmt.Errorf("Can't extract list of postgresql objects: %s", err)
	}

	for _, obj := range objList {
		pg, ok := obj.(*spec.Postgresql)
		if !ok {
			return nil, fmt.Errorf("Can't cast object to postgresql")
		}
		c.queueClusterEvent(nil, pg, spec.EventSync)

		c.logger.Debugf("Sync of the '%s' cluster has been queued", util.NameFromMeta(pg.Metadata))
	}
	if len(objList) > 0 {
		c.logger.Infof("There are %d clusters currently running", len(objList))
	} else {
		c.logger.Infof("No clusters running")
	}

	return object, err
}

func (c *Controller) processEvent(obj interface{}) error {
	var clusterName spec.NamespacedName

	event, ok := obj.(spec.ClusterEvent)
	if !ok {
		return fmt.Errorf("Can't cast to ClusterEvent")
	}
	logger := c.logger.WithField("worker", event.WorkerID)

	if event.EventType == spec.EventAdd || event.EventType == spec.EventSync {
		clusterName = util.NameFromMeta(event.NewSpec.Metadata)
	} else {
		clusterName = util.NameFromMeta(event.OldSpec.Metadata)
	}

	c.clustersMu.RLock()
	cl, clusterFound := c.clusters[clusterName]
	stopCh := c.stopChs[clusterName]
	c.clustersMu.RUnlock()

	switch event.EventType {
	case spec.EventAdd:
		if clusterFound {
			logger.Debugf("Cluster '%s' already exists", clusterName)
			return nil
		}

		logger.Infof("Creation of the '%s' cluster started", clusterName)

		stopCh := make(chan struct{})
		cl = cluster.New(c.makeClusterConfig(), *event.NewSpec, logger)

		c.clustersMu.Lock()
		c.clusters[clusterName] = cl
		c.stopChs[clusterName] = stopCh
		c.clustersMu.Unlock()

		if err := cl.Create(stopCh); err != nil {
			logger.Errorf("Can't create cluster: %s", err)
			return nil
		}

		logger.Infof("Cluster '%s' has been created", clusterName)
	case spec.EventUpdate:
		logger.Infof("Update of the '%s' cluster started", clusterName)

		if !clusterFound {
			logger.Warnf("Cluster '%s' does not exist", clusterName)
			return nil
		}
		if err := cl.Update(event.NewSpec); err != nil {
			logger.Errorf("Can't update cluster: %s", err)
			return nil
		}
		logger.Infof("Cluster '%s' has been updated", clusterName)
	case spec.EventDelete:
		logger.Infof("Deletion of the '%s' cluster started", clusterName)
		if !clusterFound {
			logger.Errorf("Unknown cluster: %s", clusterName)
			return nil
		}

		if err := cl.Delete(); err != nil {
			logger.Errorf("Can't delete cluster '%s': %s", clusterName, err)
			return nil
		}
		close(c.stopChs[clusterName])

		c.clustersMu.Lock()
		delete(c.clusters, clusterName)
		delete(c.stopChs, clusterName)
		c.clustersMu.Unlock()

		logger.Infof("Cluster '%s' has been deleted", clusterName)
	case spec.EventSync:
		logger.Infof("Syncing of the '%s' cluster started", clusterName)

		// no race condition because a cluster is always processed by single worker
		if !clusterFound {
			cl = cluster.New(c.makeClusterConfig(), *event.NewSpec, logger)
			stopCh = make(chan struct{})

			c.clustersMu.Lock()
			c.clusters[clusterName] = cl
			c.stopChs[clusterName] = stopCh
			c.clustersMu.Unlock()
		}

		cl.SyncCluster(stopCh)

		logger.Infof("Cluster '%s' has been synced", clusterName)
	}

	return nil
}

func (c *Controller) processClusterEventsQueue(idx int) {
	for {
		c.clusterEventQueues[idx].Pop(cache.PopProcessFunc(c.processEvent))
	}
}

func (c *Controller) queueClusterEvent(old, new *spec.Postgresql, eventType spec.EventType) {
	var (
		uid types.UID
		clusterName spec.NamespacedName
	)

	if old != nil {
		uid = old.Metadata.GetUID()
		clusterName = util.NameFromMeta(old.Metadata)
	} else {
		uid = new.Metadata.GetUID()
		clusterName = util.NameFromMeta(new.Metadata)
	}
	workerId := c.clusterWorkerId(clusterName)
	clusterEvent := spec.ClusterEvent{
		EventType: eventType,
		UID:       uid,
		OldSpec:   old,
		NewSpec:   new,
		WorkerID:  workerId,
	}
	//TODO: if we delete cluster, discard all the previous events for the cluster

	c.clusterEventQueues[workerId].Add(clusterEvent)
	c.logger.WithField("worker", workerId).Infof("%s of the '%s' cluster has been queued for", eventType, clusterName)
}

func (c *Controller) clusterWatchFunc(options api.ListOptions) (watch.Interface, error) {
	return c.RestClient.Get().
		Prefix("watch").
		Namespace(c.opConfig.Namespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything()).
		Watch()
}

func (c *Controller) postgresqlAdd(obj interface{}) {
	pg, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
		return
	}

	// We will not get multiple Add events for the same cluster
	c.queueClusterEvent(nil, pg, spec.EventAdd)
}

func (c *Controller) postgresqlUpdate(prev, cur interface{}) {
	pgOld, ok := prev.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
	}
	pgNew, ok := cur.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
	}
	if pgOld.Metadata.ResourceVersion == pgNew.Metadata.ResourceVersion {
		return
	}
	if reflect.DeepEqual(pgOld.Spec, pgNew.Spec) {
		return
	}

	c.queueClusterEvent(pgOld, pgNew, spec.EventUpdate)
}

func (c *Controller) postgresqlDelete(obj interface{}) {
	pg, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
		return
	}

	c.queueClusterEvent(pg, nil, spec.EventDelete)
}
