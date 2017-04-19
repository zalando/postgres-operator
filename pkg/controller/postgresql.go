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
	object, err := c.RestClient.Get().
		Namespace(c.opConfig.Namespace).
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
		clusterName := util.NameFromMeta(pg.Metadata)
		cl := cluster.New(clusterConfig, *pg, c.logger.Logger)

		stopCh := make(chan struct{})
		c.stopChMap[clusterName] = stopCh
		c.clusters[clusterName] = cl
		cl.LoadResources()
		go cl.Run(stopCh)

		cl.ListResources()
		cl.SyncCluster()
	}
	if len(c.clusters) > 0 {
		c.logger.Infof("There are %d clusters currently running", len(c.clusters))
	} else {
		c.logger.Infof("No clusters running")
	}

	return object, err
}

func (c *Controller) clusterWatchFunc(options api.ListOptions) (watch.Interface, error) {
	return c.RestClient.Get().
		Prefix("watch").
		Namespace(c.opConfig.Namespace).
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

	clusterName := util.NameFromMeta(pg.Metadata)

	_, ok = c.clusters[clusterName]
	if ok {
		c.logger.Infof("Cluster '%s' already exists", clusterName)
		return
	}

	c.logger.Infof("Creation of a new Postgresql cluster '%s' started", clusterName)
	cl := cluster.New(c.makeClusterConfig(), *pg, c.logger.Logger)

	c.clusters[clusterName] = cl
	stopCh := make(chan struct{})
	c.stopChMap[clusterName] = stopCh

	cl.SetStatus(spec.ClusterStatusCreating)
	if err := cl.Create(); err != nil {
		c.logger.Errorf("Can't create cluster: %s", err)
		cl.SetStatus(spec.ClusterStatusAddFailed)
		return
	}
	cl.SetStatus(spec.ClusterStatusRunning) //TODO: are you sure it's running?
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

	clusterName := util.NameFromMeta(pgNew.Metadata)

	//TODO: Do not update cluster which is currently creating
	if pgPrev.Metadata.ResourceVersion == pgNew.Metadata.ResourceVersion {
		c.logger.Infof("Skipping update with no resource version change")
		return
	}
	pgCluster := c.clusters[clusterName] // current
	if reflect.DeepEqual(pgPrev.Spec, pgNew.Spec) {
		c.logger.Infof("Skipping update with no spec change")
		return
	}

	pgCluster.SetStatus(spec.ClusterStatusUpdating)
	if err := pgCluster.Update(pgNew); err != nil {
		pgCluster.SetStatus(spec.ClusterStatusUpdateFailed)
		c.logger.Errorf("Can't update cluster: %s", err)
	} else {
		c.logger.Infof("Cluster has been updated")
	}
	pgCluster.SetStatus(spec.ClusterStatusRunning)
}

func (c *Controller) postgresqlDelete(obj interface{}) {
	pgCur, ok := obj.(*spec.Postgresql)
	if !ok {
		c.logger.Errorf("Can't cast to postgresql spec")
		return
	}
	clusterName := util.NameFromMeta(pgCur.Metadata)
	pgCluster, ok := c.clusters[clusterName]
	if !ok {
		c.logger.Errorf("Unknown cluster: %s", clusterName)
		return
	}

	c.logger.Infof("Starting deletion of the '%s' cluster", util.NameFromMeta(pgCur.Metadata))
	if err := pgCluster.Delete(); err != nil {
		c.logger.Errorf("Can't delete cluster '%s': %s", clusterName, err)
		return
	}

	close(c.stopChMap[clusterName])
	delete(c.clusters, clusterName)

	c.logger.Infof("Cluster '%s' has been successfully deleted", clusterName)
}
