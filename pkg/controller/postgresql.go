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
		return nil, fmt.Errorf("could not get list of postgresql objects: %v", err)
	}

	objList, err := meta.ExtractList(object)
	if err != nil {
		return nil, fmt.Errorf("could not extract list of postgresql objects: %v", err)
	}

	var activeClustersCnt, failedClustersCnt int
	for _, obj := range objList {
		pg, ok := obj.(*spec.Postgresql)
		if !ok {
			return nil, fmt.Errorf("could not cast object to postgresql")
		}

		if pg.Error != nil {
			failedClustersCnt++
			continue
		}
		c.queueClusterEvent(nil, pg, spec.EventSync)
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

func (c *Controller) processAddEvent(workerID uint32, clusterName spec.NamespacedName, spec *spec.Postgresql) {
	log := c.logger.WithField("worker", workerID)

	log.Infof("Creation of the '%s' cluster started", clusterName)

	stopCh := make(chan struct{})
	cl := cluster.New(c.makeClusterConfig(), *spec, log)
	cl.Run(stopCh)

	c.clustersMu.Lock()
	c.clusters[clusterName] = cl
	c.stopChs[clusterName] = stopCh
	c.clustersMu.Unlock()

	if err := cl.Create(); err != nil {
		cl.Error = fmt.Errorf("could not create cluster: %v", err)
		log.Errorf("%v", cl.Error)

		return
	}

	log.Infof("Cluster '%s' has been created", clusterName)
}

func (c *Controller) processUpdateEvent(workerID uint32, clusterName spec.NamespacedName, cl *cluster.Cluster, newSpec *spec.Postgresql) {
	log := c.logger.WithField("worker", workerID)

	log.Infof("Update of the '%s' cluster started", clusterName)

	if err := cl.Update(newSpec); err != nil {
		cl.Error = fmt.Errorf("could not update cluster: %s", err)
		log.Errorf("%v", cl.Error)

		return
	}
	cl.Error = nil

	log.Infof("Cluster '%s' has been updated", clusterName)
}

func (c *Controller) processDeleteEvent(workerID uint32, clusterName spec.NamespacedName, cl *cluster.Cluster) {
	log := c.logger.WithField("worker", workerID)

	log.Infof("Deletion of the '%s' cluster started", clusterName)

	if err := cl.Delete(); err != nil {
		log.Errorf("could not delete cluster '%s': %s", clusterName, err)

		return
	}
	close(c.stopChs[clusterName])

	c.clustersMu.Lock()
	delete(c.clusters, clusterName)
	delete(c.stopChs, clusterName)
	c.clustersMu.Unlock()

	log.Infof("Cluster '%s' has been deleted", clusterName)
}

func (c *Controller) processSyncEvent(workerID uint32, cl *cluster.Cluster, clusterFound bool, clusterName spec.NamespacedName, newSpec *spec.Postgresql) {
	log := c.logger.WithField("worker", workerID)

	log.Infof("Syncing of the '%s' cluster started", clusterName)

	// no race condition because a cluster is always processed by single worker
	if !clusterFound {
		stopCh := make(chan struct{})
		cl := cluster.New(c.makeClusterConfig(), *newSpec, log)
		cl.Run(stopCh)

		c.clustersMu.Lock()
		c.clusters[clusterName] = cl
		c.stopChs[clusterName] = stopCh
		c.clustersMu.Unlock()
	}

	if err := cl.Sync(); err != nil {
		cl.Error = fmt.Errorf("could not sync cluster '%s': %s", clusterName, err)
		log.Errorf("%v", cl)

		return
	}
	cl.Error = nil

	log.Infof("Cluster '%s' has been synced", clusterName)
}

func (c *Controller) processEvent(obj interface{}) error {
	var clusterName spec.NamespacedName

	event, ok := obj.(spec.ClusterEvent)
	if !ok {
		return fmt.Errorf("could not cast to ClusterEvent")
	}
	log := c.logger.WithField("worker", event.WorkerID)

	if event.EventType == spec.EventAdd || event.EventType == spec.EventSync {
		clusterName = util.NameFromMeta(event.NewSpec.Metadata)
	} else {
		clusterName = util.NameFromMeta(event.OldSpec.Metadata)
	}

	c.clustersMu.RLock()
	cl, clusterFound := c.clusters[clusterName]
	c.clustersMu.RUnlock()

	switch event.EventType {
	case spec.EventAdd:
		if clusterFound {
			log.Debugf("Cluster '%s' already exists", clusterName)
			return nil
		}

		c.processAddEvent(event.WorkerID, clusterName, event.NewSpec)
	case spec.EventUpdate:
		if !clusterFound {
			log.Warnf("Cluster '%s' does not exist", clusterName)
			return nil
		}

		c.processUpdateEvent(event.WorkerID, clusterName, cl, event.NewSpec)
	case spec.EventDelete:
		if !clusterFound {
			log.Errorf("Unknown cluster: %s", clusterName)
			return nil
		}

		c.processDeleteEvent(event.WorkerID, clusterName, cl)
	case spec.EventSync:
		c.processSyncEvent(event.WorkerID, cl, clusterFound, clusterName, event.NewSpec)
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
		if eventType == spec.EventUpdate && new.Error == nil && old.Error != nil {
			eventType = spec.EventSync
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
		c.logger.Debugf("Skipping %s event for invalid cluster %s (reason: %v)", eventType, clusterName, clusterError)
		return
	}

	workerID := c.clusterWorkerID(clusterName)
	clusterEvent := spec.ClusterEvent{
		EventType: eventType,
		UID:       uid,
		OldSpec:   old,
		NewSpec:   new,
		WorkerID:  workerID,
	}
	//TODO: if we delete cluster, discard all the previous events for the cluster

	c.clusterEventQueues[workerID].Add(clusterEvent)
	c.logger.WithField("worker", workerID).Infof("%s of the '%s' cluster has been queued", eventType, clusterName)
}

func (c *Controller) postgresqlAdd(obj interface{}) {
	pg, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("could not cast to postgresql spec")
		return
	}

	// We will not get multiple Add events for the same cluster
	c.queueClusterEvent(nil, pg, spec.EventAdd)
}

func (c *Controller) postgresqlUpdate(prev, cur interface{}) {
	pgOld, ok := prev.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("could not cast to postgresql spec")
	}
	pgNew, ok := cur.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("could not cast to postgresql spec")
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
		c.logger.Errorf("could not cast to postgresql spec")
		return
	}

	c.queueClusterEvent(pg, nil, spec.EventDelete)
}
