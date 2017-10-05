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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
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
	"github.com/zalando-incubator/postgres-operator/pkg/util/volumes"
)

var (
	alphaNumericRegexp = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9]*$")
	databaseNameRegexp = regexp.MustCompile("^[a-zA-Z_][a-zA-Z0-9_]*$")
	userRegexp         = regexp.MustCompile(`^[a-z0-9]([-_a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-_a-z0-9]*[a-z0-9])?)*$`)
)

// Config contains operator-wide clients and configuration used from a cluster. TODO: remove struct duplication.
type Config struct {
	OpConfig            config.Config
	RestConfig          *rest.Config
	InfrastructureRoles map[string]spec.PgUser // inherited from the controller
}

type kubeResources struct {
	Services    map[PostgresRole]*v1.Service
	Endpoint    *v1.Endpoints
	Secrets     map[types.UID]*v1.Secret
	Statefulset *v1beta1.StatefulSet
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
	masterLess       bool
	userSyncStrategy spec.UserSyncer
	deleteOptions    *metav1.DeleteOptions
	podEventsQueue   *cache.FIFO

	teamsAPIClient *teams.API
	KubeClient     k8sutil.KubernetesClient //TODO: move clients to the better place?
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
		Config:           cfg,
		Postgresql:       pgSpec,
		pgUsers:          make(map[string]spec.PgUser),
		systemUsers:      make(map[string]spec.PgUser),
		podSubscribers:   make(map[spec.NamespacedName]chan spec.PodEvent),
		kubeResources:    kubeResources{Secrets: make(map[types.UID]*v1.Secret), Services: make(map[PostgresRole]*v1.Service)},
		masterLess:       false,
		userSyncStrategy: users.DefaultUserSyncStrategy{},
		deleteOptions:    &metav1.DeleteOptions{OrphanDependents: &orphanDependents},
		podEventsQueue:   podEventsQueue,
		KubeClient:       kubeClient,
		teamsAPIClient:   teams.NewTeamsAPI(cfg.OpConfig.TeamsAPIUrl, logger),
	}
	cluster.logger = logger.WithField("pkg", "cluster").WithField("cluster-name", cluster.clusterName())
	cluster.patroni = patroni.New(cluster.logger)

	return cluster
}

func (c *Cluster) clusterName() spec.NamespacedName {
	return util.NameFromMeta(c.ObjectMeta)
}

func (c *Cluster) teamName() string {
	// TODO: check Teams API for the actual name (in case the user passes an integer Id).
	return c.Spec.TeamID
}

func (c *Cluster) setStatus(status spec.PostgresStatus) {
	c.Status = status
	b, err := json.Marshal(status)
	if err != nil {
		c.logger.Fatalf("could not marshal status: %v", err)
	}
	request := []byte(fmt.Sprintf(`{"status": %s}`, string(b))) //TODO: Look into/wait for k8s go client methods

	_, err = c.KubeClient.RESTClient.Patch(types.MergePatchType).
		RequestURI(c.GetSelfLink()).
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

	//TODO: service will create endpoint implicitly
	ep, err = c.createEndpoint()
	if err != nil {
		return fmt.Errorf("could not create endpoint: %v", err)
	}
	c.logger.Infof("endpoint %q has been successfully created", util.NameFromMeta(ep.ObjectMeta))

	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			continue
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

	if err = c.applySecrets(); err != nil {
		return fmt.Errorf("could not create secrets: %v", err)
	}
	c.logger.Infof("secrets have been successfully created")

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

	if !(c.masterLess || c.databaseAccessDisabled()) {
		if err := c.createRoles(); err != nil {
			return fmt.Errorf("could not create users: %v", err)
		}

		if err := c.createDatabases(); err != nil {
			return fmt.Errorf("could not create databases: %v", err)
		}

		c.logger.Infof("users have been successfully created")
	} else {
		if c.masterLess {
			c.logger.Warnln("cluster is masterless")
		}
	}

	err = c.listResources()
	if err != nil {
		c.logger.Errorf("could not list resources: %v", err)
	}

	return nil
}

