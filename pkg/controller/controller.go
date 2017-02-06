package controller

import (
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	v1beta1extensions "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.bus.zalan.do/acid/postgres-operator/pkg/cluster"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
)

type Config struct {
	Namespace  string
	KubeClient *kubernetes.Clientset
	RestClient *rest.RESTClient
}

type Controller struct {
	Config

	logger             *logrus.Entry
	events             chan *Event
	clusters           map[string]*cluster.Cluster
	stopChMap          map[string]chan struct{}
	waitCluster        sync.WaitGroup
	postgresqlInformer cache.SharedIndexInformer
}

type Event struct {
	Type   string
	Object *spec.Postgresql
}

func New(cfg *Config) *Controller {
	return &Controller{
		Config:    *cfg,
		logger:    logrus.WithField("pkg", "controller"),
		clusters:  make(map[string]*cluster.Cluster),
		stopChMap: map[string]chan struct{}{},
	}
}

func (c *Controller) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	c.initController()

	go c.watchTpr(stopCh)
	go c.watchTprEvents(stopCh)

	c.logger.Info("Started working in background")
}

func (c *Controller) watchTpr(stopCh <-chan struct{}) {
	go c.postgresqlInformer.Run(stopCh)

	<-stopCh
}

func (c *Controller) watchTprEvents(stopCh <-chan struct{}) {
	//fmt.Println("Watching tpr events")

	<-stopCh
}

func (c *Controller) createTPR() error {
	tpr := &v1beta1extensions.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", constants.TPRName, constants.TPRVendor),
		},
		Versions: []v1beta1extensions.APIVersion{
			{Name: constants.TPRApiVersion},
		},
		Description: constants.TPRDescription,
	}

	_, err := c.KubeClient.ExtensionsV1beta1().ThirdPartyResources().Create(tpr)

	if err != nil {
		if !k8sutil.IsKubernetesResourceAlreadyExistError(err) {
			return err
		} else {
			c.logger.Info("ThirdPartyResource already registered")
		}
	}

	restClient := c.RestClient

	return k8sutil.WaitTPRReady(restClient, constants.TPRReadyWaitInterval, constants.TPRReadyWaitTimeout, c.Namespace)
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		Namespace:  c.Namespace,
		KubeClient: c.KubeClient,
		RestClient: c.RestClient,
	}
}

func (c *Controller) initController() {
	err := c.createTPR()
	if err != nil {
		c.logger.Fatalf("Can't register ThirdPartyResource: %s", err)
	}

	c.postgresqlInformer = cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(c.RestClient, constants.ResourceName, v1.NamespaceAll, fields.Everything()),
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

	if pg.Spec == nil {
		return
	}

	cluster := cluster.New(c.makeClusterConfig(), pg)
	cluster.Create()

	c.logger.Infof("Add: %+v", cluster)
}

func (c *Controller) clusterUpdate(prev, cur interface{}) {
	pgPrev := prev.(*spec.Postgresql)
	pgCur := cur.(*spec.Postgresql)

	if pgPrev.Spec == nil || pgCur.Spec == nil {
		return
	}

	c.logger.Infof("Update: %+v -> %+v", *pgPrev.Spec, *pgCur.Spec)
}

func (c *Controller) clusterDelete(obj interface{}) {
	pg := obj.(*spec.Postgresql)
	if pg.Spec == nil {
		return
	}

	cluster := cluster.New(c.makeClusterConfig(), pg)
	cluster.Delete()

	c.logger.Infof("Delete: %+v", *pg.Spec)
}
