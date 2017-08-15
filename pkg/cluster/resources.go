package cluster

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

func (c *Cluster) loadResources() error {
	ns := c.Namespace
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	services, err := c.KubeClient.Services(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get list of services: %v", err)
	}
	if len(services.Items) > 2 {
		return fmt.Errorf("too many(%d) services for a cluster", len(services.Items))
	}
	for i, svc := range services.Items {
		switch postgresRole(svc.Labels[c.OpConfig.PodRoleLabel]) {
		case replica:
			c.Services[replica] = &services.Items[i]
		default:
			c.Services[master] = &services.Items[i]
		}
	}

	endpoints, err := c.KubeClient.Endpoints(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get list of endpoints: %v", err)
	}
	if len(endpoints.Items) > 2 {
		return fmt.Errorf("too many(%d) endpoints for a cluster", len(endpoints.Items))
	}

	for i, ep := range endpoints.Items {
		if ep.Labels[c.OpConfig.PodRoleLabel] != string(replica) {
			c.Endpoint = &endpoints.Items[i]
			break
		}
	}

	secrets, err := c.KubeClient.Secrets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get list of secrets: %v", err)
	}
	for i, secret := range secrets.Items {
		if _, ok := c.Secrets[secret.UID]; ok {
			continue
		}
		c.Secrets[secret.UID] = &secrets.Items[i]
		c.logger.Debugf("secret loaded, uid: %q", secret.UID)
	}

	statefulSets, err := c.KubeClient.StatefulSets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("could not get list of statefulsets: %v", err)
	}
	if len(statefulSets.Items) > 1 {
		return fmt.Errorf("too many(%d) statefulsets for a cluster", len(statefulSets.Items))
	}
	if len(statefulSets.Items) == 1 {
		c.Statefulset = &statefulSets.Items[0]
	}

	return nil
}

