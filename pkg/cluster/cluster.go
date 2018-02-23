package cluster

// Postgres CustomResourceDefinition object i.e. Spilo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	policybeta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/patroni"
	"github.com/zalando-incubator/postgres-operator/pkg/util/teams"
	"github.com/zalando-incubator/postgres-operator/pkg/util/users"
)

var (
	alphaNumericRegexp    = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9]*$")
	databaseNameRegexp    = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]*$")
	userRegexp            = regexp.MustCompile(`^[a-z0-9]([-_a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-_a-z0-9]*[a-z0-9])?)*$`)
	patroniObjectSuffixes = []string{"config", "failover", "sync"}
)

// Config contains operator-wide clients and configuration used from a cluster. TODO: remove struct duplication.
type Config struct {
	OpConfig            config.Config
	RestConfig          *rest.Config
	InfrastructureRoles map[string]spec.PgUser // inherited from the controller
}

type kubeResources struct {
	Services            map[PostgresRole]*v1.Service
	Endpoints           map[PostgresRole]*v1.Endpoints
	Secrets             map[types.UID]*v1.Secret
	Statefulset         *v1beta1.StatefulSet
	PodDisruptionBudget *policybeta1.PodDisruptionBudget
	//Pods are treated separately
	//PVCs are treated separately
}

// Cluster describes postgresql cluster
type Cluster struct {
	kubeResources
	spec.Postgresql
	Config
	logger           *logrus.Entry
	patroni          patroni.Interface
	pgUsers          map[string]spec.PgUser
	systemUsers      map[string]spec.PgUser
	podSubscribers   map[spec.NamespacedName]chan spec.PodEvent
	podSubscribersMu sync.RWMutex
	pgDb             *sql.DB
	mu               sync.Mutex
	userSyncStrategy spec.UserSyncer
	deleteOptions    *metav1.DeleteOptions
	podEventsQueue   *cache.FIFO

	teamsAPIClient   teams.Interface
	oauthTokenGetter OAuthTokenGetter
	KubeClient       k8sutil.KubernetesClient //TODO: move clients to the better place?
	currentProcess   spec.Process
	processMu        sync.RWMutex // protects the current operation for reporting, no need to hold the master mutex
	specMu           sync.RWMutex // protects the spec for reporting, no need to hold the master mutex
}

type compareStatefulsetResult struct {
	match         bool
	replace       bool
	rollingUpdate bool
	reasons       []string
}

// New creates a new cluster. This function should be called from a controller.
func New(cfg Config, kubeClient k8sutil.KubernetesClient, pgSpec spec.Postgresql, logger *logrus.Entry) *Cluster {
	orphanDependents := true

	podEventsQueue := cache.NewFIFO(func(obj interface{}) (string, error) {
		e, ok := obj.(spec.PodEvent)
		if !ok {
			return "", fmt.Errorf("could not cast to PodEvent")
		}

		return fmt.Sprintf("%s-%s", e.PodName, e.ResourceVersion), nil
	})

	cluster := &Cluster{
		Config:         cfg,
		Postgresql:     pgSpec,
		pgUsers:        make(map[string]spec.PgUser),
		systemUsers:    make(map[string]spec.PgUser),
		podSubscribers: make(map[spec.NamespacedName]chan spec.PodEvent),
		kubeResources: kubeResources{
			Secrets:   make(map[types.UID]*v1.Secret),
			Services:  make(map[PostgresRole]*v1.Service),
			Endpoints: make(map[PostgresRole]*v1.Endpoints)},
		userSyncStrategy: users.DefaultUserSyncStrategy{},
		deleteOptions:    &metav1.DeleteOptions{OrphanDependents: &orphanDependents},
		podEventsQueue:   podEventsQueue,
		KubeClient:       kubeClient,
	}
	cluster.logger = logger.WithField("pkg", "cluster").WithField("cluster-name", cluster.clusterName())
	cluster.teamsAPIClient = teams.NewTeamsAPI(cfg.OpConfig.TeamsAPIUrl, logger)
	cluster.oauthTokenGetter = NewSecretOauthTokenGetter(&kubeClient, cfg.OpConfig.OAuthTokenSecretName)
	cluster.patroni = patroni.New(cluster.logger)

	return cluster
}

