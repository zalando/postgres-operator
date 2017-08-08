package controller

import (
	"sync/atomic"

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
