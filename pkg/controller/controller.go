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
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
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
	clusters    map[spec.ClusterName]*cluster.Cluster
	stopChMap   map[spec.ClusterName]chan struct{}
	waitCluster sync.WaitGroup

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer

	podCh chan spec.PodEvent
}

func New(cfg *Config) *Controller {
	return &Controller{
		config:    *cfg,
		logger:    logrus.WithField("pkg", "controller"),
		clusters:  make(map[spec.ClusterName]*cluster.Cluster),
		stopChMap: make(map[spec.ClusterName]chan struct{}),
		podCh:     make(chan spec.PodEvent),
	}
}

func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	c.initController()
	if err := c.initEtcdClient(); err != nil {
		c.logger.Errorf("Can't get etcd client: %s", err)
		return
	}

	c.logger.Infof("'%s' namespace will be watched", c.config.PodNamespace)
	go c.runInformers(stopCh)

	c.logger.Info("Started working in background")
}

func (c *Controller) initController() {
	if err := c.createTPR(); err != nil {
		c.logger.Fatalf("Can't register ThirdPartyResource: %s", err)
	}

	c.config.TeamsAPIClient.RefreshTokenAction = c.getOAuthToken

	// Postgresqls
	clusterLw := &cache.ListWatch{
		ListFunc:  c.clusterListFunc,
		WatchFunc: c.clusterWatchFunc,
	}
	c.postgresqlInformer = cache.NewSharedIndexInformer(
		clusterLw,
		&spec.Postgresql{},
		constants.ResyncPeriodTPR,
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
		constants.ResyncPeriodPod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.podAdd,
		UpdateFunc: c.podUpdate,
		DeleteFunc: c.podDelete,
	})
}

func (c *Controller) runInformers(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)
	go c.podInformer.Run(stopCh)
	go c.podEventsDispatcher(stopCh)

	<-stopCh
}