func (c *Cluster) clusterName() spec.NamespacedName {
	return util.NameFromMeta(c.ObjectMeta)
}

func (c *Cluster) clusterNamespace() string {
	return c.ObjectMeta.Namespace
}

func (c *Cluster) teamName() string {
	// TODO: check Teams API for the actual name (in case the user passes an integer Id).
	return c.Spec.TeamID
}

func (c *Cluster) setProcessName(procName string, args ...interface{}) {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	c.currentProcess = spec.Process{
		Name:      fmt.Sprintf(procName, args...),
		StartTime: time.Now(),
	}
}

func (c *Cluster) setStatus(status spec.PostgresStatus) {
	c.Status = status
	b, err := json.Marshal(status)
	if err != nil {
		c.logger.Fatalf("could not marshal status: %v", err)
	}
	request := []byte(fmt.Sprintf(`{"status": %s}`, string(b))) //TODO: Look into/wait for k8s go client methods

	_, err = c.KubeClient.CRDREST.Patch(types.MergePatchType).
		Namespace(c.Namespace).
		Resource(constants.CRDResource).
		Name(c.Name).
		Body(request).
		DoRaw()

	if k8sutil.ResourceNotFound(err) {
		c.logger.Warningf("could not set %q status for the non-existing cluster", status)
		return
	}

	if err != nil {
		c.logger.Warningf("could not set %q status for the cluster: %v", status, err)
	}
}

// initUsers populates c.systemUsers and c.pgUsers maps.
func (c *Cluster) initUsers() error {
	c.setProcessName("initializing users")

	// clear our the previous state of the cluster users (in case we are running a sync).
	c.systemUsers = map[string]spec.PgUser{}
	c.pgUsers = map[string]spec.PgUser{}

	c.initSystemUsers()

	if err := c.initInfrastructureRoles(); err != nil {
		return fmt.Errorf("could not init infrastructure roles: %v", err)
	}

	if err := c.initRobotUsers(); err != nil {
		return fmt.Errorf("could not init robot users: %v", err)
	}

	if err := c.initHumanUsers(); err != nil {
		return fmt.Errorf("could not init human users: %v", err)
	}

	return nil
}

