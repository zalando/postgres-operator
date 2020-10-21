package cluster

import (
	"context"
	"fmt"

	"github.com/r3labs/diff"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
)

// K8S objects that are belongs to a connection pooler
type ConnectionPoolerObjects struct {
	Deployment  *appsv1.Deployment
	Service     *v1.Service
	Name        string
	ClusterName string
	Namespace   string
	// It could happen that a connection pooler was enabled, but the operator
	// was not able to properly process a corresponding event or was restarted.
	// In this case we will miss missing/require situation and a lookup function
	// will not be installed. To avoid synchronizing it all the time to prevent
	// this, we can remember the result in memory at least until the next
	// restart.
	LookupFunction bool
	// Careful with referencing cluster.spec this object pointer changes during runtime and lifetime of cluster
	// Cluster *cluster
}

func (c *Cluster) connectionPoolerName(role PostgresRole) string {
	name := c.Name + "-pooler"
	if role == Replica {
		name = name + "-repl"
	}
	return name
}

// isConnectionPoolerEnabled
func (c *Cluster) needConnectionPooler() bool {
	return needMasterConnectionPoolerWorker(&c.Spec) || needReplicaConnectionPoolerWorker(&c.Spec)
}

func (c *Cluster) needMasterConnectionPooler() bool {
	return needMasterConnectionPoolerWorker(&c.Spec)
}

func needMasterConnectionPoolerWorker(spec *acidv1.PostgresSpec) bool {
	return (nil != spec.EnableConnectionPooler && *spec.EnableConnectionPooler) || (spec.ConnectionPooler != nil && spec.EnableConnectionPooler == nil)
}

func (c *Cluster) needReplicaConnectionPooler() bool {
	return needReplicaConnectionPoolerWorker(&c.Spec)
}

func needReplicaConnectionPoolerWorker(spec *acidv1.PostgresSpec) bool {
	return spec.EnableReplicaConnectionPooler != nil && *spec.EnableReplicaConnectionPooler
}

// RolesConnectionPooler gives the list of roles which need connection pooler
func (c *Cluster) RolesConnectionPooler() []PostgresRole {
	roles := make([]PostgresRole, 2)

	if needMasterConnectionPoolerWorker(&c.Spec) {
		roles = append(roles, Master)
	}
	if needMasterConnectionPoolerWorker(&c.Spec) {
		roles = append(roles, Replica)
	}
	return roles
}

// Return connection pooler labels selector, which should from one point of view
// inherit most of the labels from the cluster itself, but at the same time
// have e.g. different `application` label, so that recreatePod operation will
// not interfere with it (it lists all the pods via labels, and if there would
// be no difference, it will recreate also pooler pods).
func (c *Cluster) connectionPoolerLabelsSelector(role PostgresRole) *metav1.LabelSelector {
	connectionPoolerLabels := labels.Set(map[string]string{})

	extraLabels := labels.Set(map[string]string{
		"connection-pooler-name": c.connectionPoolerName(role),
		"application":            "db-connection-pooler",
		"role":                   string(role),
		"cluster-name":           c.Name,
		"Namespace":              c.Namespace,
	})

	connectionPoolerLabels = labels.Merge(connectionPoolerLabels, c.labelsSet(false))
	connectionPoolerLabels = labels.Merge(connectionPoolerLabels, extraLabels)

	return &metav1.LabelSelector{
		MatchLabels:      connectionPoolerLabels,
		MatchExpressions: nil,
	}
}

// Prepare the database for connection pooler to be used, i.e. install lookup
// function (do it first, because it should be fast and if it didn't succeed,
// it doesn't makes sense to create more K8S objects. At this moment we assume
// that necessary connection pooler user exists.
//
// After that create all the objects for connection pooler, namely a deployment
// with a chosen pooler and a service to expose it.

// have connectionpooler name in the cp object to have it immutable name
// add these cp related functions to a new cp file
// opConfig, cluster, and database name
func (c *Cluster) createConnectionPooler(lookup InstallFunction) (SyncReason, error) {
	var reason SyncReason
	c.setProcessName("creating connection pooler")

	//this is essentially sync with nil as oldSpec
	if reason, err := c.syncConnectionPooler(nil, &c.Postgresql, lookup); err != nil {
		return reason, err
	}
	return reason, nil
}

