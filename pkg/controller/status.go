package controller

import (
	"fmt"
	"sync/atomic"

	"github.com/Sirupsen/logrus"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
)

type controllerStatus struct {
	ControllerConfig Config
	OperatorConfig   config.Config
	LastSyncTime     int64
	Clusters         int
}

// ClusterStatus provides status of the cluster
func (c *Controller) ClusterStatus(team, cluster string) interface{} {
	clusterName := spec.NamespacedName{
		Namespace: c.opConfig.Namespace,
		Name:      team + "-" + cluster,
	}

	c.clustersMu.RLock()
	cl, ok := c.clusters[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return struct{}{}
	}

	return cl.GetStatus()
}

// TeamClustersStatus dumps logs of all the team clusters
func (c *Controller) TeamClustersStatus(team string) []interface{} {
	var clusters []*cluster.Cluster
	c.clustersMu.RLock()

	clusterNames, ok := c.teamClusters[team]
	if !ok {
		c.clustersMu.RUnlock()
		return nil
	}
	for _, cl := range clusterNames {
		clusters = append(clusters, c.clusters[cl])
	}
	c.clustersMu.RUnlock()

	var resp []interface{}
	for _, cl := range clusters {
		resp = append(resp, cl.GetStatus())
	}

	return resp
}

// ControllerStatus dumps current config and status of the controller
func (c *Controller) ControllerStatus() interface{} {
	c.clustersMu.RLock()
	clustersCnt := len(c.clusters)
	c.clustersMu.RUnlock()

	return controllerStatus{
		ControllerConfig: c.config,
		OperatorConfig:   *c.opConfig,
		LastSyncTime:     atomic.LoadInt64(&c.lastClusterSyncTime),
		Clusters:         clustersCnt,
	}
}

// ClusterLogs dumps cluster ring logs
func (c *Controller) ClusterLogs(team, cluster string) interface{} {
	clusterName := spec.NamespacedName{
		Namespace: c.opConfig.Namespace,
		Name:      team + "-" + cluster,
	}

	c.clustersMu.RLock()
	cl, ok := c.clusterLogs[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil
	}

	res := make([]interface{}, 0)
	for _, e := range cl.Walk() {
		v := map[string]interface{}{
			"Time":    e.Time,
			"Level":   e.Level.String(),
			"Message": e.Message,
		}
		if e.Worker != nil {
			v["Worker"] = fmt.Sprintf("%d", *e.Worker)
		}

		res = append(res, v)
	}

	return res
}

// WorkerLogs dumps logs of the worker
func (c *Controller) WorkerLogs(workerID uint32) interface{} {
	lg, ok := c.workerLogs[workerID]
	if !ok {
		return nil
	}

	res := make([]interface{}, 0)
	for _, e := range lg.Walk() {
		v := map[string]interface{}{
			"Time":        e.Time,
			"ClusterName": e.ClusterName.String(),
			"Level":       e.Level.String(),
			"Message":     e.Message,
		}

		res = append(res, v)
	}

	return res
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

	var workerID *uint32
	if v, hasWorker := e.Data["worker"]; hasWorker {
		id := v.(uint32)
		workerID = &id
	}

	c.clustersMu.RLock()
	rl, ok := c.clusterLogs[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil
	}

	rl.Insert(e.Level, e.Time, workerID, clusterName, e.Message)
	if workerID == nil {
		return nil
	}

	c.workerLogs[*workerID]. // workerLogs map is immutable
					Insert(e.Level, e.Time, workerID, clusterName, e.Message)

	return nil
}

// ListQueue dumps cluster event queue of the provided worker
func (c *Controller) ListQueue(workerID uint32) interface{} {
	if workerID >= uint32(len(c.clusterEventQueues)) {
		return nil
	}

	q := c.clusterEventQueues[workerID]
	if q == nil {
		return nil
	}

	return map[string]interface{}{
		"Keys": q.ListKeys(),
		"List": q.List(),
	}
}
