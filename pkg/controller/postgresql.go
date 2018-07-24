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
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
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

// TODO: make a separate function to be called from InitSharedInformers
// clusterListFunc obtains a list of all PostgreSQL clusters and runs sync when necessary
// NB: as this function is called directly by the informer, it needs to avoid acquiring locks
// on individual cluster structures. Therefore, it acts on the manifests obtained from Kubernetes
// and not on the internal state of the clusters.
func (c *Controller) clusterListFunc(options metav1.ListOptions) (runtime.Object, error) {
	var (
		list  spec.PostgresqlList
		event spec.EventType
	)

	req := c.KubeClient.CRDREST.
		Get().
		Namespace(c.opConfig.WatchedNamespace).
		Resource(constants.PostgresCRDResource).
		VersionedParams(&options, metav1.ParameterCodec)

	b, err := req.DoRaw()
	if err != nil {
		c.logger.Errorf("could not get the list of postgresql CRD objects: %v", err)
		return nil, err
	}
	if err = json.Unmarshal(b, &list); err != nil {
		c.logger.Warningf("could not unmarshal list of clusters: %v", err)
	}

	currentTime := time.Now().Unix()
	timeFromPreviousSync := currentTime - atomic.LoadInt64(&c.lastClusterSyncTime)
	timeFromPreviousRepair := currentTime - atomic.LoadInt64(&c.lastClusterRepairTime)
	if timeFromPreviousSync >= int64(c.opConfig.ResyncPeriod.Seconds()) {
		event = spec.EventSync
	} else if timeFromPreviousRepair >= int64(c.opConfig.RepairPeriod.Seconds()) {
		event = spec.EventRepair
	}
	if event != "" {
		c.queueEvents(&list, event)
	} else {
		c.logger.Infof("not enough time passed since the last sync (%s seconds) or repair (%s seconds)",
			timeFromPreviousSync, timeFromPreviousRepair)
	}
	return &list, err
}