//
// Generate pool size related environment variables.
//
// MAX_DB_CONN would specify the global maximum for connections to a target
// 	database.
//
// MAX_CLIENT_CONN is not configurable at the moment, just set it high enough.
//
// DEFAULT_SIZE is a pool size per db/user (having in mind the use case when
// 	most of the queries coming through a connection pooler are from the same
// 	user to the same db). In case if we want to spin up more connection pooler
// 	instances, take this into account and maintain the same number of
// 	connections.
//
// MIN_SIZE is a pool's minimal size, to prevent situation when sudden workload
// 	have to wait for spinning up a new connections.
//
// RESERVE_SIZE is how many additional connections to allow for a pooler.
func (c *Cluster) getConnectionPoolerEnvVars() []v1.EnvVar {
	spec := &c.Spec
	effectiveMode := util.Coalesce(
		spec.ConnectionPooler.Mode,
		c.OpConfig.ConnectionPooler.Mode)

	numberOfInstances := spec.ConnectionPooler.NumberOfInstances
	if numberOfInstances == nil {
		numberOfInstances = util.CoalesceInt32(
			c.OpConfig.ConnectionPooler.NumberOfInstances,
			k8sutil.Int32ToPointer(1))
	}

	effectiveMaxDBConn := util.CoalesceInt32(
		spec.ConnectionPooler.MaxDBConnections,
		c.OpConfig.ConnectionPooler.MaxDBConnections)

	if effectiveMaxDBConn == nil {
		effectiveMaxDBConn = k8sutil.Int32ToPointer(
			constants.ConnectionPoolerMaxDBConnections)
	}

	maxDBConn := *effectiveMaxDBConn / *numberOfInstances

	defaultSize := maxDBConn / 2
	minSize := defaultSize / 2
	reserveSize := minSize

	return []v1.EnvVar{
		{
			Name:  "CONNECTION_POOLER_PORT",
			Value: fmt.Sprint(pgPort),
		},
		{
			Name:  "CONNECTION_POOLER_MODE",
			Value: effectiveMode,
		},
		{
			Name:  "CONNECTION_POOLER_DEFAULT_SIZE",
			Value: fmt.Sprint(defaultSize),
		},
		{
			Name:  "CONNECTION_POOLER_MIN_SIZE",
			Value: fmt.Sprint(minSize),
		},
		{
			Name:  "CONNECTION_POOLER_RESERVE_SIZE",
			Value: fmt.Sprint(reserveSize),
		},
		{
			Name:  "CONNECTION_POOLER_MAX_CLIENT_CONN",
			Value: fmt.Sprint(constants.ConnectionPoolerMaxClientConnections),
		},
		{
			Name:  "CONNECTION_POOLER_MAX_DB_CONN",
			Value: fmt.Sprint(maxDBConn),
		},
	}
}