// Create creates the new kubernetes objects associated with the cluster.
func (c *Cluster) Create() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		err error

		service *v1.Service
		ep      *v1.Endpoints
		ss      *v1beta1.StatefulSet
	)

	defer func() {
		if err == nil {
			c.setStatus(spec.ClusterStatusRunning) //TODO: are you sure it's running?
		} else {
			c.setStatus(spec.ClusterStatusAddFailed)
		}
	}()

	c.setStatus(spec.ClusterStatusCreating)

	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			continue
		}
		if c.Endpoints[role] != nil {
			return fmt.Errorf("%s endpoint already exists in the cluster", role)
		}
		ep, err = c.createEndpoint(role)
		if err != nil {
			return fmt.Errorf("could not create %s endpoint: %v", role, err)
		}
		c.logger.Infof("endpoint %q has been successfully created", util.NameFromMeta(ep.ObjectMeta))

		if c.Services[role] != nil {
			return fmt.Errorf("service already exists in the cluster")
		}
		service, err = c.createService(role)
		if err != nil {
			return fmt.Errorf("could not create %s service: %v", role, err)
		}
		c.logger.Infof("%s service %q has been successfully created", role, util.NameFromMeta(service.ObjectMeta))
	}

	if err = c.initUsers(); err != nil {
		return err
	}
	c.logger.Infof("users have been initialized")

	if err = c.syncSecrets(); err != nil {
		return fmt.Errorf("could not create secrets: %v", err)
	}
	c.logger.Infof("secrets have been successfully created")

	if c.PodDisruptionBudget != nil {
		return fmt.Errorf("pod disruption budget already exists in the cluster")
	}
	pdb, err := c.createPodDisruptionBudget()
	if err != nil {
		return fmt.Errorf("could not create pod disruption budget: %v", err)
	}
	c.logger.Infof("pod disruption budget %q has been successfully created", util.NameFromMeta(pdb.ObjectMeta))

	if c.Statefulset != nil {
		return fmt.Errorf("statefulset already exists in the cluster")
	}
	ss, err = c.createStatefulSet()
	if err != nil {
		return fmt.Errorf("could not create statefulset: %v", err)
	}
	c.logger.Infof("statefulset %q has been successfully created", util.NameFromMeta(ss.ObjectMeta))

	c.logger.Info("waiting for the cluster being ready")

	if err = c.waitStatefulsetPodsReady(); err != nil {
		c.logger.Errorf("failed to create cluster: %v", err)
		return err
	}
	c.logger.Infof("pods are ready")

	// create database objects unless we are running without pods or disabled that feature explicitely
	if !(c.databaseAccessDisabled() || c.getNumberOfInstances(&c.Spec) <= 0) {
		if err = c.createRoles(); err != nil {
			return fmt.Errorf("could not create users: %v", err)
		}
		c.logger.Infof("users have been successfully created")

		if err = c.syncDatabases(); err != nil {
			return fmt.Errorf("could not sync databases: %v", err)
		}
		c.logger.Infof("databases have been successfully created")
	}

	if err := c.listResources(); err != nil {
		c.logger.Errorf("could not list resources: %v", err)
	}

	return nil
}

func (c *Cluster) compareStatefulSetWith(statefulSet *v1beta1.StatefulSet) *compareStatefulsetResult {
	reasons := make([]string, 0)
	var match, needsRollUpdate, needsReplace bool

	match = true
	//TODO: improve me
	if *c.Statefulset.Spec.Replicas != *statefulSet.Spec.Replicas {
		match = false
		reasons = append(reasons, "new statefulset's number of replicas doesn't match the current one")
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) != len(statefulSet.Spec.Template.Spec.Containers) {
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's container specification doesn't match the current one")
	} else {
		needsRollUpdate, reasons = c.compareContainers(c.Statefulset, statefulSet)
	}
	if len(c.Statefulset.Spec.Template.Spec.Containers) == 0 {
		c.logger.Warningf("statefulset %q has no container", util.NameFromMeta(c.Statefulset.ObjectMeta))
		return &compareStatefulsetResult{}
	}
	// In the comparisons below, the needsReplace and needsRollUpdate flags are never reset, since checks fall through
	// and the combined effect of all the changes should be applied.
	// TODO: log all reasons for changing the statefulset, not just the last one.
	// TODO: make sure this is in sync with generatePodTemplate, ideally by using the same list of fields to generate
	// the template and the diff
	if c.Statefulset.Spec.Template.Spec.ServiceAccountName != statefulSet.Spec.Template.Spec.ServiceAccountName {
		needsReplace = true
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's serviceAccountName service asccount name doesn't match the current one")
	}
	if *c.Statefulset.Spec.Template.Spec.TerminationGracePeriodSeconds != *statefulSet.Spec.Template.Spec.TerminationGracePeriodSeconds {
		needsReplace = true
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's terminationGracePeriodSeconds  doesn't match the current one")
	}
	if !reflect.DeepEqual(c.Statefulset.Spec.Template.Spec.Affinity, statefulSet.Spec.Template.Spec.Affinity) {
		needsReplace = true
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's pod affinity doesn't match the current one")
	}

	// Some generated fields like creationTimestamp make it not possible to use DeepCompare on Spec.Template.ObjectMeta
	if !reflect.DeepEqual(c.Statefulset.Spec.Template.Labels, statefulSet.Spec.Template.Labels) {
		needsReplace = true
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's metadata labels doesn't match the current one")
	}
	if !reflect.DeepEqual(c.Statefulset.Spec.Template.Annotations, statefulSet.Spec.Template.Annotations) {
		needsRollUpdate = true
		needsReplace = true
		reasons = append(reasons, "new statefulset's metadata annotations doesn't match the current one")
	}
	if len(c.Statefulset.Spec.VolumeClaimTemplates) != len(statefulSet.Spec.VolumeClaimTemplates) {
		needsReplace = true
		reasons = append(reasons, "new statefulset's volumeClaimTemplates contains different number of volumes to the old one")
	}
	for i := 0; i < len(c.Statefulset.Spec.VolumeClaimTemplates); i++ {
		name := c.Statefulset.Spec.VolumeClaimTemplates[i].Name
		// Some generated fields like creationTimestamp make it not possible to use DeepCompare on ObjectMeta
		if name != statefulSet.Spec.VolumeClaimTemplates[i].Name {
			needsReplace = true
			reasons = append(reasons, fmt.Sprintf("new statefulset's name for volume %d doesn't match the current one", i))
			continue
		}
		if !reflect.DeepEqual(c.Statefulset.Spec.VolumeClaimTemplates[i].Annotations, statefulSet.Spec.VolumeClaimTemplates[i].Annotations) {
			needsReplace = true
			reasons = append(reasons, fmt.Sprintf("new statefulset's annotations for volume %q doesn't match the current one", name))
		}
		if !reflect.DeepEqual(c.Statefulset.Spec.VolumeClaimTemplates[i].Spec, statefulSet.Spec.VolumeClaimTemplates[i].Spec) {
			name := c.Statefulset.Spec.VolumeClaimTemplates[i].Name
			needsReplace = true
			reasons = append(reasons, fmt.Sprintf("new statefulset's volumeClaimTemplates specification for volume %q doesn't match the current one", name))
		}
	}

	if needsRollUpdate || needsReplace {
		match = false
	}

	return &compareStatefulsetResult{match: match, reasons: reasons, rollingUpdate: needsRollUpdate, replace: needsReplace}
}