func (c *Cluster) listResources() error {
	if c.Statefulset != nil {
		c.logger.Infof("found statefulset: %q (uid: %q)", util.NameFromMeta(c.Statefulset.ObjectMeta), c.Statefulset.UID)
	}

	for _, obj := range c.Secrets {
		c.logger.Infof("found secret: %q (uid: %q)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	if c.Endpoint != nil {
		c.logger.Infof("found endpoint: %q (uid: %q)", util.NameFromMeta(c.Endpoint.ObjectMeta), c.Endpoint.UID)
	}

	for role, service := range c.Services {
		c.logger.Infof("found %s service: %q (uid: %q)", role, util.NameFromMeta(service.ObjectMeta), service.UID)
	}

	pods, err := c.listPods()
	if err != nil {
		return fmt.Errorf("could not get the list of pods: %v", err)
	}

	for _, obj := range pods {
		c.logger.Infof("found pod: %q (uid: %q)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return fmt.Errorf("could not get the list of PVCs: %v", err)
	}

	for _, obj := range pvcs {
		c.logger.Infof("found PVC: %q (uid: %q)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	return nil
}

func (c *Cluster) createStatefulSet() (*v1beta1.StatefulSet, error) {
	if c.Statefulset != nil {
		return nil, fmt.Errorf("statefulset already exists in the cluster")
	}
	statefulSetSpec, err := c.generateStatefulSet(c.Spec)
	if err != nil {
		return nil, fmt.Errorf("could not generate statefulset: %v", err)
	}
	statefulSet, err := c.KubeClient.StatefulSets(statefulSetSpec.Namespace).Create(statefulSetSpec)
	if err != nil {
		return nil, err
	}
	c.Statefulset = statefulSet
	c.logger.Debugf("created new statefulset %q, uid: %q", util.NameFromMeta(statefulSet.ObjectMeta), statefulSet.UID)

	return statefulSet, nil
}

func (c *Cluster) updateStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	if c.Statefulset == nil {
		return fmt.Errorf("there is no statefulset in the cluster")
	}
	statefulSetName := util.NameFromMeta(c.Statefulset.ObjectMeta)

	c.logger.Debugf("updating statefulset")

	patchData, err := specPatch(newStatefulSet.Spec)
	if err != nil {
		return fmt.Errorf("could not form patch for the statefulset %q: %v", statefulSetName, err)
	}

	statefulSet, err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Patch(
		c.Statefulset.Name,
		types.MergePatchType,
		patchData, "")
	if err != nil {
		return fmt.Errorf("could not patch statefulset %q: %v", statefulSetName, err)
	}
	c.Statefulset = statefulSet

	return nil
}

// replaceStatefulSet deletes an old StatefulSet and creates the new using spec in the PostgreSQL TPR.
func (c *Cluster) replaceStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	if c.Statefulset == nil {
		return fmt.Errorf("there is no statefulset in the cluster")
	}

	statefulSetName := util.NameFromMeta(c.Statefulset.ObjectMeta)
	c.logger.Debugf("replacing statefulset")

	// Delete the current statefulset without deleting the pods
	orphanDepencies := true
	oldStatefulset := c.Statefulset

	options := metav1.DeleteOptions{OrphanDependents: &orphanDepencies}
	if err := c.KubeClient.StatefulSets(oldStatefulset.Namespace).Delete(oldStatefulset.Name, &options); err != nil {
		return fmt.Errorf("could not delete statefulset %q: %v", statefulSetName, err)
	}
	// make sure we clear the stored statefulset status if the subsequent create fails.
	c.Statefulset = nil
	// wait until the statefulset is truly deleted
	c.logger.Debugf("waiting for the statefulset to be deleted")

	err := retryutil.Retry(constants.StatefulsetDeletionInterval, constants.StatefulsetDeletionTimeout,
		func() (bool, error) {
			_, err := c.KubeClient.StatefulSets(oldStatefulset.Namespace).Get(oldStatefulset.Name, metav1.GetOptions{})

			return err != nil, nil
		})
	if err != nil {
		return fmt.Errorf("could not delete statefulset: %v", err)
	}

	// create the new statefulset with the desired spec. It would take over the remaining pods.
	createdStatefulset, err := c.KubeClient.StatefulSets(newStatefulSet.Namespace).Create(newStatefulSet)
	if err != nil {
		return fmt.Errorf("could not create statefulset %q: %v", statefulSetName, err)
	}
	// check that all the previous replicas were picked up.
	if newStatefulSet.Spec.Replicas == oldStatefulset.Spec.Replicas &&
		createdStatefulset.Status.Replicas != oldStatefulset.Status.Replicas {
		c.logger.Warnf("number of pods for the old and updated Statefulsets is not identical")
	}

	c.Statefulset = createdStatefulset
	return nil
}

func (c *Cluster) deleteStatefulSet() error {
	c.logger.Debugln("deleting statefulset")
	if c.Statefulset == nil {
		return fmt.Errorf("there is no statefulset in the cluster")
	}

	err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Delete(c.Statefulset.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("statefulset %q has been deleted", util.NameFromMeta(c.Statefulset.ObjectMeta))
	c.Statefulset = nil

	if err := c.deletePods(); err != nil {
		return fmt.Errorf("could not delete pods: %v", err)
	}

	if err := c.deletePersistenVolumeClaims(); err != nil {
		return fmt.Errorf("could not delete PersistentVolumeClaims: %v", err)
	}

	return nil
}

func (c *Cluster) createService(role postgresRole) (*v1.Service, error) {
	if c.Services[role] != nil {
		return nil, fmt.Errorf("service already exists in the cluster")
	}
	serviceSpec := c.generateService(role, &c.Spec)

	service, err := c.KubeClient.Services(serviceSpec.Namespace).Create(serviceSpec)
	if err != nil {
		return nil, err
	}

	c.Services[role] = service
	return service, nil
}

func (c *Cluster) updateService(role postgresRole, newService *v1.Service) error {
	if c.Services[role] == nil {
		return fmt.Errorf("there is no service in the cluster")
	}
	serviceName := util.NameFromMeta(c.Services[role].ObjectMeta)
	endpointName := util.NameFromMeta(c.Endpoint.ObjectMeta)
	// TODO: check if it possible to change the service type with a patch in future versions of Kubernetes
	if newService.Spec.Type != c.Services[role].Spec.Type {
		// service type has changed, need to replace the service completely.
		// we cannot use just pach the current service, since it may contain attributes incompatible with the new type.
		var (
			currentEndpoint *v1.Endpoints
			err             error
		)

		if role == master {
			// for the master service we need to re-create the endpoint as well. Get the up-to-date version of
			// the addresses stored in it before the service is deleted (deletion of the service removes the endpooint)
			currentEndpoint, err = c.KubeClient.Endpoints(c.Services[role].Namespace).Get(c.Services[role].Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("could not get current cluster endpoints: %v", err)
			}
		}
		err = c.KubeClient.Services(c.Services[role].Namespace).Delete(c.Services[role].Name, c.deleteOptions)
		if err != nil {
			return fmt.Errorf("could not delete service %q: %v", serviceName, err)
		}
		c.Endpoint = nil
		svc, err := c.KubeClient.Services(newService.Namespace).Create(newService)
		if err != nil {
			return fmt.Errorf("could not create service %q: %v", serviceName, err)
		}
		c.Services[role] = svc
		if role == master {
			// create the new endpoint using the addresses obtained from the previous one
			endpointSpec := c.generateMasterEndpoints(currentEndpoint.Subsets)
			ep, err := c.KubeClient.Endpoints(c.Services[role].Namespace).Create(endpointSpec)
			if err != nil {
				return fmt.Errorf("could not create endpoint %q: %v", endpointName, err)
			}
			c.Endpoint = ep
		}
		return nil
	}

	if len(newService.ObjectMeta.Annotations) > 0 {
		annotationsPatchData := metadataAnnotationsPatch(newService.ObjectMeta.Annotations)

		_, err := c.KubeClient.Services(c.Services[role].Namespace).Patch(
			c.Services[role].Name,
			types.StrategicMergePatchType,
			[]byte(annotationsPatchData), "")

		if err != nil {
			return fmt.Errorf("could not replace annotations for the service %q: %v", serviceName, err)
		}
	}

	patchData, err := specPatch(newService.Spec)
	if err != nil {
		return fmt.Errorf("could not form patch for the service %q: %v", serviceName, err)
	}

	svc, err := c.KubeClient.Services(c.Services[role].Namespace).Patch(
		c.Services[role].Name,
		types.MergePatchType,
		patchData, "")
	if err != nil {
		return fmt.Errorf("could not patch service %q: %v", serviceName, err)
	}
	c.Services[role] = svc

	return nil
}

func (c *Cluster) deleteService(role postgresRole) error {
	c.logger.Debugf("deleting service %s", role)
	if c.Services[role] == nil {
		return fmt.Errorf("there is no %s service in the cluster", role)
	}
	service := c.Services[role]
	err := c.KubeClient.Services(service.Namespace).Delete(service.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("%s service %q has been deleted", role, util.NameFromMeta(service.ObjectMeta))
	c.Services[role] = nil
	return nil
}

func (c *Cluster) createEndpoint() (*v1.Endpoints, error) {
	if c.Endpoint != nil {
		return nil, fmt.Errorf("endpoint already exists in the cluster")
	}
	endpointsSpec := c.generateMasterEndpoints(nil)

	endpoints, err := c.KubeClient.Endpoints(endpointsSpec.Namespace).Create(endpointsSpec)
	if err != nil {
		return nil, err
	}
	c.Endpoint = endpoints

	return endpoints, nil
}

func (c *Cluster) deleteEndpoint() error {
	c.logger.Debugln("deleting endpoint")
	if c.Endpoint == nil {
		return fmt.Errorf("there is no endpoint in the cluster")
	}
	err := c.KubeClient.Endpoints(c.Endpoint.Namespace).Delete(c.Endpoint.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("endpoint %q has been deleted", util.NameFromMeta(c.Endpoint.ObjectMeta))
	c.Endpoint = nil

	return nil
}

func (c *Cluster) applySecrets() error {
	secrets := c.generateUserSecrets()

	for secretUsername, secretSpec := range secrets {
		secret, err := c.KubeClient.Secrets(secretSpec.Namespace).Create(secretSpec)
		if k8sutil.ResourceAlreadyExists(err) {
			var userMap map[string]spec.PgUser
			curSecret, err := c.KubeClient.Secrets(secretSpec.Namespace).Get(secretSpec.Name, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("could not get current secret: %v", err)
			}
			c.logger.Debugf("secret %q already exists, fetching it's password", util.NameFromMeta(curSecret.ObjectMeta))
			if secretUsername == c.systemUsers[constants.SuperuserKeyName].Name {
				secretUsername = constants.SuperuserKeyName
				userMap = c.systemUsers
			} else if secretUsername == c.systemUsers[constants.ReplicationUserKeyName].Name {
				secretUsername = constants.ReplicationUserKeyName
				userMap = c.systemUsers
			} else {
				userMap = c.pgUsers
			}
			pwdUser := userMap[secretUsername]
			pwdUser.Password = string(curSecret.Data["password"])
			userMap[secretUsername] = pwdUser

			continue
		} else {
			if err != nil {
				return fmt.Errorf("could not create secret for user %q: %v", secretUsername, err)
			}
			c.Secrets[secret.UID] = secret
			c.logger.Debugf("created new secret %q, uid: %q", util.NameFromMeta(secret.ObjectMeta), secret.UID)
		}
	}

	return nil
}

func (c *Cluster) deleteSecret(secret *v1.Secret) error {
	c.logger.Debugf("deleting secret %q", util.NameFromMeta(secret.ObjectMeta))
	err := c.KubeClient.Secrets(secret.Namespace).Delete(secret.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("secret %q has been deleted", util.NameFromMeta(secret.ObjectMeta))
	delete(c.Secrets, secret.UID)

	return err
}

func (c *Cluster) createRoles() (err error) {
	// TODO: figure out what to do with duplicate names (humans and robots) among pgUsers
	return c.syncRoles(false)
}

// GetServiceMaster returns cluster's kubernetes master Service
func (c *Cluster) GetServiceMaster() *v1.Service {
	return c.Services[master]
}

// GetServiceReplica returns cluster's kubernetes replica Service
func (c *Cluster) GetServiceReplica() *v1.Service {
	return c.Services[replica]
}

// GetEndpoint returns cluster's kubernetes Endpoint
func (c *Cluster) GetEndpoint() *v1.Endpoints {
	return c.Endpoint
}

// GetStatefulSet returns cluster's kubernetes StatefulSet
func (c *Cluster) GetStatefulSet() *v1beta1.StatefulSet {
	return c.Statefulset
}