func (c *Cluster) generateConnectionPoolerPodTemplate(role PostgresRole) (
	*v1.PodTemplateSpec, error) {
	spec := &c.Spec
	gracePeriod := int64(c.OpConfig.PodTerminateGracePeriod.Seconds())
	resources, err := generateResourceRequirements(
		spec.ConnectionPooler.Resources,
		c.makeDefaultConnectionPoolerResources())

	effectiveDockerImage := util.Coalesce(
		spec.ConnectionPooler.DockerImage,
		c.OpConfig.ConnectionPooler.Image)

	effectiveSchema := util.Coalesce(
		spec.ConnectionPooler.Schema,
		c.OpConfig.ConnectionPooler.Schema)

	if err != nil {
		return nil, fmt.Errorf("could not generate resource requirements: %v", err)
	}

	secretSelector := func(key string) *v1.SecretKeySelector {
		effectiveUser := util.Coalesce(
			spec.ConnectionPooler.User,
			c.OpConfig.ConnectionPooler.User)

		return &v1.SecretKeySelector{
			LocalObjectReference: v1.LocalObjectReference{
				Name: c.credentialSecretName(effectiveUser),
			},
			Key: key,
		}
	}

	envVars := []v1.EnvVar{
		{
			Name:  "PGHOST",
			Value: c.serviceAddress(role),
		},
		{
			Name:  "PGPORT",
			Value: c.servicePort(role),
		},
		{
			Name: "PGUSER",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: secretSelector("username"),
			},
		},
		// the convention is to use the same schema name as
		// connection pooler username
		{
			Name:  "PGSCHEMA",
			Value: effectiveSchema,
		},
		{
			Name: "PGPASSWORD",
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: secretSelector("password"),
			},
		},
	}
	envVars = append(envVars, c.getConnectionPoolerEnvVars()...)

	poolerContainer := v1.Container{
		Name:            connectionPoolerContainer,
		Image:           effectiveDockerImage,
		ImagePullPolicy: v1.PullIfNotPresent,
		Resources:       *resources,
		Ports: []v1.ContainerPort{
			{
				ContainerPort: pgPort,
				Protocol:      v1.ProtocolTCP,
			},
		},
		Env: envVars,
		ReadinessProbe: &v1.Probe{
			Handler: v1.Handler{
				TCPSocket: &v1.TCPSocketAction{
					Port: intstr.IntOrString{IntVal: pgPort},
				},
			},
		},
	}

	podTemplate := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      c.connectionPoolerLabelsSelector(role).MatchLabels,
			Namespace:   c.Namespace,
			Annotations: c.generatePodAnnotations(spec),
		},
		Spec: v1.PodSpec{
			ServiceAccountName:            c.OpConfig.PodServiceAccountName,
			TerminationGracePeriodSeconds: &gracePeriod,
			Containers:                    []v1.Container{poolerContainer},
			// TODO: add tolerations to scheduler pooler on the same node
			// as database
			//Tolerations:                   *tolerationsSpec,
		},
	}

	return podTemplate, nil
}

func (c *Cluster) generateConnectionPoolerDeployment(role PostgresRole) (
	*appsv1.Deployment, error) {
	spec := &c.Spec

	// there are two ways to enable connection pooler, either to specify a
	// connectionPooler section or enableConnectionPooler. In the second case
	// spec.connectionPooler will be nil, so to make it easier to calculate
	// default values, initialize it to an empty structure. It could be done
	// anywhere, but here is the earliest common entry point between sync and
	// create code, so init here.
	if spec.ConnectionPooler == nil {
		spec.ConnectionPooler = &acidv1.ConnectionPooler{}
	}

	podTemplate, err := c.generateConnectionPoolerPodTemplate(role)
	numberOfInstances := spec.ConnectionPooler.NumberOfInstances
	if numberOfInstances == nil {
		numberOfInstances = util.CoalesceInt32(
			c.OpConfig.ConnectionPooler.NumberOfInstances,
			k8sutil.Int32ToPointer(1))
	}

	if *numberOfInstances < constants.ConnectionPoolerMinInstances {
		msg := "Adjusted number of connection pooler instances from %d to %d"
		c.logger.Warningf(msg, numberOfInstances, constants.ConnectionPoolerMinInstances)

		*numberOfInstances = constants.ConnectionPoolerMinInstances
	}

	if err != nil {
		return nil, err
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        c.connectionPoolerName(role),
			Namespace:   c.Namespace,
			Labels:      c.connectionPoolerLabelsSelector(role).MatchLabels,
			Annotations: map[string]string{},
			// make StatefulSet object its owner to represent the dependency.
			// By itself StatefulSet is being deleted with "Orphaned"
			// propagation policy, which means that it's deletion will not
			// clean up this deployment, but there is a hope that this object
			// will be garbage collected if something went wrong and operator
			// didn't deleted it.
			OwnerReferences: c.ownerReferences(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: numberOfInstances,
			Selector: c.connectionPoolerLabelsSelector(role),
			Template: *podTemplate,
		},
	}

	return deployment, nil
}

