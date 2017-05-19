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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	"k8s.io/client-go/pkg/types"
	"k8s.io/client-go/rest"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
	"github.com/zalando-incubator/postgres-operator/pkg/util/users"
)

var (
	alphaNumericRegexp = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9]*$")
	userRegexp         = regexp.MustCompile(`^[a-z0-9]([-_a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-_a-z0-9]*[a-z0-9])?)*$`)
)

//TODO: remove struct duplication
type Config struct {
	KubeClient          *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient          *rest.RESTClient
	TeamsAPIClient      *teams.API
	OpConfig            config.Config
	InfrastructureRoles map[string]spec.PgUser // inherited from the controller
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
	logger               *logrus.Entry
	pgUsers              map[string]spec.PgUser
	systemUsers          map[string]spec.PgUser
	podEvents            chan spec.PodEvent
	podSubscribers       map[spec.NamespacedName]chan spec.PodEvent
	podSubscribersMu     sync.RWMutex
	pgDb                 *sql.DB
	mu                   sync.Mutex
	masterLess           bool
	podDispatcherRunning bool
	userSyncStrategy     spec.UserSyncer
	deleteOptions        *v1.DeleteOptions
}

func New(cfg Config, pgSpec spec.Postgresql, logger *logrus.Entry) *Cluster {
	lg := logger.WithField("pkg", "cluster").WithField("cluster-name", pgSpec.Metadata.Name)
	kubeResources := kubeResources{Secrets: make(map[types.UID]*v1.Secret)}
	orphanDependents := true

	cluster := &Cluster{
		Config:               cfg,
		Postgresql:           pgSpec,
		logger:               lg,
		pgUsers:              make(map[string]spec.PgUser),
		systemUsers:          make(map[string]spec.PgUser),
		podEvents:            make(chan spec.PodEvent),
		podSubscribers:       make(map[spec.NamespacedName]chan spec.PodEvent),
		kubeResources:        kubeResources,
		masterLess:           false,
		podDispatcherRunning: false,
		userSyncStrategy:     users.DefaultUserSyncStrategy{},
		deleteOptions:        &v1.DeleteOptions{OrphanDependents: &orphanDependents},
	}

	return cluster
}

func (c *Cluster) ClusterName() spec.NamespacedName {
	return util.NameFromMeta(c.Metadata)
}

func (c *Cluster) teamName() string {
	// TODO: check Teams API for the actual name (in case the user passes an integer Id).
	return c.Spec.TeamID
}

func (c *Cluster) setStatus(status spec.PostgresStatus) {
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

	if err := c.initInfrastructureRoles(); err != nil {
		return fmt.Errorf("Can't init infrastructure roles: %s", err)
	}

	if err := c.initRobotUsers(); err != nil {
		return fmt.Errorf("Can't init robot users: %s", err)
	}

	if err := c.initHumanUsers(); err != nil {
		return fmt.Errorf("Can't init human users: %s", err)
	}

	c.logger.Debugf("Initialized users: %# v", util.Pretty(c.pgUsers))

	return nil
}