type ContainerCondition func(a, b v1.Container) bool

type ContainerCheck struct {
	condition ContainerCondition
	reason    string
}

func NewCheck(msg string, cond ContainerCondition) ContainerCheck {
	return ContainerCheck{reason: msg, condition: cond}
}

// compareContainers: compare containers from two stateful sets
// and return:
// * whether or not roll update is needed
// * a list of reasons in a human readable format
func (c *Cluster) compareContainers(setA, setB *v1beta1.StatefulSet) (bool, []string) {
	reasons := make([]string, 0)
	needsRollUpdate := false
	checks := []ContainerCheck{
		NewCheck("new statefulset's container %d name doesn't match the current one",
			func(a, b v1.Container) bool { return a.Name != b.Name }),
		NewCheck("new statefulset's container %d image doesn't match the current one",
			func(a, b v1.Container) bool { return a.Image != b.Image }),
		NewCheck("new statefulset's container %d ports don't match the current one",
			func(a, b v1.Container) bool { return !reflect.DeepEqual(a.Ports, b.Ports) }),
		NewCheck("new statefulset's container %d resources don't match the current ones",
			func(a, b v1.Container) bool { return !compareResources(&a.Resources, &b.Resources) }),
		NewCheck("new statefulset's container %d environment doesn't match the current one",
			func(a, b v1.Container) bool { return !reflect.DeepEqual(a.Env, b.Env) }),
		NewCheck("new statefulset's container %d environment sources don't match the current one",
			func(a, b v1.Container) bool { return !reflect.DeepEqual(a.EnvFrom, b.EnvFrom) }),
	}

	for index, containerA := range setA.Spec.Template.Spec.Containers {
		containerB := setB.Spec.Template.Spec.Containers[index]
		for _, check := range checks {
			if check.condition(containerA, containerB) {
				needsRollUpdate = true
				reasons = append(reasons, fmt.Sprintf(check.reason, index))
			}
		}
	}

	return needsRollUpdate, reasons
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
		return len(a.Requests) == 0
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

// Update changes Kubernetes objects according to the new specification. Unlike the sync case, the missing object.
// (i.e. service) is treated as an error.
func (c *Cluster) Update(oldSpec, newSpec *spec.Postgresql) error {
	updateFailed := false

	c.mu.Lock()
	defer c.mu.Unlock()

	c.setStatus(spec.ClusterStatusUpdating)
	c.setSpec(newSpec)

	defer func() {
		if updateFailed {
			c.setStatus(spec.ClusterStatusUpdateFailed)
		} else if c.Status != spec.ClusterStatusRunning {
			c.setStatus(spec.ClusterStatusRunning)
		}
	}()

	if oldSpec.Spec.PgVersion != newSpec.Spec.PgVersion { // PG versions comparison
		c.logger.Warningf("postgresql version change(%q -> %q) has no effect", oldSpec.Spec.PgVersion, newSpec.Spec.PgVersion)
		//we need that hack to generate statefulset with the old version
		newSpec.Spec.PgVersion = oldSpec.Spec.PgVersion
	}

	// Service
	if !reflect.DeepEqual(c.generateService(Master, &oldSpec.Spec), c.generateService(Master, &newSpec.Spec)) ||
		!reflect.DeepEqual(c.generateService(Replica, &oldSpec.Spec), c.generateService(Replica, &newSpec.Spec)) ||
		oldSpec.Spec.ReplicaLoadBalancer != newSpec.Spec.ReplicaLoadBalancer {
		c.logger.Debugf("syncing services")
		if err := c.syncServices(); err != nil {
			c.logger.Errorf("could not sync services: %v", err)
			updateFailed = true
		}
	}

	if !reflect.DeepEqual(oldSpec.Spec.Users, newSpec.Spec.Users) {
		c.logger.Debugf("syncing secrets")
		if err := c.initUsers(); err != nil {
			c.logger.Errorf("could not init users: %v", err)
			updateFailed = true
		}

		c.logger.Debugf("syncing secrets")

		//TODO: mind the secrets of the deleted/new users
		if err := c.syncSecrets(); err != nil {
			c.logger.Errorf("could not sync secrets: %v", err)
			updateFailed = true
		}
	}

	// Volume
	if oldSpec.Spec.Size != newSpec.Spec.Size {
		c.logger.Debugf("syncing persistent volumes")
		c.logVolumeChanges(oldSpec.Spec.Volume, newSpec.Spec.Volume)

		if err := c.syncVolumes(); err != nil {
			c.logger.Errorf("could not sync persistent volumes: %v", err)
			updateFailed = true
		}
	}

	// Statefulset
	func() {
		oldSs, err := c.generateStatefulSet(&oldSpec.Spec)
		if err != nil {
			c.logger.Errorf("could not generate old statefulset spec: %v", err)
			updateFailed = true
			return
		}

		newSs, err := c.generateStatefulSet(&newSpec.Spec)
		if err != nil {
			c.logger.Errorf("could not generate new statefulset spec: %v", err)
			updateFailed = true
			return
		}

		if !reflect.DeepEqual(oldSs, newSs) {
			c.logger.Debugf("syncing statefulsets")
			// TODO: avoid generating the StatefulSet object twice by passing it to syncStatefulSet
			if err := c.syncStatefulSet(); err != nil {
				c.logger.Errorf("could not sync statefulsets: %v", err)
				updateFailed = true
			}
		}
	}()

	// Roles and Databases
	if !(c.databaseAccessDisabled() || c.getNumberOfInstances(&c.Spec) <= 0) {
		c.logger.Debugf("syncing roles")
		if err := c.syncRoles(); err != nil {
			c.logger.Errorf("could not sync roles: %v", err)
			updateFailed = true
		}
		if !reflect.DeepEqual(oldSpec.Spec.Databases, newSpec.Spec.Databases) {
			c.logger.Infof("syncing databases")
			if err := c.syncDatabases(); err != nil {
				c.logger.Errorf("could not sync databases: %v", err)
				updateFailed = true
			}
		}
	}

	return nil
}

// Delete deletes the cluster and cleans up all objects associated with it (including statefulsets).
// The deletion order here is somewhat significant, because Patroni, when running with the Kubernetes
// DCS, reuses the master's endpoint to store the leader related metadata. If we remove the endpoint
// before the pods, it will be re-created by the current master pod and will remain, obstructing the
// creation of the new cluster with the same name. Therefore, the endpoints should be deleted last.
func (c *Cluster) Delete() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.deleteStatefulSet(); err != nil {
		return fmt.Errorf("could not delete statefulset: %v", err)
	}

	for _, obj := range c.Secrets {
		if delete, user := c.shouldDeleteSecret(obj); !delete {
			c.logger.Infof("not removing secret %q for the system user %q", obj.GetName(), user)
			continue
		}
		if err := c.deleteSecret(obj); err != nil {
			return fmt.Errorf("could not delete secret: %v", err)
		}
	}

	if err := c.deletePodDisruptionBudget(); err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}

	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			continue
		}

		if err := c.deleteEndpoint(role); err != nil {
			return fmt.Errorf("could not delete %s endpoint: %v", role, err)
		}

		if err := c.deleteService(role); err != nil {
			return fmt.Errorf("could not delete %s service: %v", role, err)
		}
	}

	if err := c.deletePatroniClusterObjects(); err != nil {
		return fmt.Errorf("could not remove leftover patroni objects; %v", err)
	}

	return nil
}