func (c *Cluster) generateConnectionPoolerService(spec *acidv1.PostgresSpec, role PostgresRole) *v1.Service {

	// there are two ways to enable connection pooler, either to specify a
	// connectionPooler section or enableConnectionPooler. In the second case
	// spec.connectionPooler will be nil, so to make it easier to calculate
	// default values, initialize it to an empty structure. It could be done
	// anywhere, but here is the earliest common entry point between sync and
	// create code, so init here.
	if spec.ConnectionPooler == nil {
		spec.ConnectionPooler = &acidv1.ConnectionPooler{}
	}

	serviceSpec := v1.ServiceSpec{
		Ports: []v1.ServicePort{
			{
				Name:       c.connectionPoolerName(role),
				Port:       pgPort,
				TargetPort: intstr.IntOrString{StrVal: c.servicePort(role)},
			},
		},
		Type: v1.ServiceTypeClusterIP,
		Selector: map[string]string{
			"connection-pooler": c.connectionPoolerName(role),
		},
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        c.connectionPoolerName(role),
			Namespace:   c.Namespace,
			Labels:      c.connectionPoolerLabelsSelector(role).MatchLabels,
			Annotations: map[string]string{},
			// make StatefulSet object its owner to represent the dependency.
			// By itself StatefulSet is being deleted with "Orphaned"
			// propagation policy, which means that it's deletion will not
			// clean up this service, but there is a hope that this object will
			// be garbage collected if something went wrong and operator didn't
			// deleted it.
			OwnerReferences: c.ownerReferences(),
		},
		Spec: serviceSpec,
	}

	return service
}

//delete connection pooler
func (c *Cluster) deleteConnectionPooler(role PostgresRole) (err error) {
	//c.setProcessName("deleting connection pooler")
	c.logger.Debugln("deleting connection pooler")

	// Lack of connection pooler objects is not a fatal error, just log it if
	// it was present before in the manifest
	if c == nil || role == "" {
		c.logger.Infof("No connection pooler to delete")
		return nil
	}

	// Clean up the deployment object. If deployment resource we've remembered
	// is somehow empty, try to delete based on what would we generate
	var deployment *appsv1.Deployment
	deployment = c.ConnectionPooler[role].Deployment

	policy := metav1.DeletePropagationForeground
	options := metav1.DeleteOptions{PropagationPolicy: &policy}

	if deployment != nil {

		// set delete propagation policy to foreground, so that replica set will be
		// also deleted.

		err = c.KubeClient.
			Deployments(c.Namespace).
			Delete(context.TODO(), c.connectionPoolerName(role), options)

		if k8sutil.ResourceNotFound(err) {
			c.logger.Debugf("Connection pooler deployment was already deleted")
		} else if err != nil {
			return fmt.Errorf("could not delete deployment: %v", err)
		}

		c.logger.Infof("Connection pooler deployment %q has been deleted for role %s", c.connectionPoolerName(role), role)
	}

	// Repeat the same for the service object
	var service *v1.Service
	service = c.ConnectionPooler[role].Service
	if service == nil {
		c.logger.Infof("nil service to be deleted")
	}
	if service != nil {

		err = c.KubeClient.
			Services(c.Namespace).
			Delete(context.TODO(), c.connectionPoolerName(role), options)

		if k8sutil.ResourceNotFound(err) {
			c.logger.Debugf("Connection pooler service was already deleted")
		} else if err != nil {
			return fmt.Errorf("could not delete service: %v", err)
		}

		c.logger.Infof("Connection pooler service %q has been deleted for role %s", c.connectionPoolerName(role), role)
	}
	// Repeat the same for the secret object
	secretName := c.credentialSecretName(c.OpConfig.ConnectionPooler.User)

	secret, err := c.KubeClient.
		Secrets(c.Namespace).
		Get(context.TODO(), secretName, metav1.GetOptions{})

	if err != nil {
		c.logger.Debugf("could not get connection pooler secret %q: %v", secretName, err)
	} else {
		if err = c.deleteSecret(secret.UID, *secret); err != nil {
			return fmt.Errorf("could not delete pooler secret: %v", err)
		}
	}

	c.ConnectionPooler = nil
	return nil
}

// Perform actual patching of a connection pooler deployment, assuming that all
// the check were already done before.
func updateConnectionPoolerDeployment(KubeClient k8sutil.KubernetesClient, newDeployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	if newDeployment == nil {
		return nil, fmt.Errorf("there is no connection pooler in the cluster")
	}

	patchData, err := specPatch(newDeployment.Spec)
	if err != nil {
		return nil, fmt.Errorf("could not form patch for the deployment: %v", err)
	}

	// An update probably requires RetryOnConflict, but since only one operator
	// worker at one time will try to update it chances of conflicts are
	// minimal.
	deployment, err := KubeClient.
		Deployments(newDeployment.Namespace).Patch(
		context.TODO(),
		newDeployment.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
		"")
	if err != nil {
		return nil, fmt.Errorf("could not patch deployment: %v", err)
	}

	return deployment, nil
}