// queueEvents queues a sync or repair event for every cluster with a valid manifest
func (c *Controller) queueEvents(list *spec.PostgresqlList, event spec.EventType) {
	var activeClustersCnt, failedClustersCnt, clustersToRepair int
	for i, pg := range list.Items {
		if pg.Error != nil {
			failedClustersCnt++
			continue
		}
		activeClustersCnt++
		// check if that cluster needs repair
		if event == spec.EventRepair {
			if pg.Status.Success() {
				continue
			} else {
				clustersToRepair++
			}
		}
		c.queueClusterEvent(nil, &list.Items[i], event)
	}
	if len(list.Items) > 0 {
		if failedClustersCnt > 0 && activeClustersCnt == 0 {
			c.logger.Infof("there are no clusters running. %d are in the failed state", failedClustersCnt)
		} else if failedClustersCnt == 0 && activeClustersCnt > 0 {
			c.logger.Infof("there are %d clusters running", activeClustersCnt)
		} else {
			c.logger.Infof("there are %d clusters running and %d are in the failed state", activeClustersCnt, failedClustersCnt)
		}
		if clustersToRepair > 0 {
			c.logger.Infof("%d clusters are scheduled for a repair scan", clustersToRepair)
		}
	} else {
		c.logger.Infof("no clusters running")
	}
	if event == spec.EventRepair || event == spec.EventSync {
		atomic.StoreInt64(&c.lastClusterRepairTime, time.Now().Unix())
		if event == spec.EventSync {
			atomic.StoreInt64(&c.lastClusterSyncTime, time.Now().Unix())
		}
	}
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
		Resource(constants.PostgresCRDResource).
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

	if event.EventType == spec.EventAdd || event.EventType == spec.EventSync || event.EventType == spec.EventRepair {
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

	if event.EventType == spec.EventRepair {
		runRepair, lastOperationStatus := cl.NeedsRepair()
		if !runRepair {
			lg.Debugf("Observed cluster status %s, repair is not required", lastOperationStatus)
			return
		}
		lg.Debugf("Observed cluster status %s, running sync scan to repair the cluster", lastOperationStatus)
		event.EventType = spec.EventSync
	}

	if event.EventType == spec.EventAdd || event.EventType == spec.EventUpdate || event.EventType == spec.EventSync {
		// handle deprecated parameters by possibly assigning their values to the new ones.
		if event.OldSpec != nil {
			c.mergeDeprecatedPostgreSQLSpecParameters(&event.OldSpec.Spec)
		}
		if event.NewSpec != nil {
			c.warnOnDeprecatedPostgreSQLSpecParameters(&event.NewSpec.Spec)
			c.mergeDeprecatedPostgreSQLSpecParameters(&event.NewSpec.Spec)
		}

		if err := c.submitRBACCredentials(event); err != nil {
			c.logger.Warnf("Pods and/or Patroni may misfunction due to the lack of permissions: %v", err)
		}

	}

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
		cl.Delete()

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

func (c *Controller) warnOnDeprecatedPostgreSQLSpecParameters(spec *spec.PostgresSpec) {

	deprecate := func(deprecated, replacement string) {
		c.logger.Warningf("Parameter %q is deprecated. Consider setting %q instead", deprecated, replacement)
	}

	noeffect := func(param string, explanation string) {
		c.logger.Warningf("Parameter %q takes no effect. %s", param, explanation)
	}

	if spec.UseLoadBalancer != nil {
		deprecate("useLoadBalancer", "enableMasterLoadBalancer")
	}
	if spec.ReplicaLoadBalancer != nil {
		deprecate("replicaLoadBalancer", "enableReplicaLoadBalancer")
	}

	if len(spec.MaintenanceWindows) > 0 {
		noeffect("maintenanceWindows", "Not implemented.")
	}

	if (spec.UseLoadBalancer != nil || spec.ReplicaLoadBalancer != nil) &&
		(spec.EnableReplicaLoadBalancer != nil || spec.EnableMasterLoadBalancer != nil) {
		c.logger.Warnf("Both old and new load balancer parameters are present in the manifest, ignoring old ones")
	}
}

// mergeDeprecatedPostgreSQLSpecParameters modifies the spec passed to the cluster by setting current parameter
// values from the obsolete ones. Note: while the spec that is modified is a copy made in queueClusterEvent, it is
// still a shallow copy, so be extra careful not to modify values pointer fields point to, but copy them instead.
func (c *Controller) mergeDeprecatedPostgreSQLSpecParameters(spec *spec.PostgresSpec) *spec.PostgresSpec {
	if (spec.UseLoadBalancer != nil || spec.ReplicaLoadBalancer != nil) &&
		(spec.EnableReplicaLoadBalancer == nil && spec.EnableMasterLoadBalancer == nil) {
		if spec.UseLoadBalancer != nil {
			spec.EnableMasterLoadBalancer = new(bool)
			*spec.EnableMasterLoadBalancer = *spec.UseLoadBalancer
		}
		if spec.ReplicaLoadBalancer != nil {
			spec.EnableReplicaLoadBalancer = new(bool)
			*spec.EnableReplicaLoadBalancer = *spec.ReplicaLoadBalancer
		}
	}
	spec.ReplicaLoadBalancer = nil
	spec.UseLoadBalancer = nil

	return spec
}

func (c *Controller) queueClusterEvent(informerOldSpec, informerNewSpec *spec.Postgresql, eventType spec.EventType) {
	var (
		uid          types.UID
		clusterName  spec.NamespacedName
		clusterError error
	)

	if informerOldSpec != nil { //update, delete
		uid = informerOldSpec.GetUID()
		clusterName = util.NameFromMeta(informerOldSpec.ObjectMeta)
		if eventType == spec.EventUpdate && informerNewSpec.Error == nil && informerOldSpec.Error != nil {
			eventType = spec.EventSync
			clusterError = informerNewSpec.Error
		} else {
			clusterError = informerOldSpec.Error
		}
	} else { //add, sync
		uid = informerNewSpec.GetUID()
		clusterName = util.NameFromMeta(informerNewSpec.ObjectMeta)
		clusterError = informerNewSpec.Error
	}

	if clusterError != nil && eventType != spec.EventDelete {
		c.logger.
			WithField("cluster-name", clusterName).
			Debugf("skipping %q event for the invalid cluster: %v", eventType, clusterError)
		return
	}

	// Don't pass the spec directly from the informer, since subsequent modifications of it would be reflected
	// in the informer internal state, making it incohherent with the actual Kubernetes object (and, as a side
	// effect, the modified state will be returned together with subsequent events).

	workerID := c.clusterWorkerID(clusterName)
	clusterEvent := spec.ClusterEvent{
		EventTime: time.Now(),
		EventType: eventType,
		UID:       uid,
		OldSpec:   informerOldSpec.Clone(),
		NewSpec:   informerNewSpec.Clone(),
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
	// A delete event discards all prior requests for that cluster.
	for _, evType := range []spec.EventType{spec.EventAdd, spec.EventSync, spec.EventUpdate, spec.EventRepair} {
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

/*
  Ensures the pod service account and role bindings exists in a namespace before a PG cluster is created there so that a user does not have to deploy these credentials manually.
  StatefulSets require the service account to create pods; Patroni requires relevant RBAC bindings to access endpoints.

  The operator does not sync accounts/role bindings after creation.
*/
func (c *Controller) submitRBACCredentials(event spec.ClusterEvent) error {

	namespace := event.NewSpec.GetNamespace()
	if _, ok := c.namespacesWithDefinedRBAC.Load(namespace); ok {
		return nil
	}

	if err := c.createPodServiceAccount(namespace); err != nil {
		return fmt.Errorf("could not create pod service account %v : %v", c.opConfig.PodServiceAccountName, err)
	}

	if err := c.createRoleBindings(namespace); err != nil {
		return fmt.Errorf("could not create role binding %v : %v", c.PodServiceAccountRoleBinding.Name, err)
	}

	c.namespacesWithDefinedRBAC.Store(namespace, true)
	return nil
}

func (c *Controller) createPodServiceAccount(namespace string) error {

	podServiceAccountName := c.opConfig.PodServiceAccountName
	_, err := c.KubeClient.ServiceAccounts(namespace).Get(podServiceAccountName, metav1.GetOptions{})
	if k8sutil.ResourceNotFound(err) {

		c.logger.Infof(fmt.Sprintf("creating pod service account in the namespace %v", namespace))

		// get a separate copy of service account
		// to prevent a race condition when setting a namespace for many clusters
		sa := *c.PodServiceAccount
		if _, err = c.KubeClient.ServiceAccounts(namespace).Create(&sa); err != nil {
			return fmt.Errorf("cannot deploy the pod service account %v defined in the config map to the %v namespace: %v", podServiceAccountName, namespace, err)
		}

		c.logger.Infof("successfully deployed the pod service account %v to the %v namespace", podServiceAccountName, namespace)
	} else if k8sutil.ResourceAlreadyExists(err) {
		return nil
	}

	return err
}

func (c *Controller) createRoleBindings(namespace string) error {

	podServiceAccountName := c.opConfig.PodServiceAccountName
	podServiceAccountRoleBindingName := c.PodServiceAccountRoleBinding.Name

	_, err := c.KubeClient.RoleBindings(namespace).Get(podServiceAccountRoleBindingName, metav1.GetOptions{})
	if k8sutil.ResourceNotFound(err) {

		c.logger.Infof("Creating the role binding %v in the namespace %v", podServiceAccountRoleBindingName, namespace)

		// get a separate copy of role binding
		// to prevent a race condition when setting a namespace for many clusters
		rb := *c.PodServiceAccountRoleBinding
		_, err = c.KubeClient.RoleBindings(namespace).Create(&rb)
		if err != nil {
			return fmt.Errorf("cannot bind the pod service account %q defined in the config map to the cluster role in the %q namespace: %v", podServiceAccountName, namespace, err)
		}

		c.logger.Infof("successfully deployed the role binding for the pod service account %q to the %q namespace", podServiceAccountName, namespace)

	} else if k8sutil.ResourceAlreadyExists(err) {
		return nil
	}

	return err
}
