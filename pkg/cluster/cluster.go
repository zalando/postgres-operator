package cluster

// Postgres ThirdPartyResource object i.e. Spilo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"github.com/Sirupsen/logrus"
	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/rest"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/resources"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/teams"
)

var (
	alphaNumericRegexp = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9]*$")
)

//TODO: remove struct duplication
type Config struct {
	ControllerNamespace string
	KubeClient          *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient          *rest.RESTClient
	EtcdClient          etcdclient.KeysAPI
	TeamsAPIClient      *teams.TeamsAPI
}

type KubeResources struct {
	Services     map[types.UID]*v1.Service
	Endpoints    map[types.UID]*v1.Endpoints
	Secrets      map[types.UID]*v1.Secret
	Statefulsets map[types.UID]*v1beta1.StatefulSet
	//Pods are treated separately
}

type Cluster struct {
	KubeResources
	spec.Postgresql
	config         Config
	logger         *logrus.Entry
	etcdHost       string
	dockerImage    string
	pgUsers        map[string]spec.PgUser
	podEvents      chan spec.PodEvent
	podSubscribers map[spec.PodName]chan spec.PodEvent
	pgDb           *sql.DB
	mu             sync.Mutex
}

func New(cfg Config, pgSpec spec.Postgresql) *Cluster {
	lg := logrus.WithField("pkg", "cluster").WithField("cluster-name", pgSpec.Metadata.Name)
	kubeResources := KubeResources{
		Services:     make(map[types.UID]*v1.Service),
		Endpoints:    make(map[types.UID]*v1.Endpoints),
		Secrets:      make(map[types.UID]*v1.Secret),
		Statefulsets: make(map[types.UID]*v1beta1.StatefulSet),
	}

	cluster := &Cluster{
		config:         cfg,
		Postgresql:     pgSpec,
		logger:         lg,
		etcdHost:       constants.EtcdHost,
		dockerImage:    constants.SpiloImage,
		pgUsers:        make(map[string]spec.PgUser),
		podEvents:      make(chan spec.PodEvent),
		podSubscribers: make(map[spec.PodName]chan spec.PodEvent),
		KubeResources:  kubeResources,
	}

	return cluster
}

func (c *Cluster) ClusterName() spec.ClusterName {
	return spec.ClusterName{
		Name:      c.Metadata.Name,
		Namespace: c.Metadata.Namespace,
	}
}

func (c *Cluster) Run(stopCh <-chan struct{}) {
	go c.podEventsDispatcher(stopCh)

	<-stopCh
}

func (c *Cluster) NeedsRollingUpdate(otherSpec *spec.Postgresql) bool {
	//TODO: add more checks
	if c.Spec.Version != otherSpec.Spec.Version {
		return true
	}

	if !reflect.DeepEqual(c.Spec.Resources, otherSpec.Spec.Resources) {
		return true
	}

	return false
}

func (c *Cluster) MustSetStatus(status spec.PostgresStatus) {
	b, err := json.Marshal(status)
	if err != nil {
		c.logger.Fatalf("Can't marshal status: %s", err)
	}
	request := []byte(fmt.Sprintf(`{"status": %s}`, string(b))) //TODO: Look into/wait for k8s go client methods

	_, err = c.config.RestClient.Patch(api.MergePatchType).
		RequestURI(c.Metadata.GetSelfLink()).
		Body(request).
		DoRaw()

	if k8sutil.ResourceNotFound(err) {
		c.logger.Warningf("Can't set status for the non-existing cluster")
		return
	}

	if err != nil {
		c.logger.Fatalf("Can't set status for cluster '%s': %s", c.ClusterName(), err)
	}
}