//updateConnectionPoolerAnnotations updates the annotations of connection pooler deployment
func (c *Cluster) updateConnectionPoolerAnnotations(annotations map[string]string, role PostgresRole) (*appsv1.Deployment, error) {
	c.logger.Debugf("updating connection pooler annotations")
	patchData, err := metaAnnotationsPatch(annotations)
	if err != nil {
		return nil, fmt.Errorf("could not form patch for the deployment metadata: %v", err)
	}
	result, err := c.KubeClient.Deployments(c.ConnectionPooler[role].Deployment.Namespace).Patch(
		context.TODO(),
		c.ConnectionPooler[role].Deployment.Name,
		types.MergePatchType,
		[]byte(patchData),
		metav1.PatchOptions{},
		"")
	if err != nil {
		return nil, fmt.Errorf("could not patch connection pooler annotations %q: %v", patchData, err)
	}
	return result, nil

}

// Test if two connection pooler configuration needs to be synced. For simplicity
// compare not the actual K8S objects, but the configuration itself and request
// sync if there is any difference.
func needSyncConnectionPoolerSpecs(oldSpec, newSpec *acidv1.ConnectionPooler) (sync bool, reasons []string) {
	reasons = []string{}
	sync = false

	changelog, err := diff.Diff(oldSpec, newSpec)
	if err != nil {
		//c.logger.Infof("Cannot get diff, do not do anything, %+v", err)
		return false, reasons
	}

	if len(changelog) > 0 {
		sync = true
	}

	for _, change := range changelog {
		msg := fmt.Sprintf("%s %+v from '%+v' to '%+v'",
			change.Type, change.Path, change.From, change.To)
		reasons = append(reasons, msg)
	}

	return sync, reasons
}

// Check if we need to synchronize connection pooler deployment due to new
// defaults, that are different from what we see in the DeploymentSpec
func (c *Cluster) needSyncConnectionPoolerDefaults(spec *acidv1.ConnectionPooler, deployment *appsv1.Deployment) (sync bool, reasons []string) {

	reasons = []string{}
	sync = false

	config := c.OpConfig.ConnectionPooler
	podTemplate := deployment.Spec.Template
	poolerContainer := podTemplate.Spec.Containers[constants.ConnectionPoolerContainer]

	if spec == nil {
		spec = &acidv1.ConnectionPooler{}
	}
	if spec.NumberOfInstances == nil &&
		*deployment.Spec.Replicas != *config.NumberOfInstances {

		sync = true
		msg := fmt.Sprintf("NumberOfInstances is different (having %d, required %d)",
			*deployment.Spec.Replicas, *config.NumberOfInstances)
		reasons = append(reasons, msg)
	}

	if spec.DockerImage == "" &&
		poolerContainer.Image != config.Image {

		sync = true
		msg := fmt.Sprintf("DockerImage is different (having %s, required %s)",
			poolerContainer.Image, config.Image)
		reasons = append(reasons, msg)
	}

	expectedResources, err := generateResourceRequirements(spec.Resources,
		c.makeDefaultConnectionPoolerResources())

	// An error to generate expected resources means something is not quite
	// right, but for the purpose of robustness do not panic here, just report
	// and ignore resources comparison (in the worst case there will be no
	// updates for new resource values).
	if err == nil && syncResources(&poolerContainer.Resources, expectedResources) {
		sync = true
		msg := fmt.Sprintf("Resources are different (having %+v, required %+v)",
			poolerContainer.Resources, expectedResources)
		reasons = append(reasons, msg)
	}

	if err != nil {
		c.logger.Warningf("Cannot generate expected resources, %v", err)
	}

	for _, env := range poolerContainer.Env {
		if spec.User == "" && env.Name == "PGUSER" {
			ref := env.ValueFrom.SecretKeyRef.LocalObjectReference

			if ref.Name != c.credentialSecretName(config.User) {
				sync = true
				msg := fmt.Sprintf("pooler user is different (having %s, required %s)",
					ref.Name, config.User)
				reasons = append(reasons, msg)
			}
		}

		if spec.Schema == "" && env.Name == "PGSCHEMA" && env.Value != config.Schema {
			sync = true
			msg := fmt.Sprintf("pooler schema is different (having %s, required %s)",
				env.Value, config.Schema)
			reasons = append(reasons, msg)
		}
	}

	return sync, reasons
}

