package controller

import (
	"sync/atomic"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

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

type clusterStatus struct {
	Team           string
	Cluster        string
	MasterService  *v1.Service
	ReplicaService *v1.Service
	Endpoint       *v1.Endpoints
	StatefulSet    *v1beta1.StatefulSet

	Config cluster.Config
	Status spec.PostgresStatus
	Spec   spec.PostgresSpec
	Error  error
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

	return clusterStatus{
		Config:  cl.Config,
		Cluster: cl.Spec.ClusterName,
		Team:    cl.Spec.TeamID,
		Status:  cl.Status,
		Spec:    cl.Spec,

		MasterService:  cl.GetServiceMaster(),
		ReplicaService: cl.GetServiceReplica(),
		Endpoint:       cl.GetEndpoint(),
		StatefulSet:    cl.GetStatefulSet(),

		Error: cl.Error,
	}
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
		resp = append(resp,
			clusterStatus{
				Config:  cl.Config,
				Cluster: cl.Spec.ClusterName,
				Team:    cl.Spec.TeamID,
				Status:  cl.Status,
				Spec:    cl.Spec,

				MasterService:  cl.GetServiceMaster(),
				ReplicaService: cl.GetServiceReplica(),
				Endpoint:       cl.GetEndpoint(),
				StatefulSet:    cl.GetStatefulSet(),

				Error: cl.Error,
			})
	}

	return resp
}

func (c *Controller) Status() interface{} {
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