func (c *Cluster) Create() error {
	//TODO: service will create endpoint implicitly
	ep, err := c.createEndpoint()
	if err != nil {
		return fmt.Errorf("Can't create endpoint: %s", err)
	}
	c.logger.Infof("Endpoint '%s' has been successfully created", util.NameFromMeta(ep.ObjectMeta))

	service, err := c.createService()
	if err != nil {
		return fmt.Errorf("Can't create service: %s", err)
	} else {
		c.logger.Infof("Service '%s' has been successfully created", util.NameFromMeta(service.ObjectMeta))
	}

	c.initSystemUsers()
	err = c.initRobotUsers()
	if err != nil {
		return fmt.Errorf("Can't init robot users: %s", err)
	}

	err = c.initHumanUsers()
	if err != nil {
		return fmt.Errorf("Can't init human users: %s", err)
	}

	err = c.applySecrets()
	if err != nil {
		return fmt.Errorf("Can't create secrets: %s", err)
	} else {
		c.logger.Infof("Secrets have been successfully created")
	}

	ss, err := c.createStatefulSet()
	if err != nil {
		return fmt.Errorf("Can't create StatefulSet: %s", err)
	} else {
		c.logger.Infof("StatefulSet '%s' has been successfully created", util.NameFromMeta(ss.ObjectMeta))
	}

	c.logger.Info("Waiting for cluster being ready")
	err = c.waitClusterReady()
	if err != nil {
		c.logger.Errorf("Failed to create cluster: %s", err)
		return err
	}

	err = c.initDbConn()
	if err != nil {
		return fmt.Errorf("Can't init db connection: %s", err)
	}

	err = c.createUsers()
	if err != nil {
		return fmt.Errorf("Can't create users: %s", err)
	} else {
		c.logger.Infof("Users have been successfully created")
	}

	c.ListResources()

	return nil
}

func (c *Cluster) Update(newSpec *spec.Postgresql, rollingUpdate bool) error {
	nSpec := newSpec.Spec
	clusterName := c.ClusterName()
	resourceList := resources.ResourceList(nSpec.Resources)
	template := resources.PodTemplate(clusterName, resourceList, c.dockerImage, nSpec.Version, c.etcdHost)
	statefulSet := resources.StatefulSet(clusterName, template, nSpec.NumberOfInstances)

	//TODO: mind the case of updating allowedSourceRanges
	err := c.updateStatefulSet(statefulSet)
	if err != nil {
		return fmt.Errorf("Can't upate cluster: %s", err)
	}

	if rollingUpdate {
		err = c.recreatePods()
		// TODO: wait for actual streaming to the replica
		if err != nil {
			return fmt.Errorf("Can't recreate pods: %s", err)
		}
	}

	return nil
}

func (c *Cluster) Delete() error {
	for _, obj := range c.Statefulsets {
		err := c.deleteStatefulSet(obj)
		if err != nil {
			c.logger.Errorf("Can't delete StatefulSet: %s", err)
		} else {
			c.logger.Infof("StatefulSet '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	for _, obj := range c.Secrets {
		err := c.deleteSecret(obj)
		if err != nil {
			c.logger.Errorf("Can't delete secret: %s", err)
		} else {
			c.logger.Infof("Secret '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	for _, obj := range c.Endpoints {
		err := c.deleteEndpoint(obj)
		if err != nil {
			c.logger.Errorf("Can't delete endpoint: %s", err)
		} else {
			c.logger.Infof("Endpoint '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	for _, obj := range c.Services {
		err := c.deleteService(obj)
		if err != nil {
			c.logger.Errorf("Can't delete service: %s", err)
		} else {
			c.logger.Infof("Service '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	err := c.deletePods()
	if err != nil {
		return fmt.Errorf("Can't delete pods: %s", err)
	}

	return nil
}

func (c *Cluster) ReceivePodEvent(event spec.PodEvent) {
	c.podEvents <- event
}

func (c *Cluster) initSystemUsers() {
	c.pgUsers[constants.SuperuserName] = spec.PgUser{
		Name:     constants.SuperuserName,
		Password: util.RandomPassword(constants.PasswordLength),
	}

	c.pgUsers[constants.ReplicationUsername] = spec.PgUser{
		Name:     constants.ReplicationUsername,
		Password: util.RandomPassword(constants.PasswordLength),
	}
}

func (c *Cluster) initRobotUsers() error {
	for username, userFlags := range c.Spec.Users {
		if !isValidUsername(username) {
			return fmt.Errorf("Invalid username: '%s'", username)
		}

		flags, err := normalizeUserFlags(userFlags)
		if err != nil {
			return fmt.Errorf("Invalid flags for user '%s': %s", username, err)
		}

		c.pgUsers[username] = spec.PgUser{
			Name:     username,
			Password: util.RandomPassword(constants.PasswordLength),
			Flags:    flags,
		}
	}

	return nil
}

func (c *Cluster) initHumanUsers() error {
	teamMembers, err := c.getTeamMembers()
	if err != nil {
		return fmt.Errorf("Can't get list of team members: %s", err)
	} else {
		for _, username := range teamMembers {
			c.pgUsers[username] = spec.PgUser{Name: username}
		}
	}

	return nil
}