// ReceivePodEvent is called back by the controller in order to add the cluster's pod event to the queue.
func (c *Cluster) ReceivePodEvent(event spec.PodEvent) {
	if err := c.podEventsQueue.Add(event); err != nil {
		c.logger.Errorf("error when receiving pod events: %v", err)
	}
}

func (c *Cluster) processPodEvent(obj interface{}) error {
	event, ok := obj.(spec.PodEvent)
	if !ok {
		return fmt.Errorf("could not cast to PodEvent")
	}

	c.podSubscribersMu.RLock()
	subscriber, ok := c.podSubscribers[event.PodName]
	c.podSubscribersMu.RUnlock()
	if ok {
		subscriber <- event
	}

	return nil
}

// Run starts the pod event dispatching for the given cluster.
func (c *Cluster) Run(stopCh <-chan struct{}) {
	go c.processPodEventQueue(stopCh)
}

func (c *Cluster) processPodEventQueue(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		default:
			if _, err := c.podEventsQueue.Pop(cache.PopProcessFunc(c.processPodEvent)); err != nil {
				c.logger.Errorf("error when processing pod event queue %v", err)
			}
		}
	}
}

func (c *Cluster) initSystemUsers() {
	// We don't actually use that to create users, delegating this
	// task to Patroni. Those definitions are only used to create
	// secrets, therefore, setting flags like SUPERUSER or REPLICATION
	// is not necessary here
	c.systemUsers[constants.SuperuserKeyName] = spec.PgUser{
		Origin:   spec.RoleOriginSystem,
		Name:     c.OpConfig.SuperUsername,
		Password: util.RandomPassword(constants.PasswordLength),
	}
	c.systemUsers[constants.ReplicationUserKeyName] = spec.PgUser{
		Origin:   spec.RoleOriginSystem,
		Name:     c.OpConfig.ReplicationUsername,
		Password: util.RandomPassword(constants.PasswordLength),
	}
}

