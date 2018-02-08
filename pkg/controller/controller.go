package controller

import (
	"fmt"
	"os"
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

	curWorkerID      uint32 //initialized with 0
	curWorkerCluster sync.Map
	clusterWorkers   map[spec.NamespacedName]uint32
	clustersMu       sync.RWMutex
	clusters         map[spec.NamespacedName]*cluster.Cluster
	clusterLogs      map[spec.NamespacedName]ringlog.RingLogger
	clusterHistory   map[spec.NamespacedName]ringlog.RingLogger // history of the cluster changes
	teamClusters     map[string][]spec.NamespacedName

	postgresqlInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer
	nodesInformer      cache.SharedIndexInformer
	podCh              chan spec.PodEvent

	clusterEventQueues  []*cache.FIFO // [workerID]Queue
	lastClusterSyncTime int64

	workerLogs map[uint32]ringlog.RingLogger
}

// NewController creates a new controller
func NewController(controllerConfig *spec.ControllerConfig) *Controller {
	logger := logrus.New()

	c := &Controller{
		config:           *controllerConfig,
		opConfig:         &config.Config{},
		logger:           logger.WithField("pkg", "controller"),
		curWorkerCluster: sync.Map{},
		clusterWorkers:   make(map[spec.NamespacedName]uint32),
		clusters:         make(map[spec.NamespacedName]*cluster.Cluster),
		clusterLogs:      make(map[spec.NamespacedName]ringlog.RingLogger),
		clusterHistory:   make(map[spec.NamespacedName]ringlog.RingLogger),
		teamClusters:     make(map[string][]spec.NamespacedName),
		stopCh:           make(chan struct{}),
		podCh:            make(chan spec.PodEvent),
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

	// by default, the operator listens to all namespaces
	if configMapData["watched_namespace"] == "" {
		c.logger.Infoln("The operator config map specifies no namespace to watch. Falling back to watching all namespaces.")
		configMapData["watched_namespace"] = v1.NamespaceAll
	}

	watchedNsEnvVar, isPresentInEnv := os.LookupEnv("WATCHED_NAMESPACE")
	if isPresentInEnv {
		c.logger.Infoln("The WATCHED_NAMESPACE env variable takes priority over the same param from the operator configMap\n")
		// special case: v1.NamespaceAll currently also evaluates to the empty string
		// so when the env var is set to the empty string, use the default ns
		// since the meaning of this env var is only one namespace
		if watchedNsEnvVar == "" {
			c.logger.Infof("The WATCHED_NAMESPACE env var evaluates to the empty string, falling back to watching the 'default' namespace.\n", watchedNsEnvVar)
			configMapData["watched_namespace"] = v1.NamespaceDefault
		} else {
			c.logger.Infof("Watch the %q namespace specified in the env variable WATCHED_NAMESPACE\n", watchedNsEnvVar)
			configMapData["watched_namespace"] = watchedNsEnvVar
		}
	}

	if c.config.NoDatabaseAccess {
		configMapData["enable_database_access"] = "false"
	}
	if c.config.NoTeamsAPI {
		configMapData["enable_teams_api"] = "false"
	}

	c.opConfig = config.NewFromMap(configMapData)

	scalyrAPIKey := os.Getenv("SCALYR_API_KEY")
	if scalyrAPIKey != "" {
		c.opConfig.ScalyrAPIKey = scalyrAPIKey
	}
}

func (c *Controller) initController() {
	c.initClients()
	c.initOperatorConfig()

	// earliest point where we can check if the namespace to watch actually exists
	if c.opConfig.WatchedNamespace != v1.NamespaceAll {
		_, err := c.KubeClient.Namespaces().Get(c.opConfig.WatchedNamespace, metav1.GetOptions{})
		if err != nil {
			c.logger.Fatalf("Operator was told to watch the %q namespace but was unable to find it via Kubernetes API.", c.opConfig.WatchedNamespace)
		}

	}

	c.initSharedInformers()

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

func (c *Controller) initSharedInformers() {
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

	// Kubernetes Nodes
	nodeLw := &cache.ListWatch{
		ListFunc:  c.nodeListFunc,
		WatchFunc: c.nodeWatchFunc,
	}

	c.nodesInformer = cache.NewSharedIndexInformer(
		nodeLw,
		&v1.Node{},
		constants.QueueResyncPeriodNode,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	c.nodesInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.nodeAdd,
		UpdateFunc: c.nodeUpdate,
		DeleteFunc: c.nodeDelete,
	})
}

// Run starts background controller processes
func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	c.initController()

	wg.Add(5)
	go c.runPodInformer(stopCh, wg)
	go c.runPostgresqlInformer(stopCh, wg)
	go c.clusterResync(stopCh, wg)
	go c.apiserver.Run(stopCh, wg)
	go c.kubeNodesInformer(stopCh, wg)

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

func (c *Controller) kubeNodesInformer(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	c.nodesInformer.Run(stopCh)
}
