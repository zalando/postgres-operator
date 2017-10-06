package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/apiserver"
	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/ringlog"
)

// Controller represents operator controller
type Controller struct {
	config   spec.ControllerConfig
	opConfig *config.Config

	logger     *logrus.Entry
	KubeClient k8sutil.KubernetesClient
	apiserver  *apiserver.Server

	stopCh chan struct{}

	clustersMu     sync.RWMutex
	clusters       map[spec.NamespacedName]*cluster.Cluster
	clusterLogs    map[spec.NamespacedName]ringlog.RingLogger
	clusterHistory map[spec.NamespacedName]ringlog.RingLogger // history of the cluster changes
	teamClusters   map[string][]spec.NamespacedName

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	podCh              chan spec.PodEvent

	clusterEventQueues  []*cache.FIFO // [workerID]Queue
	lastClusterSyncTime int64

	workerLogs map[uint32]ringlog.RingLogger
}

// NewController creates a new controller
func NewController(controllerConfig *spec.ControllerConfig) *Controller {
	logger := logrus.New()

	c := &Controller{
		config:         *controllerConfig,
		opConfig:       &config.Config{},
		logger:         logger.WithField("pkg", "controller"),
		clusters:       make(map[spec.NamespacedName]*cluster.Cluster),
		clusterLogs:    make(map[spec.NamespacedName]ringlog.RingLogger),
		clusterHistory: make(map[spec.NamespacedName]ringlog.RingLogger),
		teamClusters:   make(map[string][]spec.NamespacedName),
		stopCh:         make(chan struct{}),
		podCh:          make(chan spec.PodEvent),
	}
	logger.Hooks.Add(c)

	return c
}

func (c *Controller) initClients() {
	var err error

	c.KubeClient, err = k8sutil.NewFromConfig(c.config.RestConfig)
	if err != nil {
		c.logger.Fatalf("could not create kubernetes clients: %v", err)
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

	if err := c.createCRD(); err != nil {
		c.logger.Fatalf("could not register CustomResourceDefinition: %v", err)
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
	c.workerLogs = make(map[uint32]ringlog.RingLogger, c.opConfig.Workers)
	for i := range c.clusterEventQueues {
		c.clusterEventQueues[i] = cache.NewFIFO(func(obj interface{}) (string, error) {
			e, ok := obj.(spec.ClusterEvent)
			if !ok {
				return "", fmt.Errorf("could not cast to ClusterEvent")
			}

			return queueClusterKey(e.EventType, e.UID), nil
		})
	}

	c.apiserver = apiserver.New(c, c.opConfig.APIPort, c.logger.Logger)
}

// Run starts background controller processes
func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	c.initController()

	wg.Add(4)
	go c.runPodInformer(stopCh, wg)
	go c.runPostgresqlInformer(stopCh, wg)
	go c.clusterResync(stopCh, wg)
	go c.apiserver.Run(stopCh, wg)

	for i := range c.clusterEventQueues {
		wg.Add(1)
		c.workerLogs[uint32(i)] = ringlog.New(c.opConfig.RingLogLines)
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

func queueClusterKey(eventType spec.EventType, uid types.UID) string {
	return fmt.Sprintf("%s-%s", eventType, uid)
}