func (c *Cluster) initRobotUsers() error {
	for username, userFlags := range c.Spec.Users {
		if !isValidUsername(username) {
			return fmt.Errorf("invalid username: %q", username)
		}

		if c.shouldAvoidProtectedOrSystemRole(username, "manifest robot role") {
			continue
		}
		flags, err := normalizeUserFlags(userFlags)
		if err != nil {
			return fmt.Errorf("invalid flags for user %q: %v", username, err)
		}
		if _, present := c.pgUsers[username]; !present {
			c.pgUsers[username] = spec.PgUser{
				Origin:   spec.RoleOriginManifest,
				Name:     username,
				Password: util.RandomPassword(constants.PasswordLength),
				Flags:    flags,
			}
		} else {
			// avoid overwriting the password if the user is already there. The flags should be
			// merged here, but since there is no mechanism to define them for non-robot roles
			// they are assigned from the robot user.
			c.logger.Debugf("merging manifest and infrastructure user %q data", username)
			user := c.pgUsers[username]
			user.Flags = flags
			c.pgUsers[username] = user
		}
	}

	return nil
}

func (c *Cluster) initHumanUsers() error {
	teamMembers, err := c.getTeamMembers()
	if err != nil {
		return fmt.Errorf("could not get list of team members: %v", err)
	}
	for _, username := range teamMembers {
		flags := []string{constants.RoleFlagLogin}
		memberOf := []string{c.OpConfig.PamRoleName}

		if c.shouldAvoidProtectedOrSystemRole(username, "API role") {
			continue
		}
		if c.OpConfig.EnableTeamSuperuser {
			flags = append(flags, constants.RoleFlagSuperuser)
		} else {
			if c.OpConfig.TeamAdminRole != "" {
				memberOf = append(memberOf, c.OpConfig.TeamAdminRole)
			}
		}

		if _, present := c.pgUsers[username]; present {
			c.logger.Warnf("overwriting existing user %q with the data from the teams API", username)
		}

		c.pgUsers[username] = spec.PgUser{
			Origin:     spec.RoleOriginTeamsAPI,
			Name:       username,
			Flags:      flags,
			MemberOf:   memberOf,
			Parameters: c.OpConfig.TeamAPIRoleConfiguration,
		}
	}

	return nil
}