func (c *Cluster) sameServiceWith(role PostgresRole, service *v1.Service) (match bool, reason string) {
	//TODO: improve comparison
	if c.Services[role].Spec.Type != service.Spec.Type {
		return false, fmt.Sprintf("new %s service's type %q doesn't match the current one %q",
			role, service.Spec.Type, c.Services[role].Spec.Type)
	}
	oldSourceRanges := c.Services[role].Spec.LoadBalancerSourceRanges
	newSourceRanges := service.Spec.LoadBalancerSourceRanges
	/* work around Kubernetes 1.6 serializing [] as nil. See https://github.com/kubernetes/kubernetes/issues/43203 */
	if (len(oldSourceRanges) == 0) && (len(newSourceRanges) == 0) {
		return true, ""
	}
	if !reflect.DeepEqual(oldSourceRanges, newSourceRanges) {
		return false, fmt.Sprintf("new %s service's LoadBalancerSourceRange doesn't match the current one", role)
	}

	oldDNSAnnotation := c.Services[role].Annotations[constants.ZalandoDNSNameAnnotation]
	newDNSAnnotation := service.Annotations[constants.ZalandoDNSNameAnnotation]
	if oldDNSAnnotation != newDNSAnnotation {
		return false, fmt.Sprintf("new %s service's %q annotation doesn't match the current one", role, constants.ZalandoDNSNameAnnotation)
	}

	return true, ""
}

func (c *Cluster) sameVolumeWith(volume spec.Volume) (match bool, reason string) {
	if !reflect.DeepEqual(c.Spec.Volume, volume) {
		reason = "new volume's specification doesn't match the current one"
	} else {
		match = true
	}
	return
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

	container1 := c.Statefulset.Spec.Template.Spec.Containers[0]
	container2 := statefulSet.Spec.Template.Spec.Containers[0]
	if container1.Image != container2.Image {
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's container image doesn't match the current one")
	}

	if !reflect.DeepEqual(container1.Ports, container2.Ports) {
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's container ports don't match the current one")
	}

	if !compareResources(&container1.Resources, &container2.Resources) {
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's container resources don't match the current ones")
	}
	if !reflect.DeepEqual(container1.Env, container2.Env) {
		needsRollUpdate = true
		reasons = append(reasons, "new statefulset's container environment doesn't match the current one")
	}

	if needsRollUpdate || needsReplace {
		match = false
	}
	return &compareStatefulsetResult{match: match, reasons: reasons, rollingUpdate: needsRollUpdate, replace: needsReplace}
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

// Update changes Kubernetes objects according to the new specification. Unlike the sync case, the missing object.
// (i.e. service) is treated as an error.
func (c *Cluster) Update(newSpec *spec.Postgresql) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.setStatus(spec.ClusterStatusUpdating)
	c.logger.Debugf("cluster update from version %q to %q",
		c.ResourceVersion, newSpec.ResourceVersion)

	/* Make sure we update when this function exists */
	defer func() {
		c.Postgresql = *newSpec
	}()

	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica {
			if !newSpec.Spec.ReplicaLoadBalancer {
				// old spec had a load balancer, but the new one doesn't
				if c.Spec.ReplicaLoadBalancer {
					err := c.deleteService(role)
					if err != nil {
						return fmt.Errorf("could not delete obsolete %s service: %v", role, err)
					}
					c.logger.Infof("deleted obsolete %s service", role)
				}
			} else {
				if !c.Spec.ReplicaLoadBalancer {
					// old spec didn't have a load balancer, but the one does
					service, err := c.createService(role)
					if err != nil {
						return fmt.Errorf("could not create new %s service: %v", role, err)
					}
					c.logger.Infof("%s service %q has been created", role, util.NameFromMeta(service.ObjectMeta))
				}
			}
			// only proceed further if both old and new load balancer were present
			if !(newSpec.Spec.ReplicaLoadBalancer && c.Spec.ReplicaLoadBalancer) {
				continue
			}
		}
		newService := c.generateService(role, &newSpec.Spec)
		if match, reason := c.sameServiceWith(role, newService); !match {
			c.logServiceChanges(role, c.Services[role], newService, true, reason)
			if err := c.updateService(role, newService); err != nil {
				c.setStatus(spec.ClusterStatusUpdateFailed)
				return fmt.Errorf("could not update %s service: %v", role, err)
			}
			c.logger.Infof("%s service %q has been updated", role, util.NameFromMeta(c.Services[role].ObjectMeta))
		}
	}

	newStatefulSet, err := c.generateStatefulSet(newSpec.Spec)
	if err != nil {
		return fmt.Errorf("could not generate statefulset: %v", err)
	}
	cmp := c.compareStatefulSetWith(newStatefulSet)

	if !cmp.match {
		c.logStatefulSetChanges(c.Statefulset, newStatefulSet, true, cmp.reasons)
		//TODO: mind the case of updating allowedSourceRanges
		if !cmp.replace {
			if err := c.updateStatefulSet(newStatefulSet); err != nil {
				c.setStatus(spec.ClusterStatusUpdateFailed)
				return fmt.Errorf("could not upate statefulset: %v", err)
			}
		} else {
			if err := c.replaceStatefulSet(newStatefulSet); err != nil {
				c.setStatus(spec.ClusterStatusUpdateFailed)
				return fmt.Errorf("could not replace statefulset: %v", err)
			}
		}
		//TODO: if there is a change in numberOfInstances, make sure Pods have been created/deleted
		c.logger.Infof("statefulset %q has been updated", util.NameFromMeta(c.Statefulset.ObjectMeta))
	}

	if c.Spec.PgVersion != newSpec.Spec.PgVersion { // PG versions comparison
		c.logger.Warningf("postgresql version change(%q -> %q) is not allowed",
			c.Spec.PgVersion, newSpec.Spec.PgVersion)
		//TODO: rewrite pg version in tpr spec
	}

	if cmp.rollingUpdate {
		c.logger.Infof("rolling update is needed")
		// TODO: wait for actual streaming to the replica
		if err := c.recreatePods(); err != nil {
			c.setStatus(spec.ClusterStatusUpdateFailed)
			return fmt.Errorf("could not recreate pods: %v", err)
		}
		c.logger.Infof("rolling update has been finished")
	}

	if match, reason := c.sameVolumeWith(newSpec.Spec.Volume); !match {
		c.logVolumeChanges(c.Spec.Volume, newSpec.Spec.Volume, reason)
		if err := c.resizeVolumes(newSpec.Spec.Volume, []volumes.VolumeResizer{&volumes.EBSVolumeResizer{}}); err != nil {
			return fmt.Errorf("could not update volumes: %v", err)
		}
		c.logger.Infof("volumes have been updated successfully")
	}

	c.setStatus(spec.ClusterStatusRunning)

	return nil
}

