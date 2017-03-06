package controller

import (
	"fmt"
	"reflect"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/watch"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

func (c *Controller) clusterListFunc(options api.ListOptions) (runtime.Object, error) {
	c.logger.Info("Getting list of currently running clusters")
	object, err := c.config.RestClient.Get().
		Namespace(c.config.PodNamespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything()).
		Do().
		Get()

	if err != nil {
		return nil, fmt.Errorf("Can't get list of postgresql objects: %s", err)
	}

	objList, err := meta.ExtractList(object)
	if err != nil {
		return nil, fmt.Errorf("Can't extract list of postgresql objects: %s", err)
	}

	clusterConfig := c.makeClusterConfig()
	for _, obj := range objList {
		pg, ok := obj.(*spec.Postgresql)
		if !ok {
			return nil, fmt.Errorf("Can't cast object to postgresql")
		}
		clusterName := spec.ClusterName{
			Namespace: pg.Metadata.Namespace,
			Name:      pg.Metadata.Name,
		}

		cl := cluster.New(clusterConfig, *pg)

		stopCh := make(chan struct{})
		c.stopChMap[clusterName] = stopCh
		c.clusters[clusterName] = cl
		cl.LoadResources()
		cl.ListResources()

		go cl.Run(stopCh)
	}
	if len(c.clusters) > 0 {
		c.logger.Infof("There are %d clusters currently running", len(c.clusters))
	} else {
		c.logger.Infof("No clusters running")
	}

	return object, err
}

func (c *Controller) clusterWatchFunc(options api.ListOptions) (watch.Interface, error) {
	return c.config.RestClient.Get().
		Prefix("watch").
		Namespace(c.config.PodNamespace).
		Resource(constants.ResourceName).
		VersionedParams(&options, api.ParameterCodec).
		FieldsSelectorParam(fields.Everything()).
		Watch()
}

func (c *Controller) postgresqlAdd(obj interface{}) {
	pg, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
		return
	}

	clusterName := spec.ClusterName{
		Namespace: pg.Metadata.Namespace,
		Name:      pg.Metadata.Name,
	}

	_, ok = c.clusters[clusterName]
	if ok {
		c.logger.Infof("Cluster '%s' already exists", clusterName)
		return
	}

	c.logger.Infof("Creation of a new Postgresql cluster '%s' started", clusterName)
	cl := cluster.New(c.makeClusterConfig(), *pg)
	cl.MustSetStatus(spec.ClusterStatusCreating)
	err := cl.Create()
	if err != nil {
		c.logger.Errorf("Can't create cluster: %s", err)
		cl.MustSetStatus(spec.ClusterStatusAddFailed)
		return
	}
	cl.MustSetStatus(spec.ClusterStatusRunning) //TODO: are you sure it's running?

	stopCh := make(chan struct{})
	c.stopChMap[clusterName] = stopCh
	c.clusters[clusterName] = cl
	go cl.Run(stopCh)

	c.logger.Infof("Postgresql cluster '%s' has been created", clusterName)
}

func (c *Controller) postgresqlUpdate(prev, cur interface{}) {
	pgPrev, ok := prev.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
	}
	pgNew, ok := cur.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
	}

	clusterName := spec.ClusterName{
		Namespace: pgNew.Metadata.Namespace,
		Name:      pgNew.Metadata.Name,
	}

	//TODO: Do not update cluster which is currently creating

	if pgPrev.Metadata.ResourceVersion == pgNew.Metadata.ResourceVersion {
		c.logger.Debugf("Skipping update with no resource version change")
		return
	}
	pgCluster := c.clusters[clusterName] // current

	if reflect.DeepEqual(pgPrev.Spec, pgNew.Spec) {
		c.logger.Infof("Skipping update with no spec change")
		return
	}

	c.logger.Infof("Cluster update: %s(version: %s) -> %s(version: %s)",
		util.NameFromMeta(pgPrev.Metadata), pgPrev.Metadata.ResourceVersion,
		util.NameFromMeta(pgNew.Metadata), pgNew.Metadata.ResourceVersion)

	rollingUpdate := pgCluster.NeedsRollingUpdate(pgNew)
	if rollingUpdate {
		c.logger.Infof("Pods need to be recreated")
	}

	pgCluster.MustSetStatus(spec.ClusterStatusUpdating)
	err := pgCluster.Update(pgNew, rollingUpdate)
	if err != nil {
		pgCluster.MustSetStatus(spec.ClusterStatusUpdateFailed)
		c.logger.Errorf("Can't update cluster: %s", err)
	} else {
		c.logger.Infof("Cluster has been updated")
	}
}

func (c *Controller) postgresqlDelete(obj interface{}) {
	pgCur, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
		return
	}
	clusterName := spec.ClusterName{
		Namespace: pgCur.Metadata.Namespace,
		Name:      pgCur.Metadata.Name,
	}
	pgCluster, ok := c.clusters[clusterName]
	if !ok {
		c.logger.Errorf("Unknown cluster: %s", clusterName)
		return
	}

	c.logger.Infof("Cluster delete: %s", util.NameFromMeta(pgCur.Metadata))
	err := pgCluster.Delete()
	if err != nil {
		c.logger.Errorf("Can't delete cluster '%s': %s", clusterName, err)
		return
	}

	close(c.stopChMap[clusterName])
	delete(c.clusters, clusterName)

	c.logger.Infof("Cluster '%s' has been successfully deleted", clusterName)
}
