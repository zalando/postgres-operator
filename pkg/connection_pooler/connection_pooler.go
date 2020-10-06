package connection_pooler

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/r3labs/diff"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/cluster"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
)

// K8S objects that are belongs to a connection pooler
type ConnectionPoolerObjects struct {
	Deployment *appsv1.Deployment
	Service    *v1.Service
	Name       string
	// It could happen that a connection pooler was enabled, but the operator
	// was not able to properly process a corresponding event or was restarted.
	// In this case we will miss missing/require situation and a lookup function
	// will not be installed. To avoid synchronizing it all the time to prevent
	// this, we can remember the result in memory at least until the next
	// restart.
	LookupFunction bool
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
func (cp *ConnectionPoolerObjects) createConnectionPooler(lookup cluster.InstallFunction, role cluster.PostgresRole, c cluster.Cluster) (*ConnectionPoolerObjects, error) {
	var msg string
	c.setProcessName("creating connection pooler")

	schema := c.Spec.ConnectionPooler.Schema

	if schema == "" {
		schema = c.OpConfig.ConnectionPooler.Schema
	}

	user := c.Spec.ConnectionPooler.User
	if user == "" {
		user = c.OpConfig.ConnectionPooler.User
	}

	err := c.lookup(schema, user, role)

	if err != nil {
		msg = "could not prepare database for connection pooler: %v"
		return nil, fmt.Errorf(msg, err)
	}
	if c.ConnectionPooler[role] == nil {
		c.ConnectionPooler = make(map[c.PostgresRole]*ConnectionPoolerObjects)
		c.ConnectionPooler[role].Deployment = nil
		c.ConnectionPooler[role].Service = nil
		c.ConnectionPooler[role].LookupFunction = false
	}
	deploymentSpec, err := c.generateConnectionPoolerDeployment(&c.Spec, role)
	if err != nil {
		msg = "could not generate deployment for connection pooler: %v"
		return nil, fmt.Errorf(msg, err)
	}

	// client-go does retry 10 times (with NoBackoff by default) when the API
	// believe a request can be retried and returns Retry-After header. This
	// should be good enough to not think about it here.
	deployment, err := c.KubeClient.
		Deployments(deploymentSpec.Namespace).
		Create(context.TODO(), deploymentSpec, metav1.CreateOptions{})

	if err != nil {
		return nil, err
	}

	serviceSpec := c.generateConnectionPoolerService(&c.Spec, role)
	service, err := c.KubeClient.
		Services(serviceSpec.Namespace).
		Create(context.TODO(), serviceSpec, metav1.CreateOptions{})

	if err != nil {
		return nil, err
	}
	c.ConnectionPooler[role].Deployment = deployment
	c.ConnectionPooler[role].Service = service

	c.logger.Debugf("created new connection pooler %q, uid: %q",
		util.NameFromMeta(deployment.ObjectMeta), deployment.UID)

	return c.ConnectionPooler[role], nil
}

func (cp *ConnectionPoolerObjects) generateConnectionPoolerDeployment(spec *acidv1.PostgresSpec, role cluster.PostgresRole, c cluster.Cluster) (
	*appsv1.Deployment, error) {

	// there are two ways to enable connection pooler, either to specify a
	// connectionPooler section or enableConnectionPooler. In the second case
	// spec.connectionPooler will be nil, so to make it easier to calculate
	// default values, initialize it to an empty structure. It could be done
	// anywhere, but here is the earliest common entry point between sync and
	// create code, so init here.
	if spec.ConnectionPooler == nil {
		spec.ConnectionPooler = &acidv1.ConnectionPooler{}
	}

	podTemplate, err := c.generateConnectionPoolerPodTemplate(spec, role)
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

func (cp *ConnectionPoolerObjects) generateConnectionPoolerService(spec *acidv1.PostgresSpec, role cluster.PostgresRole, c cluster.Cluster) *v1.Service {

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

// delete connection pooler
func (cp *ConnectionPoolerObjects) deleteConnectionPooler(role cluster.PostgresRole, c cluster.Cluster) (err error) {
	c.setProcessName("deleting connection pooler")
	c.logger.Debugln("deleting connection pooler")

	// Lack of connection pooler objects is not a fatal error, just log it if
	// it was present before in the manifest
	if c.ConnectionPooler == nil {
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

		c.logger.Infof("Connection pooler deployment %q has been deleted", c.connectionPoolerName(role))
	}

	// Repeat the same for the service object
	var service *v1.Service
	service = c.ConnectionPooler[role].Service

	if service != nil {

		err = c.KubeClient.
			Services(c.Namespace).
			Delete(context.TODO(), c.connectionPoolerName(role), options)

		if k8sutil.ResourceNotFound(err) {
			c.logger.Debugf("Connection pooler service was already deleted")
		} else if err != nil {
			return fmt.Errorf("could not delete service: %v", err)
		}

		c.logger.Infof("Connection pooler service %q has been deleted", c.connectionPoolerName(role))
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
func (cp *ConnectionPoolerObjects) updateConnectionPoolerDeployment(oldDeploymentSpec, newDeployment *appsv1.Deployment, role cluster.PostgresRole, c cluster.Cluster) (*appsv1.Deployment, error) {
	c.setProcessName("updating connection pooler")
	if c.ConnectionPooler == nil || c.ConnectionPooler[role].Deployment == nil {
		return nil, fmt.Errorf("there is no connection pooler in the cluster")
	}

	patchData, err := specPatch(newDeployment.Spec)
	if err != nil {
		return nil, fmt.Errorf("could not form patch for the deployment: %v", err)
	}

	// An update probably requires RetryOnConflict, but since only one operator
	// worker at one time will try to update it chances of conflicts are
	// minimal.
	deployment, err := c.KubeClient.
		Deployments(c.ConnectionPooler[role].Deployment.Namespace).Patch(
		context.TODO(),
		c.ConnectionPooler[role].Deployment.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
		"")
	if err != nil {
		return nil, fmt.Errorf("could not patch deployment: %v", err)
	}

	c.ConnectionPooler[role].Deployment = deployment

	return deployment, nil
}

//updateConnectionPoolerAnnotations updates the annotations of connection pooler deployment
func (cp *ConnectionPoolerObjects) updateConnectionPoolerAnnotations(annotations map[string]string, role cluster.PostgresRole, c cluster.Cluster) (*appsv1.Deployment, error) {
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

//sync connection pooler

// Test if two connection pooler configuration needs to be synced. For simplicity
// compare not the actual K8S objects, but the configuration itself and request
// sync if there is any difference.
func (cp *ConnectionPoolerObjects) needSyncConnectionPoolerSpecs(oldSpec, newSpec *acidv1.ConnectionPooler, c cluster.Cluster) (sync bool, reasons []string) {
	reasons = []string{}
	sync = false

	changelog, err := diff.Diff(oldSpec, newSpec)
	if err != nil {
		c.logger.Infof("Cannot get diff, do not do anything, %+v", err)
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
func (cp *ConnectionPoolerObjects) needSyncConnectionPoolerDefaults(spec *acidv1.ConnectionPooler, deployment *appsv1.Deployment, c cluster.Cluster) (sync bool, reasons []string) {

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

func (cp *ConnectionPoolerObjects) syncConnectionPooler(oldSpec, newSpec *acidv1.Postgresql, lookup cluster.InstallFunction, c cluster.Cluster) (SyncReason, error) {

	var reason SyncReason
	var err error
	var newNeedConnectionPooler, oldNeedConnectionPooler bool

	// Check and perform the sync requirements for each of the roles.
	for _, role := range [2]PostgresRole{Master, Replica} {
		if role == Master {
			newNeedConnectionPooler = c.needMasterConnectionPoolerWorker(&newSpec.Spec)
			oldNeedConnectionPooler = c.needMasterConnectionPoolerWorker(&oldSpec.Spec)
		} else {
			newNeedConnectionPooler = c.needReplicaConnectionPoolerWorker(&newSpec.Spec)
			oldNeedConnectionPooler = c.needReplicaConnectionPoolerWorker(&oldSpec.Spec)
		}
		if c.ConnectionPooler == nil {
			c.ConnectionPooler = make(map[PostgresRole]*ConnectionPoolerObjects)
			c.ConnectionPooler[role].Deployment = nil
			c.ConnectionPooler[role].Service = nil
			c.ConnectionPooler[role].LookupFunction = false
		}

		if newNeedConnectionPooler {
			// Try to sync in any case. If we didn't needed connection pooler before,
			// it means we want to create it. If it was already present, still sync
			// since it could happen that there is no difference in specs, and all
			// the resources are remembered, but the deployment was manually deleted
			// in between
			c.logger.Debug("syncing connection pooler for the role %v", role)

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

				if err = lookup(schema, user, role); err != nil {
					return NoSync, err
				}
			}

			if reason, err = c.syncConnectionPoolerWorker(oldSpec, newSpec, role); err != nil {
				c.logger.Errorf("could not sync connection pooler: %v", err)
				return reason, err
			}
		}

		if oldNeedConnectionPooler && !newNeedConnectionPooler {
			// delete and cleanup resources
			otherRole := role
			if len(c.RolesConnectionPooler()) == 2 {
				if role == Master {
					otherRole = Replica
				} else {
					otherRole = Master
				}
			}
			if c.ConnectionPooler != nil &&
				(c.ConnectionPooler[role].Deployment != nil ||
					c.ConnectionPooler[role].Service != nil) {

				if err = c.deleteConnectionPooler(role); err != nil {
					c.logger.Warningf("could not remove connection pooler: %v", err)
				}
			}
			if c.ConnectionPooler != nil && c.ConnectionPooler[otherRole].Deployment == nil && c.ConnectionPooler[otherRole].Service == nil {
				c.ConnectionPooler = nil
			}
		}

		if !oldNeedConnectionPooler && !newNeedConnectionPooler {
			// delete and cleanup resources if not empty
			otherRole := role
			if len(c.RolesConnectionPooler()) == 2 {
				if role == Master {
					otherRole = Replica
				} else {
					otherRole = Master
				}
			}
			if c.ConnectionPooler != nil &&
				(c.ConnectionPooler[role].Deployment != nil ||
					c.ConnectionPooler[role].Service != nil) {

				if err = c.deleteConnectionPooler(role); err != nil {
					c.logger.Warningf("could not remove connection pooler: %v", err)
				}
			} else if c.ConnectionPooler[otherRole].Deployment == nil && c.ConnectionPooler[otherRole].Service == nil {
				c.ConnectionPooler = nil
			}
		}
	}

	return reason, nil
}

// Synchronize connection pooler resources. Effectively we're interested only in
// synchronizing the corresponding deployment, but in case of deployment or
// service is missing, create it. After checking, also remember an object for
// the future references.
func (cp *ConnectionPoolerObjects) syncConnectionPoolerWorker(oldSpec, newSpec *acidv1.Postgresql, role cluster.PostgresRole, c cluster.Cluster) (
	SyncReason, error) {

	deployment, err := c.KubeClient.
		Deployments(c.Namespace).
		Get(context.TODO(), c.connectionPoolerName(role), metav1.GetOptions{})

	if err != nil && k8sutil.ResourceNotFound(err) {
		msg := "Deployment %s for connection pooler synchronization is not found, create it"
		c.logger.Warningf(msg, c.connectionPoolerName(role))

		deploymentSpec, err := c.generateConnectionPoolerDeployment(&newSpec.Spec, role)
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
		oldConnectionPooler := oldSpec.Spec.ConnectionPooler
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

		specSync, specReason := c.needSyncConnectionPoolerSpecs(oldConnectionPooler, newConnectionPooler)
		defaultsSync, defaultsReason := c.needSyncConnectionPoolerDefaults(newConnectionPooler, deployment)
		reason := append(specReason, defaultsReason...)

		if specSync || defaultsSync {
			c.logger.Infof("Update connection pooler deployment %s, reason: %+v",
				c.connectionPoolerName(role), reason)
			newDeploymentSpec, err := c.generateConnectionPoolerDeployment(&newSpec.Spec, role)
			if err != nil {
				msg := "could not generate deployment for connection pooler: %v"
				return reason, fmt.Errorf(msg, err)
			}

			oldDeploymentSpec := c.ConnectionPooler[role].Deployment

			deployment, err := c.updateConnectionPoolerDeployment(
				oldDeploymentSpec,
				newDeploymentSpec,
				role)

			if err != nil {
				return reason, err
			}
			c.ConnectionPooler[role].Deployment = deployment

			return reason, nil
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