// Delete deletes the cluster and cleans up all objects associated with it (including statefulsets).
func (c *Cluster) Delete() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.deleteEndpoint(); err != nil {
		return fmt.Errorf("could not delete endpoint: %v", err)
	}

	for _, role := range []PostgresRole{Master, Replica} {
		if role == Replica && !c.Spec.ReplicaLoadBalancer {
			continue
		}
		if err := c.deleteService(role); err != nil {
			return fmt.Errorf("could not delete %s service: %v", role, err)
		}
	}

	if err := c.deleteStatefulSet(); err != nil {
		return fmt.Errorf("could not delete statefulset: %v", err)
	}

	for _, obj := range c.Secrets {
		if err := c.deleteSecret(obj); err != nil {
			return fmt.Errorf("could not delete secret: %v", err)
		}
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
			return fmt.Errorf("invalid username: '%v'", username)
		}

		flags, err := normalizeUserFlags(userFlags)
		if err != nil {
			return fmt.Errorf("invalid flags for user '%v': %v", username, err)
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
		return fmt.Errorf("could not get list of team members: %v", err)
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
			return fmt.Errorf("invalid username: '%v'", username)
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

// GetStatus provides status of the cluster
func (c *Cluster) GetStatus() *spec.ClusterStatus {
	return &spec.ClusterStatus{
		Cluster: c.Spec.ClusterName,
		Team:    c.Spec.TeamID,
		Status:  c.Status,
		Spec:    c.Spec,

		MasterService:  c.GetServiceMaster(),
		ReplicaService: c.GetServiceReplica(),
		Endpoint:       c.GetEndpoint(),
		StatefulSet:    c.GetStatefulSet(),

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

		select {
		case <-stopCh:
		case podLabelErr <- c.waitForPodLabel(ch, &role):
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
