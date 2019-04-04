package controller

import (
	"fmt"
	"sort"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"github.com/zalando/postgres-operator/pkg/cluster"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"k8s.io/apimachinery/pkg/types"
)

// ClusterStatus provides status of the cluster
func (c *Controller) ClusterStatus(team, namespace, cluster string) (*cluster.ClusterStatus, error) {

	clusterName := spec.NamespacedName{
		Namespace: namespace,
		Name:      team + "-" + cluster,
	}

	c.clustersMu.RLock()
	cl, ok := c.clusters[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("could not find cluster")
	}

	status := cl.GetStatus()
	status.Worker = c.clusterWorkerID(clusterName)

	return status, nil
}

// ClusterDatabasesMap returns for each cluster the list of databases running there
func (c *Controller) ClusterDatabasesMap() map[string][]string {

	m := make(map[string][]string)

	// avoid modifying the cluster list while we are fetching each one of them.
	c.clustersMu.RLock()
	defer c.clustersMu.RUnlock()
	for _, cluster := range c.clusters {
		// GetSpec holds the specMu lock of a cluster
		if spec, err := cluster.GetSpec(); err == nil {
			for database := range spec.Spec.Databases {
				m[cluster.Name] = append(m[cluster.Name], database)
			}
			sort.Strings(m[cluster.Name])
		} else {
			c.logger.Warningf("could not get the list of databases for cluster %q: %v", cluster.Name, err)
		}
	}

	return m
}

// TeamClusterList returns team-clusters map
func (c *Controller) TeamClusterList() map[string][]spec.NamespacedName {
	return c.teamClusters
}

// GetConfig returns controller config
func (c *Controller) GetConfig() *spec.ControllerConfig {
	return &c.config
}

// GetOperatorConfig returns operator config
func (c *Controller) GetOperatorConfig() *config.Config {
	return c.opConfig
}

// GetStatus dumps current config and status of the controller
func (c *Controller) GetStatus() *spec.ControllerStatus {
	c.clustersMu.RLock()
	clustersCnt := len(c.clusters)
	c.clustersMu.RUnlock()

	queueSizes := make(map[int]int, c.opConfig.Workers)
	for workerID, queue := range c.clusterEventQueues {
		queueSizes[workerID] = len(queue.ListKeys())
	}

	return &spec.ControllerStatus{
		LastSyncTime:    atomic.LoadInt64(&c.lastClusterSyncTime),
		Clusters:        clustersCnt,
		WorkerQueueSize: queueSizes,
	}
}

// ClusterLogs dumps cluster ring logs
func (c *Controller) ClusterLogs(team, namespace, name string) ([]*spec.LogEntry, error) {

	clusterName := spec.NamespacedName{
		Namespace: namespace,
		Name:      team + "-" + name,
	}

	c.clustersMu.RLock()
	cl, ok := c.clusterLogs[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("could not find cluster")
	}

	res := make([]*spec.LogEntry, 0)
	for _, e := range cl.Walk() {
		logEntry := e.(*spec.LogEntry)
		logEntry.ClusterName = nil

		res = append(res, logEntry)
	}

	return res, nil
}

// WorkerLogs dumps logs of the worker
func (c *Controller) WorkerLogs(workerID uint32) ([]*spec.LogEntry, error) {
	lg, ok := c.workerLogs[workerID]
	if !ok {
		return nil, fmt.Errorf("could not find worker")
	}

	res := make([]*spec.LogEntry, 0)
	for _, e := range lg.Walk() {
		logEntry := e.(*spec.LogEntry)
		logEntry.Worker = nil

		res = append(res, logEntry)
	}

	return res, nil
}

// Levels returns logrus levels for which hook must fire
func (c *Controller) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire is a logrus hook
func (c *Controller) Fire(e *logrus.Entry) error {
	var clusterName spec.NamespacedName

	v, ok := e.Data["cluster-name"]
	if !ok {
		return nil
	}
	clusterName = v.(spec.NamespacedName)
	c.clustersMu.RLock()
	clusterRingLog, ok := c.clusterLogs[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil
	}

	logEntry := &spec.LogEntry{
		Time:        e.Time,
		Level:       e.Level,
		ClusterName: &clusterName,
		Message:     e.Message,
	}

	if v, hasWorker := e.Data["worker"]; hasWorker {
		id := v.(uint32)

		logEntry.Worker = &id
	}
	clusterRingLog.Insert(logEntry)

	if logEntry.Worker == nil {
		return nil
	}
	c.workerLogs[*logEntry.Worker].Insert(logEntry) // workerLogs map is immutable. No need to lock it

	return nil
}

// ListQueue dumps cluster event queue of the provided worker
func (c *Controller) ListQueue(workerID uint32) (*spec.QueueDump, error) {
	if workerID >= uint32(len(c.clusterEventQueues)) {
		return nil, fmt.Errorf("could not find worker")
	}

	q := c.clusterEventQueues[workerID]
	return &spec.QueueDump{
		Keys: q.ListKeys(),
		List: q.List(),
	}, nil
}

// GetWorkersCnt returns number of the workers
func (c *Controller) GetWorkersCnt() uint32 {
	return c.opConfig.Workers
}

//WorkerStatus provides status of the worker
func (c *Controller) WorkerStatus(workerID uint32) (*cluster.WorkerStatus, error) {
	obj, ok := c.curWorkerCluster.Load(workerID)
	if !ok || obj == nil {
		return nil, nil
	}

	cl, ok := obj.(*cluster.Cluster)
	if !ok {
		return nil, fmt.Errorf("could not cast to Cluster struct")
	}

	return &cluster.WorkerStatus{
		CurrentCluster: types.NamespacedName(util.NameFromMeta(cl.ObjectMeta)),
		CurrentProcess: cl.GetCurrentProcess(),
	}, nil
}

// ClusterHistory dumps history of cluster changes
func (c *Controller) ClusterHistory(team, namespace, name string) ([]*spec.Diff, error) {

	clusterName := spec.NamespacedName{
		Namespace: namespace,
		Name:      team + "-" + name,
	}

	c.clustersMu.RLock()
	cl, ok := c.clusterHistory[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("could not find cluster")
	}

	res := make([]*spec.Diff, 0)
	for _, e := range cl.Walk() {
		res = append(res, e.(*spec.Diff))
	}

	return res, nil
}
