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
	KubeClient     *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient     *rest.RESTClient
	EtcdClient     etcdclient.KeysAPI
	TeamsAPIClient *teams.TeamsAPI
}

type KubeResources struct {
	Service     *v1.Service
	Endpoint    *v1.Endpoints
	Secrets     map[types.UID]*v1.Secret
	Statefulset *v1beta1.StatefulSet
	//Pods are treated separately
	//PVCs are treated separately
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
	kubeResources := KubeResources{Secrets: make(map[types.UID]*v1.Secret)}

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

func (c *Cluster) TeamName() string {
	// TODO: check Teams API for the actual name (in case the user passes an integer Id).
	return c.Spec.TeamId
}

func (c *Cluster) Run(stopCh <-chan struct{}) {
	go c.podEventsDispatcher(stopCh)

	<-stopCh
}

func (c *Cluster) needsRollingUpdate(otherSpec *spec.Postgresql) bool {
	//TODO: add more checks
	if c.Spec.Version != otherSpec.Spec.Version {
		return true
	}

	if !reflect.DeepEqual(c.Spec.Resources, otherSpec.Spec.Resources) {
		return true
	}

	return false
}

func (c *Cluster) SetStatus(status spec.PostgresStatus) {
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
		c.logger.Warningf("Can't set status for cluster '%s': %s", c.ClusterName(), err)
	}
}

func (c *Cluster) Create() error {
	//TODO: service will create endpoint implicitly
	ep, err := c.createEndpoint()
	if err != nil {
		return fmt.Errorf("Can't create Endpoint: %s", err)
	}
	c.logger.Infof("Endpoint '%s' has been successfully created", util.NameFromMeta(ep.ObjectMeta))

	service, err := c.createService()
	if err != nil {
		return fmt.Errorf("Can't create Service: %s", err)
	} else {
		c.logger.Infof("Service '%s' has been successfully created", util.NameFromMeta(service.ObjectMeta))
	}

	c.initSystemUsers()
	if err := c.initRobotUsers(); err != nil {
		return fmt.Errorf("Can't init robot users: %s", err)
	}

	if err := c.initHumanUsers(); err != nil {
		return fmt.Errorf("Can't init human users: %s", err)
	}

	if err := c.applySecrets(); err != nil {
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
	err = c.waitStatefulsetPodsReady()
	if err != nil {
		c.logger.Errorf("Failed to create cluster: %s", err)
		return err
	}

	if err := c.initDbConn(); err != nil {
		return fmt.Errorf("Can't init db connection: %s", err)
	}

	if err := c.createUsers(); err != nil {
		return fmt.Errorf("Can't create users: %s", err)
	} else {
		c.logger.Infof("Users have been successfully created")
	}

	c.ListResources()

	return nil
}

func (c *Cluster) Update(newSpec *spec.Postgresql) error {
	c.logger.Infof("Cluster update from version %s to %s",
		c.Metadata.ResourceVersion, newSpec.Metadata.ResourceVersion)

	rollingUpdate := c.needsRollingUpdate(newSpec)
	if rollingUpdate {
		c.logger.Infof("Pods need to be recreated")
	}

	newStatefulSet := getStatefulSet(c.ClusterName(), newSpec.Spec, c.etcdHost, c.dockerImage)

	newService := resources.Service(c.ClusterName(), c.TeamName(), newSpec.Spec.AllowedSourceRanges)
	if !servicesEqual(newService, c.Service) {
		c.logger.Infof("Service needs to be upated")
		if err := c.updateService(newService); err != nil {
			return fmt.Errorf("Can't update Service: %s", err)
		} else {
			c.logger.Infof("Service has been updated")
		}
	}

	if !reflect.DeepEqual(newSpec.Spec.Volume, c.Spec.Volume) {
		//TODO: update PVC
	}

	//TODO: mind the case of updating allowedSourceRanges
	if err := c.updateStatefulSet(newStatefulSet); err != nil {
		return fmt.Errorf("Can't upate StatefulSet: %s", err)
	}

	if rollingUpdate {
		// TODO: wait for actual streaming to the replica
		if err := c.recreatePods(); err != nil {
			return fmt.Errorf("Can't recreate Pods: %s", err)
		}
	}

	return nil
}

func (c *Cluster) Delete() error {
	if err := c.deleteEndpoint(); err != nil {
		c.logger.Errorf("Can't delete Endpoint: %s", err)
	} else {
		c.logger.Infof("Endpoint '%s' has been deleted", util.NameFromMeta(c.Endpoint.ObjectMeta))
	}

	if err := c.deleteService(); err != nil {
		c.logger.Errorf("Can't delete Service: %s", err)
	} else {
		c.logger.Infof("Service '%s' has been deleted", util.NameFromMeta(c.Service.ObjectMeta))
	}

	if err := c.deleteStatefulSet(); err != nil {
		c.logger.Errorf("Can't delete StatefulSet: %s", err)
	} else {
		c.logger.Infof("StatefulSet '%s' has been deleted", util.NameFromMeta(c.Statefulset.ObjectMeta))
	}

	for _, obj := range c.Secrets {
		if err := c.deleteSecret(obj); err != nil {
			c.logger.Errorf("Can't delete Secret: %s", err)
		} else {
			c.logger.Infof("Secret '%s' has been deleted", util.NameFromMeta(obj.ObjectMeta))
		}
	}

	if err := c.deletePods(); err != nil {
		c.logger.Errorf("Can't delete Pods: %s", err)
	} else {
		c.logger.Infof("Pods have been deleted")
	}
	err := c.deletePersistenVolumeClaims()
	if err != nil {
		return fmt.Errorf("Can't delete PersistentVolumeClaims: %s", err)
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
