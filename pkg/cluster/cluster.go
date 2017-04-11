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
	"golang.org/x/net/context"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/rest"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/config"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
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
	OpConfig       *config.Config
}

type kubeResources struct {
	Service     *v1.Service
	Endpoint    *v1.Endpoints
	Secrets     map[types.UID]*v1.Secret
	Statefulset *v1beta1.StatefulSet
	//Pods are treated separately
	//PVCs are treated separately
}

type Cluster struct {
	kubeResources
	spec.Postgresql
	Config
	logger         *logrus.Entry
	pgUsers        map[string]spec.PgUser
	podEvents      chan spec.PodEvent
	podSubscribers map[spec.PodName]chan spec.PodEvent
	pgDb           *sql.DB
	mu             sync.Mutex
}

func New(cfg Config, pgSpec spec.Postgresql, logger *logrus.Logger) *Cluster {
	lg := logger.WithField("pkg", "cluster").WithField("cluster-name", pgSpec.Metadata.Name)
	kubeResources := kubeResources{Secrets: make(map[types.UID]*v1.Secret)}

	cluster := &Cluster{
		Config:         cfg,
		Postgresql:     pgSpec,
		logger:         lg,
		pgUsers:        make(map[string]spec.PgUser),
		podEvents:      make(chan spec.PodEvent),
		podSubscribers: make(map[spec.PodName]chan spec.PodEvent),
		kubeResources:  kubeResources,
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

func (c *Cluster) SetStatus(status spec.PostgresStatus) {
	c.Status = status
	b, err := json.Marshal(status)
	if err != nil {
		c.logger.Fatalf("Can't marshal status: %s", err)
	}
	request := []byte(fmt.Sprintf(`{"status": %s}`, string(b))) //TODO: Look into/wait for k8s go client methods

	_, err = c.RestClient.Patch(api.MergePatchType).
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

func (c *Cluster) initUsers() error {
	c.initSystemUsers()
	if err := c.initRobotUsers(); err != nil {
		return fmt.Errorf("Can't init robot users: %s", err)
	}

	if err := c.initHumanUsers(); err != nil {
		return fmt.Errorf("Can't init human users: %s", err)
	}

	return nil
}

func (c *Cluster) etcdKeyExists(keyName string) (bool, error) {
	options := etcdclient.GetOptions{}
	resp, err := c.EtcdClient.Get(context.Background(), keyName, &options)
	if err != nil {
		etcdErr, ok := err.(etcdclient.Error)
		if !ok {
			return false, err
		}
		if etcdErr.Code == etcdclient.ErrorCodeKeyNotFound {
			return false, nil
		}
	}

	return resp != nil, err
}

func (c *Cluster) Create() error {
	keyExist, err := c.etcdKeyExists(fmt.Sprintf("/%s/%s", c.OpConfig.EtcdScope, c.Metadata.Name))
	if err != nil {
		c.logger.Warnf("Can't check etcd key: %s", err)
	}
	if keyExist {
		c.logger.Warnf("Etcd key for the cluster already exists")
	}
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

	if err := c.initUsers(); err != nil {
		return err
	} else {
		c.logger.Infof("User secrets have been initialized")
	}

	if err := c.applySecrets(); err != nil {
		return fmt.Errorf("Can't create Secrets: %s", err)
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

	if err := c.waitStatefulsetPodsReady(); err != nil {
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

func (c Cluster) sameServiceWith(service *v1.Service) (match bool, reason string) {
	//TODO: improve comparison
	if !reflect.DeepEqual(c.Service.Spec.LoadBalancerSourceRanges, service.Spec.LoadBalancerSourceRanges) {
		reason = "new service's LoadBalancerSourceRange doesn't match the current one"
	} else {
		match = true
	}
	return
}

func (c Cluster) sameVolumeWith(volume spec.Volume) (match bool, reason string) {
	if !reflect.DeepEqual(c.Spec.Volume, volume) {
		reason = "new volume's specification doesn't match the current one"
	} else {
		match = true
	}
	return
}

func (c Cluster) compareStatefulSetWith(statefulSet *v1beta1.StatefulSet) (match, needsRollUpdate bool, reason string) {
	match = true
	//TODO: improve me
	if *c.Statefulset.Spec.Replicas != *statefulSet.Spec.Replicas {
		match = false
		reason = "new statefulset's number of replicas doesn't match the current one"
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) != len(statefulSet.Spec.Template.Spec.Containers) {
		match = false
		needsRollUpdate = true
		reason = "new statefulset's container specification doesn't match the current one"
		return
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) == 0 {
		c.logger.Warnf("StatefulSet '%s' has no container", util.NameFromMeta(c.Statefulset.ObjectMeta))
		return
	}

	container1 := c.Statefulset.Spec.Template.Spec.Containers[0]
	container2 := statefulSet.Spec.Template.Spec.Containers[0]
	if container1.Image != container2.Image {
		match = false
		needsRollUpdate = true
		reason = "new statefulset's container image doesn't match the current one"
		return
	}

	if !reflect.DeepEqual(container1.Ports, container2.Ports) {
		match = false
		needsRollUpdate = true
		reason = "new statefulset's container ports don't match the current one"
		return
	}

	if !reflect.DeepEqual(container1.Resources, container2.Resources) {
		match = false
		needsRollUpdate = true
		reason = "new statefulset's container resources don't match the current ones"
		return
	}
	if !reflect.DeepEqual(container1.Env, container2.Env) {
		match = false
		needsRollUpdate = true
		reason = "new statefulset's container environment doesn't match the current one"
	}

	return
}

func (c *Cluster) Update(newSpec *spec.Postgresql) error {
	c.logger.Infof("Cluster update from version %s to %s",
		c.Metadata.ResourceVersion, newSpec.Metadata.ResourceVersion)

	newService := c.genService(newSpec.Spec.AllowedSourceRanges)
	if match, reason := c.sameServiceWith(newService); !match {
		c.logServiceChanges(c.Service, newService, true, reason)
		if err := c.updateService(newService); err != nil {
			return fmt.Errorf("Can't update Service: %s", err)
		} else {
			c.logger.Infof("Service '%s' has been updated", util.NameFromMeta(c.Service.ObjectMeta))
		}
	}

	if match, reason := c.sameVolumeWith(newSpec.Spec.Volume); !match {
		c.logVolumeChanges(c.Spec.Volume, newSpec.Spec.Volume, reason)
		//TODO: update PVC
	}

	newStatefulSet := c.genStatefulSet(newSpec.Spec)
	sameSS, rollingUpdate, reason := c.compareStatefulSetWith(newStatefulSet)

	if !sameSS {
		c.logStatefulSetChanges(c.Statefulset, newStatefulSet, true, reason)
		//TODO: mind the case of updating allowedSourceRanges
		if err := c.updateStatefulSet(newStatefulSet); err != nil {
			return fmt.Errorf("Can't upate StatefulSet: %s", err)
		}
		c.logger.Infof("StatefulSet '%s' has been updated", util.NameFromMeta(c.Statefulset.ObjectMeta))
	}

	if c.Spec.PgVersion != newSpec.Spec.PgVersion { // PG versions comparison
		c.logger.Warnf("Postgresql version change(%s -> %s) is not allowed",
			c.Spec.PgVersion, newSpec.Spec.PgVersion)
		//TODO: rewrite pg version in tpr spec
	}

	if rollingUpdate {
		c.logger.Infof("Rolling update is needed")
		// TODO: wait for actual streaming to the replica
		if err := c.recreatePods(); err != nil {
			return fmt.Errorf("Can't recreate Pods: %s", err)
		}
		c.logger.Infof("Rolling update has been finished")
	}

	return nil
}

func (c *Cluster) Delete() error {
	if err := c.deleteEndpoint(); err != nil {
		c.logger.Errorf("Can't delete Endpoint: %s", err)
	}

	if err := c.deleteService(); err != nil {
		c.logger.Errorf("Can't delete Service: %s", err)
	}

	if err := c.deleteStatefulSet(); err != nil {
		c.logger.Errorf("Can't delete StatefulSet: %s", err)
	}

	for _, obj := range c.Secrets {
		if err := c.deleteSecret(obj); err != nil {
			c.logger.Errorf("Can't delete Secret: %s", err)
		}
	}

	return nil
}

func (c *Cluster) ReceivePodEvent(event spec.PodEvent) {
	c.podEvents <- event
}

func (c *Cluster) initSystemUsers() {
	c.pgUsers[c.OpConfig.SuperUsername] = spec.PgUser{
		Name:     c.OpConfig.SuperUsername,
		Password: util.RandomPassword(constants.PasswordLength),
	}

	c.pgUsers[c.OpConfig.ReplicationUsername] = spec.PgUser{
		Name:     c.OpConfig.ReplicationUsername,
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