func (c *Cluster) Create(stopCh <-chan struct{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error

	if !c.podDispatcherRunning {
		go c.podEventsDispatcher(stopCh)
		c.podDispatcherRunning = true
	}

	defer func() {
		if err == nil {
			c.setStatus(spec.ClusterStatusRunning) //TODO: are you sure it's running?
		} else {
			c.setStatus(spec.ClusterStatusAddFailed)
		}
	}()

	c.setStatus(spec.ClusterStatusCreating)

	//TODO: service will create endpoint implicitly
	ep, err := c.createEndpoint()
	if err != nil {
		return fmt.Errorf("Can't create Endpoint: %s", err)
	}
	c.logger.Infof("Endpoint '%s' has been successfully created", util.NameFromMeta(ep.ObjectMeta))

	service, err := c.createService()
	if err != nil {
		return fmt.Errorf("Can't create Service: %s", err)
	}
	c.logger.Infof("Service '%s' has been successfully created", util.NameFromMeta(service.ObjectMeta))

	if err = c.initUsers(); err != nil {
		return err
	}
	c.logger.Infof("User secrets have been initialized")

	if err = c.applySecrets(); err != nil {
		return fmt.Errorf("Can't create Secrets: %s", err)
	}
	c.logger.Infof("Secrets have been successfully created")

	ss, err := c.createStatefulSet()
	if err != nil {
		return fmt.Errorf("Can't create StatefulSet: %s", err)
	}
	c.logger.Infof("StatefulSet '%s' has been successfully created", util.NameFromMeta(ss.ObjectMeta))

	c.logger.Info("Waiting for cluster being ready")

	if err = c.waitStatefulsetPodsReady(); err != nil {
		c.logger.Errorf("Failed to create cluster: %s", err)
		return err
	}
	c.logger.Infof("Pods are ready")

	if !(c.masterLess || c.databaseAccessDisabled()) {
		if err := c.initDbConn(); err != nil {
			return fmt.Errorf("Can't init db connection: %s", err)
		}
		if err = c.createUsers(); err != nil {
			return fmt.Errorf("Can't create users: %s", err)
		}
		c.logger.Infof("Users have been successfully created")
	} else {
		if c.masterLess {
			c.logger.Warnln("Cluster is masterless")
		}
	}

	err = c.ListResources()
	if err != nil {
		c.logger.Errorf("Can't list resources: %s", err)
	}

	return nil
}

func (c *Cluster) sameServiceWith(service *v1.Service) (match bool, reason string) {
	//TODO: improve comparison
	if !reflect.DeepEqual(c.Service.Spec.LoadBalancerSourceRanges, service.Spec.LoadBalancerSourceRanges) {
		reason = "new service's LoadBalancerSourceRange doesn't match the current one"
	} else {
		match = true
	}
	return
}

func (c *Cluster) sameVolumeWith(volume spec.Volume) (match bool, reason string) {
	if !reflect.DeepEqual(c.Spec.Volume, volume) {
		reason = "new volume's specification doesn't match the current one"
	} else {
		match = true
	}
	return
}

func (c *Cluster) compareStatefulSetWith(statefulSet *v1beta1.StatefulSet) (match, needsReplace, needsRollUpdate bool, reason string) {
	match = true
	//TODO: improve me
	if *c.Statefulset.Spec.Replicas != *statefulSet.Spec.Replicas {
		match = false
		reason = "new statefulset's number of replicas doesn't match the current one"
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) != len(statefulSet.Spec.Template.Spec.Containers) {
		needsRollUpdate = true
		reason = "new statefulset's container specification doesn't match the current one"
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) == 0 {
		c.logger.Warnf("StatefulSet '%s' has no container", util.NameFromMeta(c.Statefulset.ObjectMeta))
		return
	}
	// In the comparisons below, the needsReplace and needsRollUpdate flags are never reset, since checks fall through
	// and the combined effect of all the changes should be applied.
	// TODO: log all reasons for changing the statefulset, not just the last one.
	// TODO: make sure this is in sync with genPodTemplate, ideally by using the same list of fields to generate
	// the template and the diff
	if c.Statefulset.Spec.Template.Spec.ServiceAccountName != statefulSet.Spec.Template.Spec.ServiceAccountName {
		needsReplace = true
		needsRollUpdate = true
		reason = "new statefulset's serviceAccountName service asccount name doesn't match the current one"
	}
	if *c.Statefulset.Spec.Template.Spec.TerminationGracePeriodSeconds != *statefulSet.Spec.Template.Spec.TerminationGracePeriodSeconds {
		needsReplace = true
		needsRollUpdate = true
		reason = "new statefulset's terminationGracePeriodSeconds  doesn't match the current one"
	}
	// Some generated fields like creationTimestamp make it not possible to use DeepCompare on Spec.Template.ObjectMeta
	if !reflect.DeepEqual(c.Statefulset.Spec.Template.Labels, statefulSet.Spec.Template.Labels) {
		needsReplace = true
		needsRollUpdate = true
		reason = "new statefulset's metadata labels doesn't match the current one"
	}
	if !reflect.DeepEqual(c.Statefulset.Spec.Template.Annotations, statefulSet.Spec.Template.Annotations) {
		needsRollUpdate = true
		needsReplace = true
		reason = "new statefulset's metadata annotations doesn't match the current one"
	}
	if len(c.Statefulset.Spec.VolumeClaimTemplates) != len(statefulSet.Spec.VolumeClaimTemplates) {
		needsReplace = true
		needsRollUpdate = true
		reason = "new statefulset's volumeClaimTemplates contains different number of volumes to the old one"
	}
	for i := 0; i < len(c.Statefulset.Spec.VolumeClaimTemplates); i++ {
		name := c.Statefulset.Spec.VolumeClaimTemplates[i].Name
		// Some generated fields like creationTimestamp make it not possible to use DeepCompare on ObjectMeta
		if name != statefulSet.Spec.VolumeClaimTemplates[i].Name {
			needsReplace = true
			needsRollUpdate = true
			reason = fmt.Sprintf("new statefulset's name for volume %d doesn't match the current one", i)
			continue
		}
		if !reflect.DeepEqual(c.Statefulset.Spec.VolumeClaimTemplates[i].Annotations, statefulSet.Spec.VolumeClaimTemplates[i].Annotations) {
			needsReplace = true
			needsRollUpdate = true
			reason = fmt.Sprintf("new statefulset's annotations for volume %s doesn't match the current one", name)
		}
		if !reflect.DeepEqual(c.Statefulset.Spec.VolumeClaimTemplates[i].Spec, statefulSet.Spec.VolumeClaimTemplates[i].Spec) {
			name := c.Statefulset.Spec.VolumeClaimTemplates[i].Name
			needsReplace = true
			needsRollUpdate = true
			reason = fmt.Sprintf("new statefulset's volumeClaimTemplates specification for volume %s doesn't match the current one", name)
		}
	}

	container1 := c.Statefulset.Spec.Template.Spec.Containers[0]
	container2 := statefulSet.Spec.Template.Spec.Containers[0]
	if container1.Image != container2.Image {
		needsRollUpdate = true
		reason = "new statefulset's container image doesn't match the current one"
	}

	if !reflect.DeepEqual(container1.Ports, container2.Ports) {
		needsRollUpdate = true
		reason = "new statefulset's container ports don't match the current one"
	}

	if !compareResources(&container1.Resources, &container2.Resources) {
		needsRollUpdate = true
		reason = "new statefulset's container resources don't match the current ones"
	}
	if !reflect.DeepEqual(container1.Env, container2.Env) {
		needsRollUpdate = true
		reason = "new statefulset's container environment doesn't match the current one"
	}

	if needsRollUpdate || needsReplace {
		match = false
	}

	return
}

func compareResources(a *v1.ResourceRequirements, b *v1.ResourceRequirements) (equal bool) {
	equal = true
	if a != nil {
		equal = compareResoucesAssumeFirstNotNil(a, b)
	}
	if equal && (b != nil) {
		equal = compareResoucesAssumeFirstNotNil(b, a)
	}
	return
}

func compareResoucesAssumeFirstNotNil(a *v1.ResourceRequirements, b *v1.ResourceRequirements) bool {
	if b == nil || (len(b.Requests) == 0) {
		return (len(a.Requests) == 0)
	}
	for k, v := range a.Requests {
		if (&v).Cmp(b.Requests[k]) != 0 {
			return false
		}
	}
	for k, v := range a.Limits {
		if (&v).Cmp(b.Limits[k]) != 0 {
			return false
		}
	}
	return true

}

func (c *Cluster) Update(newSpec *spec.Postgresql) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setStatus(spec.ClusterStatusUpdating)
	c.logger.Debugf("Cluster update from version %s to %s",
		c.Metadata.ResourceVersion, newSpec.Metadata.ResourceVersion)

	newService := c.genService(newSpec.Spec.AllowedSourceRanges)
	if match, reason := c.sameServiceWith(newService); !match {
		c.logServiceChanges(c.Service, newService, true, reason)
		if err := c.updateService(newService); err != nil {
			c.setStatus(spec.ClusterStatusUpdateFailed)
			return fmt.Errorf("Can't update Service: %s", err)
		}
		c.logger.Infof("Service '%s' has been updated", util.NameFromMeta(c.Service.ObjectMeta))
	}

	if match, reason := c.sameVolumeWith(newSpec.Spec.Volume); !match {
		c.logVolumeChanges(c.Spec.Volume, newSpec.Spec.Volume, reason)
		//TODO: update PVC
	}

	newStatefulSet, err := c.genStatefulSet(newSpec.Spec)
	if err != nil {
		return fmt.Errorf("Can't generate StatefulSet: %s", err)
	}

	sameSS, needsReplace, rollingUpdate, reason := c.compareStatefulSetWith(newStatefulSet)

	if !sameSS {
		c.logStatefulSetChanges(c.Statefulset, newStatefulSet, true, reason)
		//TODO: mind the case of updating allowedSourceRanges
		if !needsReplace {
			if err := c.updateStatefulSet(newStatefulSet); err != nil {
				c.setStatus(spec.ClusterStatusUpdateFailed)
				return fmt.Errorf("Can't upate StatefulSet: %s", err)
			}
		} else {
			if err := c.replaceStatefulSet(newStatefulSet); err != nil {
				c.setStatus(spec.ClusterStatusUpdateFailed)
				return fmt.Errorf("Can't replace StatefulSet: %s", err)
			}
		}
		//TODO: if there is a change in numberOfInstances, make sure Pods have been created/deleted
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
			c.setStatus(spec.ClusterStatusUpdateFailed)
			return fmt.Errorf("Can't recreate Pods: %s", err)
		}
		c.logger.Infof("Rolling update has been finished")
	}
	c.setStatus(spec.ClusterStatusRunning)

	return nil
}

