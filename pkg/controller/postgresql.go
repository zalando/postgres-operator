package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/ringlog"
)

func (c *Controller) clusterResync(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(c.opConfig.ResyncPeriod)

	for {
		select {
		case <-ticker.C:
			if _, err := c.clusterListFunc(metav1.ListOptions{ResourceVersion: "0"}); err != nil {
				c.logger.Errorf("could not list clusters: %v", err)
			}
		case <-stopCh:
			return
		}
	}
}

func (c *Controller) clusterListFunc(options metav1.ListOptions) (runtime.Object, error) {
	var list spec.PostgresqlList
	var activeClustersCnt, failedClustersCnt int

	req := c.KubeClient.CRDREST.
		Get().
		Namespace(c.opConfig.WatchedNamespace).
		Resource(constants.CRDResource).
		VersionedParams(&options, metav1.ParameterCodec)

	b, err := req.DoRaw()
	if err != nil {
		c.logger.Errorf("could not get the list of postgresql CRD objects: %v", err)
		return nil, err
	}
	if err = json.Unmarshal(b, &list); err != nil {
		c.logger.Warningf("could not unmarshal list of clusters: %v", err)
	}

	if time.Now().Unix()-atomic.LoadInt64(&c.lastClusterSyncTime) <= int64(c.opConfig.ResyncPeriod.Seconds()) {
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
			c.logger.Infof("there are no clusters running. %d are in the failed state", failedClustersCnt)
		} else if failedClustersCnt == 0 && activeClustersCnt > 0 {
			c.logger.Infof("there are %d clusters running", activeClustersCnt)
		} else {
			c.logger.Infof("there are %d clusters running and %d are in the failed state", activeClustersCnt, failedClustersCnt)
		}
	} else {
		c.logger.Infof("no clusters running")
	}

	atomic.StoreInt64(&c.lastClusterSyncTime, time.Now().Unix())

	return &list, err
}

type crdDecoder struct {
	dec   *json.Decoder
	close func() error
}

func (d *crdDecoder) Close() {
	d.close()
}

func (d *crdDecoder) Decode() (action watch.EventType, object runtime.Object, err error) {
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
	r, err := c.KubeClient.CRDREST.
		Get().
		Namespace(c.opConfig.WatchedNamespace).
		Resource(constants.CRDResource).
		VersionedParams(&options, metav1.ParameterCodec).
		FieldsSelectorParam(nil).
		Stream()

	if err != nil {
		return nil, err
	}

	return watch.NewStreamWatcher(&crdDecoder{
		dec:   json.NewDecoder(r),
		close: r.Close,
	}), nil
}

func (c *Controller) addCluster(lg *logrus.Entry, clusterName spec.NamespacedName, pgSpec *spec.Postgresql) *cluster.Cluster {
	cl := cluster.New(c.makeClusterConfig(), c.KubeClient, *pgSpec, lg)
	cl.Run(c.stopCh)
	teamName := strings.ToLower(cl.Spec.TeamID)

	defer c.clustersMu.Unlock()
	c.clustersMu.Lock()

	c.teamClusters[teamName] = append(c.teamClusters[teamName], clusterName)
	c.clusters[clusterName] = cl
	c.clusterLogs[clusterName] = ringlog.New(c.opConfig.RingLogLines)
	c.clusterHistory[clusterName] = ringlog.New(c.opConfig.ClusterHistoryEntries)

	return cl
}

