package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/teams"
)

type Config struct {
	PodNamespace   string
	KubeClient     *kubernetes.Clientset
	RestClient     *rest.RESTClient
	EtcdClient     etcdclient.KeysAPI
	TeamsAPIClient *teams.TeamsAPI
}

type Controller struct {
	config      Config
	logger      *logrus.Entry
	events      chan *Event
	clusters    map[string]*cluster.Cluster
	stopChMap   map[string]chan struct{}
	waitCluster sync.WaitGroup

	postgresqlInformer cache.SharedIndexInformer
}

type Event struct {
	Type   string
	Object *spec.Postgresql
}

func New(cfg *Config) *Controller {
	return &Controller{
		config:    *cfg,
		logger:    logrus.WithField("pkg", "controller"),
		clusters:  make(map[string]*cluster.Cluster),
		stopChMap: make(map[string]chan struct{}),
	}
}

func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	c.initController()
	err := c.initEtcdClient()
	if err != nil {
		c.logger.Errorf("Can't get etcd client: %s", err)
		return
	}

	go c.watchTpr(stopCh)

	c.logger.Info("Started working in background")
}

func (c *Controller) watchTpr(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)

	<-stopCh
}

func (c *Controller) createTPR() error {
	TPRName := fmt.Sprintf("%s.%s", constants.TPRName, constants.TPRVendor)
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			Name: TPRName,
			//PodNamespace: c.config.PodNamespace, //ThirdPartyResources are cluster-wide
		},
		Versions: []v1beta1extensions.APIVersion{
			{Name: constants.TPRApiVersion},
		},
		Description: constants.TPRDescription,
	}

	_, err := c.config.KubeClient.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)
	if err != nil {
		if !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			return err
		} else {
			c.logger.Infof("ThirdPartyResource '%s' is already registered", TPRName)
		}
	} else {
		c.logger.Infof("ThirdPartyResource '%s' has been registered", TPRName)
	}

	restClient := c.config.RestClient

	return k8sutil.WaitTPRReady(restClient, constants.TPRReadyWaitInterval, constants.TPRReadyWaitTimeout, c.config.PodNamespace)
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ControllerNamespace: c.config.PodNamespace,
		KubeClient:          c.config.KubeClient,
		RestClient:          c.config.RestClient,
		EtcdClient:          c.config.EtcdClient,
		TeamsAPIClient:      c.config.TeamsAPIClient,
	}
}

func (c *Controller) initController() {
	err := c.createTPR()
	if err != nil {
		c.logger.Fatalf("Can't register ThirdPartyResource: %s", err)
	}

	c.postgresqlInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(c.config.RestClient, constants.ResourceName, v1.NamespaceAll, fields.Everything()),
		&spec.Postgresql{},
		constants.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.postgresqlInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.clusterAdd,
		UpdateFunc: c.clusterUpdate,
		DeleteFunc: c.clusterDelete,
	})
}

func (c *Controller) clusterAdd(obj interface{}) {
	pg := obj.(*spec.Postgresql)

	//TODO: why do we need to have this check
	if pg.Spec == nil {
		return
	}

	clusterName := (*pg).Metadata.Name

	cl := cluster.New(c.makeClusterConfig(), pg)
	err := cl.Create()
	if err != nil {
		c.logger.Errorf("Can't create cluster: %s", err)
		return
	}
	c.stopChMap[clusterName] = make(chan struct{})
	c.clusters[clusterName] = cl

	c.logger.Infof("Postgresql cluster '%s' has been created", util.FullObjectNameFromMeta((*pg).Metadata))
}

func (c *Controller) clusterUpdate(prev, cur interface{}) {
	pgPrev := prev.(*spec.Postgresql)
	pgCur := cur.(*spec.Postgresql)

	if pgPrev.Spec == nil || pgCur.Spec == nil {
		return
	}
	if pgPrev.Metadata.ResourceVersion == pgCur.Metadata.ResourceVersion {
		return
	}

	c.logger.Infof("Update: %+v -> %+v", *pgPrev, *pgCur)
}

func (c *Controller) clusterDelete(obj interface{}) {
	pg := obj.(*spec.Postgresql)
	if pg.Spec == nil {
		return
	}
	clusterName := (*pg).Metadata.Name

	cluster := cluster.New(c.makeClusterConfig(), pg)
	err := cluster.Delete()
	if err != nil {
		c.logger.Errorf("Can't delete cluster '%s': %s", util.FullObjectNameFromMeta((*pg).Metadata), err)
		return
	}

	close(c.stopChMap[clusterName])
	delete(c.clusters, clusterName)

	c.logger.Infof("Cluster has been deleted: '%s'", util.FullObjectNameFromMeta((*pg).Metadata))
}
