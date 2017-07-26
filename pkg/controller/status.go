package controller

import (
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
)

type controllerStatus struct {
	ControllerConfig Config
	OperatorConfig   config.Config
	LastSyncTime     int64
	Clusters  int
}

type clusterStatus struct {
	Team    string
	Cluster string

	Config    cluster.Config
	Status    spec.PostgresStatus
	Resources cluster.KubeResources
	Spec      spec.PostgresSpec
}

func (c *Controller) ClusterStatus(team, cluster string) interface{} {
	clusterName := spec.NamespacedName{
		Namespace: c.opConfig.Namespace,
		Name:      team + "-" + cluster,
	}

	cl, ok := c.clusters[clusterName]
	if !ok {
		return struct{}{}
	}

	return clusterStatus{
		Config:    cl.Config,
		Cluster:   cl.Spec.ClusterName,
		Team:      cl.Spec.TeamID,
		Status:    cl.Status,
		Resources: cl.KubeResources,
		Spec:      cl.Spec,
	}
}

func (c *Controller) Status() interface{} {
	return controllerStatus{
		ControllerConfig: c.config,
		OperatorConfig:   *c.opConfig,
		LastSyncTime:     c.lastClusterSyncTime,
		Clusters:  len(c.clusters),
	}
}