// Generate default resource section for connection pooler deployment, to be
// used if nothing custom is specified in the manifest
func (c Cluster) makeDefaultConnectionPoolerResources() acidv1.Resources {
	config := c.OpConfig

	defaultRequests := acidv1.ResourceDescription{
		CPU:    config.ConnectionPooler.ConnectionPoolerDefaultCPURequest,
		Memory: config.ConnectionPooler.ConnectionPoolerDefaultMemoryRequest,
	}
	defaultLimits := acidv1.ResourceDescription{
		CPU:    config.ConnectionPooler.ConnectionPoolerDefaultCPULimit,
		Memory: config.ConnectionPooler.ConnectionPoolerDefaultMemoryLimit,
	}

	return acidv1.Resources{
		ResourceRequests: defaultRequests,
		ResourceLimits:   defaultLimits,
	}
}

func (c *Cluster) syncConnectionPooler(oldSpec, newSpec *acidv1.Postgresql, installLookupFunction InstallFunction) (SyncReason, error) {

	var reason SyncReason
	var err error
	var newNeedConnectionPooler, oldNeedConnectionPooler bool
	oldNeedConnectionPooler = false

	// Check and perform the sync requirements for each of the roles.
	for _, role := range [2]PostgresRole{Master, Replica} {

		if role == Master {
			newNeedConnectionPooler = needMasterConnectionPoolerWorker(&newSpec.Spec)
			if oldSpec != nil {
				oldNeedConnectionPooler = needMasterConnectionPoolerWorker(&oldSpec.Spec)
			}
		} else {
			newNeedConnectionPooler = needReplicaConnectionPoolerWorker(&newSpec.Spec)
			if oldSpec != nil {
				oldNeedConnectionPooler = needReplicaConnectionPoolerWorker(&oldSpec.Spec)
			}
		}
		if c.ConnectionPooler == nil {
			c.ConnectionPooler = map[PostgresRole]*ConnectionPoolerObjects{
				Master: {
					Deployment:     nil,
					Service:        nil,
					LookupFunction: false,
				},
				Replica: {
					Deployment:     nil,
					Service:        nil,
					LookupFunction: false,
				},
			}
		}
		if newNeedConnectionPooler {
			// Try to sync in any case. If we didn't needed connection pooler before,
			// it means we want to create it. If it was already present, still sync
			// since it could happen that there is no difference in specs, and all
			// the resources are remembered, but the deployment was manually deleted
			// in between
			c.logger.Debugf("syncing connection pooler for the Spilo role %s", role)

			// in this case also do not forget to install lookup function as for
			// creating cluster
			if !oldNeedConnectionPooler || !c.ConnectionPooler[role].LookupFunction {
				newConnectionPooler := newSpec.Spec.ConnectionPooler

				specSchema := ""
				specUser := ""

				if newConnectionPooler != nil {
					specSchema = newConnectionPooler.Schema
					specUser = newConnectionPooler.User
				}

				schema := util.Coalesce(
					specSchema,
					c.OpConfig.ConnectionPooler.Schema)

				user := util.Coalesce(
					specUser,
					c.OpConfig.ConnectionPooler.User)

				if err = installLookupFunction(schema, user, role); err != nil {
					return NoSync, err
				}
			}

			if reason, err = c.syncConnectionPoolerWorker(oldSpec, newSpec, role); err != nil {
				c.logger.Errorf("could not sync connection pooler: %v", err)
				return reason, err
			}
		} else {
			// delete and cleanup resources
			if c.ConnectionPooler[role] != nil &&
				(c.ConnectionPooler[role].Deployment != nil ||
					c.ConnectionPooler[role].Service != nil) {

				if err = c.deleteConnectionPooler(role); err != nil {
					c.logger.Warningf("could not remove connection pooler: %v", err)
				}
			}
		}
	}

	return reason, nil
}

