package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

// Config describes configuration of the controller
type Config struct {
	RestConfig          *rest.Config
	InfrastructureRoles map[string]spec.PgUser

	NoDatabaseAccess bool
	NoTeamsAPI       bool
	ConfigMapName    spec.NamespacedName
	Namespace        string
}

// Controller represents operator controller
type Controller struct {
	config   Config
	opConfig *config.Config

	logger     *logrus.Entry
	KubeClient k8sutil.KubernetesClient
	RestClient rest.Interface // kubernetes API group REST client

	clustersMu sync.RWMutex
	clusters   map[spec.NamespacedName]*cluster.Cluster
	stopChs    map[spec.NamespacedName]chan struct{}

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	podCh              chan spec.PodEvent

	clusterEventQueues  []*cache.FIFO
	lastClusterSyncTime int64
}

// NewController creates a new controller
func NewController(controllerConfig *Config) *Controller {
	logger := logrus.New()

	return &Controller{
		config:   *controllerConfig,
		opConfig: &config.Config{},
		logger:   logger.WithField("pkg", "controller"),
		clusters: make(map[spec.NamespacedName]*cluster.Cluster),
		stopChs:  make(map[spec.NamespacedName]chan struct{}),
		podCh:    make(chan spec.PodEvent),
	}
}

func (c *Controller) initClients() {
	client, err := k8sutil.ClientSet(c.config.RestConfig)
	if err != nil {
		c.logger.Fatalf("couldn't create client: %v", err)
	}
	c.KubeClient = k8sutil.NewFromKubernetesInterface(client)

	c.RestClient, err = k8sutil.KubernetesRestClient(*c.config.RestConfig)
	if err != nil {
		c.logger.Fatalf("couldn't create rest client: %v", err)
	}
}

func (c *Controller) initOperatorConfig() {
	configMapData := make(map[string]string)

	if c.config.ConfigMapName != (spec.NamespacedName{}) {
		configMap, err := c.KubeClient.ConfigMaps(c.config.ConfigMapName.Namespace).
			Get(c.config.ConfigMapName.Name, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}

		configMapData = configMap.Data
	} else {
		c.logger.Infoln("no ConfigMap specified. Loading default values")
	}

	if configMapData["namespace"] == "" { // Namespace in ConfigMap has priority over env var
		configMapData["namespace"] = c.config.Namespace
	}
	if c.config.NoDatabaseAccess {
		configMapData["enable_database_access"] = "false"
	}
	if c.config.NoTeamsAPI {
		configMapData["enable_teams_api"] = "false"
	}

	c.opConfig = config.NewFromMap(configMapData)
}

func (c *Controller) initController() {
	c.initClients()
	c.initOperatorConfig()

	c.logger.Infof("config: %s", c.opConfig.MustMarshal())

	if c.opConfig.DebugLogging {
		c.logger.Logger.Level = logrus.DebugLevel
	}

	if err := c.createTPR(); err != nil {
		c.logger.Fatalf("could not register ThirdPartyResource: %v", err)
	}

	if infraRoles, err := c.getInfrastructureRoles(&c.opConfig.InfrastructureRolesSecretName); err != nil {
		c.logger.Warningf("could not get infrastructure roles: %v", err)
	} else {
		c.config.InfrastructureRoles = infraRoles
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

// Run starts background controller processes
func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	c.initController()

	wg.Add(3)
	go c.runPodInformer(stopCh, wg)
	go c.runPostgresqlInformer(stopCh, wg)
	go c.clusterResync(stopCh, wg)

	for i := range c.clusterEventQueues {
		wg.Add(1)
		go c.processClusterEventsQueue(i, stopCh, wg)
	}

	c.logger.Info("started working in background")
}

func (c *Controller) runPodInformer(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	c.podInformer.Run(stopCh)
}

func (c *Controller) runPostgresqlInformer(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	c.postgresqlInformer.Run(stopCh)
}
