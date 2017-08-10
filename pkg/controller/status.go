package controller

import (
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

func (c *Controller) GetStatus() interface{} {
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

	return cl.Walk()
}

func (c *Controller) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (c *Controller) Fire(e *logrus.Entry) error {
	var clusterName spec.NamespacedName

	if v, ok := e.Data["cluster-name"]; !ok {
		return nil
	} else {
		clusterName = v.(spec.NamespacedName)
	}

	var workerId *uint32
	if v, ok := e.Data["worker"]; ok {
		id := v.(uint32)
		workerId = &id
	}

	c.clustersMu.RLock()
	rl, ok := c.clusterLogs[clusterName]
	c.clustersMu.RUnlock()
	if !ok {
		return nil
	}

	rl.Insert(e.Level, e.Time, workerId, clusterName, e.Message)

	return nil
}
