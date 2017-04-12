package controller

import (
	"sync"

	"github.com/Sirupsen/logrus"
	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/config"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/teams"
)

type Config struct {
	PodNamespace        string
	KubeClient          *kubernetes.Clientset
	RestClient          *rest.RESTClient
	EtcdClient          etcdclient.KeysAPI
	TeamsAPIClient      *teams.TeamsAPI
	InfrastructureRoles map[string]spec.PgUser
}

type Controller struct {
	Config
	opConfig    *config.Config
	logger      *logrus.Entry
	clusters    map[spec.ClusterName]*cluster.Cluster
	stopChMap   map[spec.ClusterName]chan struct{}
	waitCluster sync.WaitGroup

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer

	podCh chan spec.PodEvent
}

func New(controllerConfig *Config, operatorConfig *config.Config) *Controller {
	logger := logrus.New()

	if operatorConfig.DebugLogging {
		logger.Level = logrus.DebugLevel
	}

	controllerConfig.TeamsAPIClient = teams.NewTeamsAPI(operatorConfig.TeamsAPIUrl, logger)
	return &Controller{
		Config:    *controllerConfig,
		opConfig:  operatorConfig,
		logger:    logger.WithField("pkg", "controller"),
		clusters:  make(map[spec.ClusterName]*cluster.Cluster),
		stopChMap: make(map[spec.ClusterName]chan struct{}),
		podCh:     make(chan spec.PodEvent),
	}
}

func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	c.initController()

	c.logger.Infof("'%s' namespace will be watched", c.PodNamespace)
	go c.runInformers(stopCh)

	c.logger.Info("Started working in background")
}

func (c *Controller) initController() {
	if err := c.createTPR(); err != nil {
		c.logger.Fatalf("Can't register ThirdPartyResource: %s", err)
	}

	c.TeamsAPIClient.RefreshTokenAction = c.getOAuthToken
	if infraRoles, err := c.getInfrastructureRoles(); err != nil {
		c.logger.Warningf("Can't get infrastructure roles: %s", err)
	} else {
		c.InfrastructureRoles = infraRoles
	}

	// Postgresqls
	clusterLw := &cache.ListWatch{
		ListFunc:  c.clusterListFunc,
		WatchFunc: c.clusterWatchFunc,
	}
	c.postgresqlInformer = cache.NewSharedIndexInformer(
		clusterLw,
		&spec.Postgresql{},
		c.opConfig.ResyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.postgresqlInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.postgresqlAdd,
		UpdateFunc: c.postgresqlUpdate,
		DeleteFunc: c.postgresqlDelete,
	})

	// Pods
	podLw := &cache.ListWatch{
		ListFunc:  c.podListFunc,
		WatchFunc: c.podWatchFunc,
	}

	c.podInformer = cache.NewSharedIndexInformer(
		podLw,
		&v1.Pod{},
		c.opConfig.ResyncPeriodPod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.podAdd,
		UpdateFunc: c.podUpdate,
		DeleteFunc: c.podDelete,
	})

	if err := c.initEtcdClient(c.opConfig.EtcdHost); err != nil {
		c.logger.Fatalf("Can't get etcd client: %s", err)
	}
}

func (c *Controller) runInformers(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)
	go c.podInformer.Run(stopCh)
	go c.podEventsDispatcher(stopCh)

	<-stopCh
}