func (c *Cluster) initInfrastructureRoles() error {
	// add infrastucture roles from the operator's definition
	for username, data := range c.InfrastructureRoles {
		if !isValidUsername(username) {
			return fmt.Errorf("invalid username: '%v'", username)
		}
		if c.shouldAvoidProtectedOrSystemRole(username, "infrastructure role") {
			continue
		}
		flags, err := normalizeUserFlags(data.Flags)
		if err != nil {
			return fmt.Errorf("invalid flags for user '%v': %v", username, err)
		}
		data.Flags = flags
		c.pgUsers[username] = data
	}
	return nil
}

func (c *Cluster) shouldAvoidProtectedOrSystemRole(username, purpose string) bool {
	if c.isProtectedUsername(username) {
		c.logger.Warnf("cannot initialize a new %s with the name of the protected user %q", purpose, username)
		return true
	}
	if c.isSystemUsername(username) {
		c.logger.Warnf("cannot initialize a new %s with the name of the system user %q", purpose, username)
		return true
	}
	return false
}

// GetCurrentProcess provides name of the last process of the cluster
func (c *Cluster) GetCurrentProcess() spec.Process {
	c.processMu.RLock()
	defer c.processMu.RUnlock()

	return c.currentProcess
}

// GetStatus provides status of the cluster
func (c *Cluster) GetStatus() *spec.ClusterStatus {
	return &spec.ClusterStatus{
		Cluster: c.Spec.ClusterName,
		Team:    c.Spec.TeamID,
		Status:  c.Status,
		Spec:    c.Spec,

		MasterService:       c.GetServiceMaster(),
		ReplicaService:      c.GetServiceReplica(),
		MasterEndpoint:      c.GetEndpointMaster(),
		ReplicaEndpoint:     c.GetEndpointReplica(),
		StatefulSet:         c.GetStatefulSet(),
		PodDisruptionBudget: c.GetPodDisruptionBudget(),
		CurrentProcess:      c.GetCurrentProcess(),

		Error: c.Error,
	}
}

