package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
)

type Config struct {
	RestConfig          *rest.Config
	InfrastructureRoles map[string]spec.PgUser

	NoDatabaseAccess bool
	NoTeamsAPI       bool
	ConfigMapName    spec.NamespacedName
	Namespace        string
}

type Controller struct {
	Config
	opConfig *config.Config
	logger   *logrus.Entry

	KubeClient     *kubernetes.Clientset
	RestClient     rest.Interface
	TeamsAPIClient *teams.API

	clustersMu sync.RWMutex
	clusters   map[spec.NamespacedName]*cluster.Cluster
	stopChs    map[spec.NamespacedName]chan struct{}

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	podCh              chan spec.PodEvent

	clusterEventQueues []*cache.FIFO

	lastClusterSyncTime int64
}

func New(controllerConfig *Config) *Controller {
	configMapData := make(map[string]string)
	logger := logrus.New()

	client, err := k8sutil.KubernetesClient(controllerConfig.RestConfig)
	if err != nil {
		logger.Fatalf("couldn't create client: %v", err)
	}

	restClient, err := k8sutil.KubernetesRestClient(*controllerConfig.RestConfig)
	if err != nil {
		logger.Fatalf("couldn't create rest client: %v", err)
	}

	if controllerConfig.ConfigMapName != (spec.NamespacedName{}) {
		configMap, err := client.ConfigMaps(controllerConfig.ConfigMapName.Namespace).Get(controllerConfig.ConfigMapName.Name, meta_v1.GetOptions{})
		if err != nil {
			panic(err)
		}

		configMapData = configMap.Data
	} else {
		logger.Infoln("No ConfigMap specified. Loading default values")
	}

	if configMapData["namespace"] == "" { // Namespace in ConfigMap has priority over env var
		configMapData["namespace"] = controllerConfig.Namespace
	}
	if controllerConfig.NoDatabaseAccess {
		configMapData["enable_database_access"] = "false"
	}
	if controllerConfig.NoTeamsAPI {
		configMapData["enable_teams_api"] = "false"
	}
	operatorConfig := config.NewFromMap(configMapData)

	logger.Infof("Config: %s", operatorConfig.MustMarshal())

	if operatorConfig.DebugLogging {
		logger.Level = logrus.DebugLevel
	}

	return &Controller{
		Config:         *controllerConfig,
		opConfig:       operatorConfig,
		logger:         logger.WithField("pkg", "controller"),
		clusters:       make(map[spec.NamespacedName]*cluster.Cluster),
		stopChs:        make(map[spec.NamespacedName]chan struct{}),
		podCh:          make(chan spec.PodEvent),
		TeamsAPIClient: teams.NewTeamsAPI(operatorConfig.TeamsAPIUrl, logger),
		KubeClient:     client,
		RestClient:     restClient,
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
		c.logger.Fatalf("could not register ThirdPartyResource: %v", err)
	}

	if infraRoles, err := c.getInfrastructureRoles(); err != nil {
		c.logger.Warningf("could not get infrastructure roles: %v", err)
	} else {
		c.InfrastructureRoles = infraRoles
	}

	// Postgresqls
	c.postgresqlInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc:  c.clusterListFunc,
			WatchFunc: c.clusterWatchFunc,
		},
		&spec.Postgresql{},
		constants.QueueResyncPeriodTPR,
		cache.Indexers{})

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
		constants.QueueResyncPeriodPod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.podAdd,
		UpdateFunc: c.podUpdate,
		DeleteFunc: c.podDelete,
	})

	c.clusterEventQueues = make([]*cache.FIFO, c.opConfig.Workers)
	for i := range c.clusterEventQueues {
		c.clusterEventQueues[i] = cache.NewFIFO(func(obj interface{}) (string, error) {
			e, ok := obj.(spec.ClusterEvent)
			if !ok {
				return "", fmt.Errorf("could not cast to ClusterEvent")
			}

			return fmt.Sprintf("%s-%s", e.EventType, e.UID), nil
		})
	}
}

func (c *Controller) runInformers(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)
	go c.podInformer.Run(stopCh)
	go c.podEventsDispatcher(stopCh)
	go c.clusterResync(stopCh)

	<-stopCh
}
