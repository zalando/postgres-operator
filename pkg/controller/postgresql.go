package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

func (c *Controller) clusterResync(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(c.opConfig.ResyncPeriod)

	for {
		select {
		case <-ticker.C:
			c.clusterListFunc(metav1.ListOptions{ResourceVersion: "0"})
		case <-stopCh:
			return
		}
	}
}

func (c *Controller) clusterListFunc(options metav1.ListOptions) (runtime.Object, error) {
	var list spec.PostgresqlList
	var activeClustersCnt, failedClustersCnt int

	req := c.RestClient.
		Get().
		Namespace(c.opConfig.Namespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, metav1.ParameterCodec)

	b, err := req.DoRaw()
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(b, &list)

	if time.Now().Unix()-atomic.LoadInt64(&c.lastClusterSyncTime) <= int64(c.opConfig.ResyncPeriod.Seconds()) {
		c.logger.Debugln("skipping resync of clusters")
		return &list, err
	}

	for i, pg := range list.Items {
		if pg.Error != nil {
			failedClustersCnt++
			continue
		}
		c.queueClusterEvent(nil, &list.Items[i], spec.EventSync)
		activeClustersCnt++
	}
	if len(list.Items) > 0 {
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

	atomic.StoreInt64(&c.lastClusterSyncTime, time.Now().Unix())

	return &list, err
}

type tprDecoder struct {
	dec   *json.Decoder
	close func() error
}

func (d *tprDecoder) Close() {
	d.close()
}

func (d *tprDecoder) Decode() (action watch.EventType, object runtime.Object, err error) {
	var e struct {
		Type   watch.EventType
		Object spec.Postgresql
	}
	if err := d.dec.Decode(&e); err != nil {
		return watch.Error, nil, err
	}

	return e.Type, &e.Object, nil
}

func (c *Controller) clusterWatchFunc(options metav1.ListOptions) (watch.Interface, error) {
	options.Watch = true
	r, err := c.RestClient.
		Get().
		Namespace(c.opConfig.Namespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, metav1.ParameterCodec).
		FieldsSelectorParam(nil).
		Stream()

	if err != nil {
		return nil, err
	}

	return watch.NewStreamWatcher(&tprDecoder{
		dec:   json.NewDecoder(r),
		close: r.Close,
	}), nil
}

func (c *Controller) processEvent(obj interface{}) error {
	var clusterName spec.NamespacedName

	event, ok := obj.(spec.ClusterEvent)
	if !ok {
		return fmt.Errorf("could not cast to ClusterEvent")
	}
	logger := c.logger.WithField("worker", event.WorkerID)

	if event.EventType == spec.EventAdd || event.EventType == spec.EventSync {
		clusterName = util.NameFromMeta(event.NewSpec.ObjectMeta)
	} else {
		clusterName = util.NameFromMeta(event.OldSpec.ObjectMeta)
	}

	c.clustersMu.RLock()
	cl, clusterFound := c.clusters[clusterName]
	c.clustersMu.RUnlock()

	switch event.EventType {
	case spec.EventAdd:
		if clusterFound {
			logger.Debugf("Cluster %q already exists", clusterName)
			return nil
		}

		logger.Infof("Creation of the %q cluster started", clusterName)

		stopCh := make(chan struct{})
		cl = cluster.New(c.makeClusterConfig(), c.KubeClient, *event.NewSpec, logger)
		cl.Run(stopCh)

		c.clustersMu.Lock()
		c.clusters[clusterName] = cl
		c.stopChs[clusterName] = stopCh
		c.clustersMu.Unlock()

		if err := cl.Create(); err != nil {
			cl.Error = fmt.Errorf("could not create cluster: %v", err)
			logger.Errorf("%v", cl.Error)

			return nil
		}

		logger.Infof("Cluster %q has been created", clusterName)
	case spec.EventUpdate:
		logger.Infof("Update of the %q cluster started", clusterName)

		if !clusterFound {
			logger.Warnf("Cluster %q does not exist", clusterName)
			return nil
		}
		if err := cl.Update(event.NewSpec); err != nil {
			cl.Error = fmt.Errorf("could not update cluster: %v", err)
			logger.Errorf("%v", cl.Error)

			return nil
		}
		cl.Error = nil
		logger.Infof("Cluster %q has been updated", clusterName)
	case spec.EventDelete:
		logger.Infof("Deletion of the %q cluster started", clusterName)
		if !clusterFound {
			logger.Errorf("Unknown cluster: %q", clusterName)
			return nil
		}

		if err := cl.Delete(); err != nil {
			logger.Errorf("could not delete cluster %q: %v", clusterName, err)
			return nil
		}
		close(c.stopChs[clusterName])

		c.clustersMu.Lock()
		delete(c.clusters, clusterName)
		delete(c.stopChs, clusterName)
		c.clustersMu.Unlock()

		logger.Infof("Cluster %q has been deleted", clusterName)
	case spec.EventSync:
		logger.Infof("Syncing of the %q cluster started", clusterName)

		// no race condition because a cluster is always processed by single worker
		if !clusterFound {
			stopCh := make(chan struct{})
			cl = cluster.New(c.makeClusterConfig(), c.KubeClient, *event.NewSpec, logger)
			cl.Run(stopCh)

			c.clustersMu.Lock()
			c.clusters[clusterName] = cl
			c.stopChs[clusterName] = stopCh
			c.clustersMu.Unlock()
		}

		if err := cl.Sync(); err != nil {
			cl.Error = fmt.Errorf("could not sync cluster %q: %v", clusterName, err)
			logger.Errorf("%v", cl.Error)
			return nil
		}
		cl.Error = nil

		logger.Infof("Cluster %q has been synced", clusterName)
	}

	return nil
}

func (c *Controller) processClusterEventsQueue(idx int, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	go func() {
		<-stopCh
		c.clusterEventQueues[idx].Close()
	}()

	for {
		if _, err := c.clusterEventQueues[idx].Pop(cache.PopProcessFunc(c.processEvent)); err != nil {
			if err == cache.FIFOClosedError {
				return
			}

			c.logger.Errorf("error when processing cluster events queue: %v", err)
		}
	}
}

func (c *Controller) queueClusterEvent(old, new *spec.Postgresql, eventType spec.EventType) {
	var (
		uid          types.UID
		clusterName  spec.NamespacedName
		clusterError error
	)

	if old != nil { //update, delete
		uid = old.GetUID()
		clusterName = util.NameFromMeta(old.ObjectMeta)
		if eventType == spec.EventUpdate && new.Error == nil && old.Error != nil {
			eventType = spec.EventSync
			clusterError = new.Error
		} else {
			clusterError = old.Error
		}
	} else { //add, sync
		uid = new.GetUID()
		clusterName = util.NameFromMeta(new.ObjectMeta)
		clusterError = new.Error
	}

	if clusterError != nil && eventType != spec.EventDelete {
		c.logger.Debugf("Skipping %q event for invalid cluster %q (reason: %v)", eventType, clusterName, clusterError)
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

	if err := c.clusterEventQueues[workerID].Add(clusterEvent); err != nil {
		c.logger.WithField("worker", workerID).Errorf("error when queueing cluster event: %v", clusterEvent)
	}
	c.logger.WithField("worker", workerID).Infof("%q of the %q cluster has been queued", eventType, clusterName)
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
	if pgOld.ResourceVersion == pgNew.ResourceVersion {
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