func (c *Cluster) Delete() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.deleteEndpoint(); err != nil {
		return fmt.Errorf("Can't delete Endpoint: %s", err)
	}

	if err := c.deleteService(); err != nil {
		return fmt.Errorf("Can't delete Service: %s", err)
	}

	if err := c.deleteStatefulSet(); err != nil {
		return fmt.Errorf("Can't delete StatefulSet: %s", err)
	}

	for _, obj := range c.Secrets {
		if err := c.deleteSecret(obj); err != nil {
			return fmt.Errorf("Can't delete Secret: %s", err)
		}
	}

	return nil
}

func (c *Cluster) ReceivePodEvent(event spec.PodEvent) {
	c.podEvents <- event
}

func (c *Cluster) initSystemUsers() {
	// We don't actually use that to create users, delegating this
	// task to Patroni. Those definitions are only used to create
	// secrets, therefore, setting flags like SUPERUSER or REPLICATION
	// is not necessary here
	c.systemUsers[constants.SuperuserKeyName] = spec.PgUser{
		Name:     c.OpConfig.SuperUsername,
		Password: util.RandomPassword(constants.PasswordLength),
	}
	c.systemUsers[constants.ReplicationUserKeyName] = spec.PgUser{
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
	}
	for _, username := range teamMembers {
		flags := []string{constants.RoleFlagLogin, constants.RoleFlagSuperuser}
		memberOf := []string{c.OpConfig.PamRoleName}
		c.pgUsers[username] = spec.PgUser{Name: username, Flags: flags, MemberOf: memberOf}
	}

	return nil
}

func (c *Cluster) initInfrastructureRoles() error {
	// add infrastucture roles from the operator's definition
	for username, data := range c.InfrastructureRoles {
		if !isValidUsername(username) {
			return fmt.Errorf("Invalid username: '%s'", username)
		}
		flags, err := normalizeUserFlags(data.Flags)
		if err != nil {
			return fmt.Errorf("Invalid flags for user '%s': %s", username, err)
		}
		data.Flags = flags
		c.pgUsers[username] = data
	}
	return nil
}