// ManualFailover does manual failover to a candidate pod
func (c *Cluster) ManualFailover(curMaster *v1.Pod, candidate spec.NamespacedName) error {
	c.logger.Debugf("failing over from %q to %q", curMaster.Name, candidate)
	podLabelErr := make(chan error)
	stopCh := make(chan struct{})
	defer close(podLabelErr)

	go func() {
		ch := c.registerPodSubscriber(candidate)
		defer c.unregisterPodSubscriber(candidate)

		role := Master

		_, err := c.waitForPodLabel(ch, &role)

		select {
		case <-stopCh:
		case podLabelErr <- err:
		}
	}()

	if err := c.patroni.Failover(curMaster, candidate.Name); err != nil {
		close(stopCh)
		return fmt.Errorf("could not failover: %v", err)
	}
	c.logger.Debugf("successfully failed over from %q to %q", curMaster.Name, candidate)

	defer close(stopCh)

	if err := <-podLabelErr; err != nil {
		return fmt.Errorf("could not get master pod label: %v", err)
	}

	return nil
}

// Lock locks the cluster
func (c *Cluster) Lock() {
	c.mu.Lock()
}

// Unlock unlocks the cluster
func (c *Cluster) Unlock() {
	c.mu.Unlock()
}

func (c *Cluster) shouldDeleteSecret(secret *v1.Secret) (delete bool, userName string) {
	secretUser := string(secret.Data["username"])
	return (secretUser != c.OpConfig.ReplicationUsername && secretUser != c.OpConfig.SuperUsername), secretUser
}

type simpleActionWithResult func() error

type ClusterObjectGet func(name string) (spec.NamespacedName, error)

type ClusterObjectDelete func(name string) error

func (c *Cluster) deletePatroniClusterObjects() error {
	// TODO: figure out how to remove leftover patroni objects in other cases
	if !c.patroniUsesKubernetes() {
		c.logger.Infof("not cleaning up Etcd Patroni objects on cluster delete")
	}
	c.logger.Debugf("removing leftover Patroni objects (endpoints or configmaps)")
	for _, deleter := range []simpleActionWithResult{c.deletePatroniClusterEndpoints, c.deletePatroniClusterConfigMaps} {
		if err := deleter(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cluster) deleteClusterObject(
	get ClusterObjectGet,
	del ClusterObjectDelete,
	objType string) error {
	for _, suffix := range patroniObjectSuffixes {
		name := fmt.Sprintf("%s-%s", c.Name, suffix)

		if namespacedName, err := get(name); err == nil {
			c.logger.Debugf("deleting Patroni cluster object %q with name %q",
				objType, namespacedName)

			if err = del(name); err != nil {
				return fmt.Errorf("could not Patroni delete cluster object %q with name %q: %v",
					objType, namespacedName, err)
			}

		} else if !k8sutil.ResourceNotFound(err) {
			return fmt.Errorf("could not fetch Patroni Endpoint %q: %v",
				namespacedName, err)
		}
	}
	return nil
}

func (c *Cluster) deletePatroniClusterEndpoints() error {
	get := func(name string) (spec.NamespacedName, error) {
		ep, err := c.KubeClient.Endpoints(c.Namespace).Get(name, metav1.GetOptions{})
		return util.NameFromMeta(ep.ObjectMeta), err
	}

	delete := func(name string) error {
		return c.KubeClient.Endpoints(c.Namespace).Delete(name, c.deleteOptions)
	}

	return c.deleteClusterObject(get, delete, "endpoint")
}

func (c *Cluster) deletePatroniClusterConfigMaps() error {
	get := func(name string) (spec.NamespacedName, error) {
		cm, err := c.KubeClient.ConfigMaps(c.Namespace).Get(name, metav1.GetOptions{})
		return util.NameFromMeta(cm.ObjectMeta), err
	}

	delete := func(name string) error {
		return c.KubeClient.ConfigMaps(c.Namespace).Delete(name, c.deleteOptions)
	}

	return c.deleteClusterObject(get, delete, "configmap")
}
