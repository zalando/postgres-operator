package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
)

type Config struct {
	RestConfig          *rest.Config
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
	waitCluster sync.WaitGroup

	clustersMu sync.RWMutex
	clusters   map[spec.NamespacedName]*cluster.Cluster
	stopChs    map[spec.NamespacedName]chan struct{}

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	podCh              chan spec.PodEvent

	clusterEventQueues []*cache.FIFO
}

func New(controllerConfig *Config, operatorConfig *config.Config) *Controller {
	logger := logrus.New()

	if operatorConfig.DebugLogging {
		logger.Level = logrus.DebugLevel
	}

	controllerConfig.TeamsAPIClient = teams.NewTeamsAPI(operatorConfig.TeamsAPIUrl, logger)
	return &Controller{
		Config:   *controllerConfig,
		opConfig: operatorConfig,
		logger:   logger.WithField("pkg", "controller"),
		clusters: make(map[spec.NamespacedName]*cluster.Cluster),
		stopChs:  make(map[spec.NamespacedName]chan struct{}),
		podCh:    make(chan spec.PodEvent),
	}
}

func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	c.initController()

	go c.runInformers(stopCh)

	for i := range c.clusterEventQueues {
		go c.processClusterEventsQueue(i)
	}

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

	c.clusterEventQueues = make([]*cache.FIFO, c.opConfig.Workers)
	for i := range c.clusterEventQueues {
		c.clusterEventQueues[i] = cache.NewFIFO(func(obj interface{}) (string, error) {
			e, ok := obj.(spec.ClusterEvent)
			if !ok {
				return "", fmt.Errorf("Can't cast to ClusterEvent")
			}

			return fmt.Sprintf("%s-%s", e.EventType, e.UID), nil
		})
	}
}

func (c *Controller) runInformers(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)
	go c.podInformer.Run(stopCh)
	go c.podEventsDispatcher(stopCh)

	<-stopCh
}
