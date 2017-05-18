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

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

func (c *Controller) clusterListFunc(options api.ListOptions) (runtime.Object, error) {
	c.logger.Info("Getting list of currently running clusters")

	req := c.RestClient.Get().
		RequestURI(fmt.Sprintf(constants.ListClustersURITemplate, c.opConfig.Namespace)).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything())

	object, err := req.Do().Get()

	if err != nil {
		return nil, fmt.Errorf("Can't get list of postgresql objects: %s", err)
	}

	objList, err := meta.ExtractList(object)
	if err != nil {
		return nil, fmt.Errorf("Can't extract list of postgresql objects: %s", err)
	}

	var activeClustersCnt, failedClustersCnt int
	for _, obj := range objList {
		pg, ok := obj.(*spec.Postgresql)
		if !ok {
			return nil, fmt.Errorf("Can't cast object to postgresql")
		}

		if pg.Error != nil {
			failedClustersCnt++
			continue
		}
		c.queueClusterEvent(nil, pg, spec.EventSync)

		c.logger.Debugf("Sync of the '%s' cluster has been queued", util.NameFromMeta(pg.Metadata))
		activeClustersCnt++
	}
	if len(objList) > 0 {
		if failedClustersCnt > 0 && activeClustersCnt == 0 {
			c.logger.Infof("There are no clusters running. %d are in the failed state", failedClustersCnt)
		} else if failedClustersCnt == 0 && activeClustersCnt > 0 {
			c.logger.Infof("There are %d clusters running", activeClustersCnt)
		} else {
			c.logger.Infof("There are %d clusters running and %d are in the failed state", activeClustersCnt, failedClustersCnt)
		}
	} else {
		c.logger.Infof("No clusters running")
	}

	return object, err
}

func (c *Controller) clusterWatchFunc(options api.ListOptions) (watch.Interface, error) {
	req := c.RestClient.Get().
		RequestURI(fmt.Sprintf(constants.WatchClustersURITemplate, c.opConfig.Namespace)).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything())

	return req.Watch()
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

		if err := cl.Sync(stopCh); err != nil {
			logger.Errorf("Can't sync cluster '%s': %s", clusterName, err)
			return nil
		}

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
		uid          types.UID
		clusterName  spec.NamespacedName
		clusterError error
	)

	if old != nil { //update, delete
		uid = old.Metadata.GetUID()
		clusterName = util.NameFromMeta(old.Metadata)
		if eventType == spec.EventUpdate && new.Error == nil && old != nil {
			eventType = spec.EventAdd
			clusterError = new.Error
		} else {
			clusterError = old.Error
		}
	} else { //add, sync
		uid = new.Metadata.GetUID()
		clusterName = util.NameFromMeta(new.Metadata)
		clusterError = new.Error
	}

	if clusterError != nil && eventType != spec.EventDelete {
		c.logger.Debugf("Skipping %s event for invalid cluster %s (reason: %s)", eventType, clusterName, clusterError)
		return
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