func (c *Controller) processEvent(event spec.ClusterEvent) {
	var clusterName spec.NamespacedName
	var clHistory ringlog.RingLogger

	lg := c.logger.WithField("worker", event.WorkerID)

	if event.EventType == spec.EventAdd || event.EventType == spec.EventSync {
		clusterName = util.NameFromMeta(event.NewSpec.ObjectMeta)
	} else {
		clusterName = util.NameFromMeta(event.OldSpec.ObjectMeta)
	}
	lg = lg.WithField("cluster-name", clusterName)

	c.clustersMu.RLock()
	cl, clusterFound := c.clusters[clusterName]
	if clusterFound {
		clHistory = c.clusterHistory[clusterName]
	}
	c.clustersMu.RUnlock()

	defer c.curWorkerCluster.Store(event.WorkerID, nil)

	switch event.EventType {
	case spec.EventAdd:
		if clusterFound {
			lg.Debugf("cluster already exists")
			return
		}

		lg.Infof("creation of the cluster started")

		cl = c.addCluster(lg, clusterName, event.NewSpec)

		c.curWorkerCluster.Store(event.WorkerID, cl)

		if err := cl.Create(); err != nil {
			cl.Error = fmt.Errorf("could not create cluster: %v", err)
			lg.Error(cl.Error)

			return
		}

		lg.Infoln("cluster has been created")
	case spec.EventUpdate:
		lg.Infoln("update of the cluster started")

		if !clusterFound {
			lg.Warningln("cluster does not exist")
			return
		}
		c.curWorkerCluster.Store(event.WorkerID, cl)
		if err := cl.Update(event.OldSpec, event.NewSpec); err != nil {
			cl.Error = fmt.Errorf("could not update cluster: %v", err)
			lg.Error(cl.Error)

			return
		}
		cl.Error = nil
		lg.Infoln("cluster has been updated")

		clHistory.Insert(&spec.Diff{
			EventTime:   event.EventTime,
			ProcessTime: time.Now(),
			Diff:        util.Diff(event.OldSpec, event.NewSpec),
		})
	case spec.EventDelete:
		if !clusterFound {
			lg.Errorf("unknown cluster: %q", clusterName)
			return
		}
		lg.Infoln("deletion of the cluster started")

		teamName := strings.ToLower(cl.Spec.TeamID)

		c.curWorkerCluster.Store(event.WorkerID, cl)
		if err := cl.Delete(); err != nil {
			lg.Errorf("could not delete cluster: %v", err)
		}

		func() {
			defer c.clustersMu.Unlock()
			c.clustersMu.Lock()

			delete(c.clusters, clusterName)
			delete(c.clusterLogs, clusterName)
			delete(c.clusterHistory, clusterName)
			for i, val := range c.teamClusters[teamName] {
				if val == clusterName {
					copy(c.teamClusters[teamName][i:], c.teamClusters[teamName][i+1:])
					c.teamClusters[teamName][len(c.teamClusters[teamName])-1] = spec.NamespacedName{}
					c.teamClusters[teamName] = c.teamClusters[teamName][:len(c.teamClusters[teamName])-1]
					break
				}
			}
		}()

		lg.Infof("cluster has been deleted")
	case spec.EventSync:
		lg.Infof("syncing of the cluster started")

		// no race condition because a cluster is always processed by single worker
		if !clusterFound {
			cl = c.addCluster(lg, clusterName, event.NewSpec)
		}

		c.curWorkerCluster.Store(event.WorkerID, cl)
		if err := cl.Sync(event.NewSpec); err != nil {
			cl.Error = fmt.Errorf("could not sync cluster: %v", err)
			lg.Error(cl.Error)
			return
		}
		cl.Error = nil

		lg.Infof("cluster has been synced")
	}
}

func (c *Controller) processClusterEventsQueue(idx int, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	go func() {
		<-stopCh
		c.clusterEventQueues[idx].Close()
	}()

	for {
		obj, err := c.clusterEventQueues[idx].Pop(cache.PopProcessFunc(func(interface{}) error { return nil }))
		if err != nil {
			if err == cache.FIFOClosedError {
				return
			}
			c.logger.Errorf("error when processing cluster events queue: %v", err)
			continue
		}
		event, ok := obj.(spec.ClusterEvent)
		if !ok {
			c.logger.Errorf("could not cast to ClusterEvent")
		}

		c.processEvent(event)
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
		c.logger.
			WithField("cluster-name", clusterName).
			Debugf("skipping %q event for the invalid cluster: %v", eventType, clusterError)
		return
	}

	workerID := c.clusterWorkerID(clusterName)
	clusterEvent := spec.ClusterEvent{
		EventTime: time.Now(),
		EventType: eventType,
		UID:       uid,
		OldSpec:   old,
		NewSpec:   new,
		WorkerID:  workerID,
	}

	lg := c.logger.WithField("worker", workerID).WithField("cluster-name", clusterName)
	if err := c.clusterEventQueues[workerID].Add(clusterEvent); err != nil {
		lg.Errorf("error while queueing cluster event: %v", clusterEvent)
	}
	lg.Infof("%q event has been queued", eventType)

	if eventType != spec.EventDelete {
		return
	}

	for _, evType := range []spec.EventType{spec.EventAdd, spec.EventSync, spec.EventUpdate} {
		obj, exists, err := c.clusterEventQueues[workerID].GetByKey(queueClusterKey(evType, uid))
		if err != nil {
			lg.Warningf("could not get event from the queue: %v", err)
			continue
		}

		if !exists {
			continue
		}

		err = c.clusterEventQueues[workerID].Delete(obj)
		if err != nil {
			lg.Warningf("could not delete event from the queue: %v", err)
		} else {
			lg.Debugf("event %q has been discarded for the cluster", evType)
		}
	}
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