// Synchronize connection pooler resources. Effectively we're interested only in
// synchronizing the corresponding deployment, but in case of deployment or
// service is missing, create it. After checking, also remember an object for
// the future references.
func (c *Cluster) syncConnectionPoolerWorker(oldSpec, newSpec *acidv1.Postgresql, role PostgresRole) (
	SyncReason, error) {

	deployment, err := c.KubeClient.
		Deployments(c.Namespace).
		Get(context.TODO(), c.connectionPoolerName(role), metav1.GetOptions{})

	if err != nil && k8sutil.ResourceNotFound(err) {
		msg := "Deployment %s for connection pooler synchronization is not found, create it"
		c.logger.Warningf(msg, c.connectionPoolerName(role))

		deploymentSpec, err := c.generateConnectionPoolerDeployment(role)
		if err != nil {
			msg = "could not generate deployment for connection pooler: %v"
			return NoSync, fmt.Errorf(msg, err)
		}

		deployment, err := c.KubeClient.
			Deployments(deploymentSpec.Namespace).
			Create(context.TODO(), deploymentSpec, metav1.CreateOptions{})

		if err != nil {
			return NoSync, err
		}
		c.ConnectionPooler[role].Deployment = deployment
	} else if err != nil {
		msg := "could not get connection pooler deployment to sync: %v"
		return NoSync, fmt.Errorf(msg, err)
	} else {
		c.ConnectionPooler[role].Deployment = deployment
		// actual synchronization

		var oldConnectionPooler *acidv1.ConnectionPooler

		if oldSpec != nil {
			oldConnectionPooler = oldSpec.Spec.ConnectionPooler
		}

		newConnectionPooler := newSpec.Spec.ConnectionPooler
		// sync implementation below assumes that both old and new specs are
		// not nil, but it can happen. To avoid any confusion like updating a
		// deployment because the specification changed from nil to an empty
		// struct (that was initialized somewhere before) replace any nil with
		// an empty spec.
		if oldConnectionPooler == nil {
			oldConnectionPooler = &acidv1.ConnectionPooler{}
		}

		if newConnectionPooler == nil {
			newConnectionPooler = &acidv1.ConnectionPooler{}
		}

		c.logger.Infof("Old: %+v, New %+v", oldConnectionPooler, newConnectionPooler)

		var specSync bool
		var specReason []string

		if oldSpec != nil {
			specSync, specReason = needSyncConnectionPoolerSpecs(oldConnectionPooler, newConnectionPooler)
		}

		defaultsSync, defaultsReason := c.needSyncConnectionPoolerDefaults(newConnectionPooler, deployment)
		reason := append(specReason, defaultsReason...)

		if specSync || defaultsSync {
			c.logger.Infof("Update connection pooler deployment %s, reason: %+v",
				c.connectionPoolerName(role), reason)
			newDeploymentSpec, err := c.generateConnectionPoolerDeployment(role)
			if err != nil {
				msg := "could not generate deployment for connection pooler: %v"
				return reason, fmt.Errorf(msg, err)
			}

			//oldDeploymentSpec := c.ConnectionPooler[role].Deployment

			deployment, err := updateConnectionPoolerDeployment(c.KubeClient,
				newDeploymentSpec)

			if err != nil {
				return reason, err
			}
			c.ConnectionPooler[role].Deployment = deployment
		}
	}

	newAnnotations := c.AnnotationsToPropagate(c.ConnectionPooler[role].Deployment.Annotations)
	if newAnnotations != nil {
		c.updateConnectionPoolerAnnotations(newAnnotations, role)
	}

	service, err := c.KubeClient.
		Services(c.Namespace).
		Get(context.TODO(), c.connectionPoolerName(role), metav1.GetOptions{})

	if err != nil && k8sutil.ResourceNotFound(err) {
		msg := "Service %s for connection pooler synchronization is not found, create it"
		c.logger.Warningf(msg, c.connectionPoolerName(role))

		serviceSpec := c.generateConnectionPoolerService(&newSpec.Spec, role)
		service, err := c.KubeClient.
			Services(serviceSpec.Namespace).
			Create(context.TODO(), serviceSpec, metav1.CreateOptions{})

		if err != nil {
			return NoSync, err
		}
		c.ConnectionPooler[role].Service = service

	} else if err != nil {
		msg := "could not get connection pooler service to sync: %v"
		return NoSync, fmt.Errorf(msg, err)
	} else {
		// Service updates are not supported and probably not that useful anyway
		c.ConnectionPooler[role].Service = service
	}

	return NoSync, nil
}
