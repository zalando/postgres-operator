package cluster

import (
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	policybeta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

func (c *Cluster) loadResources() error {
	var err error
	ns := c.Namespace

	masterService, err := c.KubeClient.Services(ns).Get(c.serviceName(Master), metav1.GetOptions{})
	if err == nil {
		c.Services[Master] = masterService
	} else if !k8sutil.ResourceNotFound(err) {
		c.logger.Errorf("could not get master service: %v", err)
	}

	replicaService, err := c.KubeClient.Services(ns).Get(c.serviceName(Replica), metav1.GetOptions{})
	if err == nil {
		c.Services[Replica] = replicaService
	} else if !k8sutil.ResourceNotFound(err) {
		c.logger.Errorf("could not get replica service: %v", err)
	}

	ep, err := c.KubeClient.Endpoints(ns).Get(c.endpointName(), metav1.GetOptions{})
	if err == nil {
		c.Endpoint = ep
	} else if !k8sutil.ResourceNotFound(err) {
		c.logger.Errorf("could not get endpoint: %v", err)
	}

	secrets, err := c.KubeClient.Secrets(ns).List(metav1.ListOptions{LabelSelector: c.labelsSet().String()})
	if err != nil {
		c.logger.Errorf("could not get list of secrets: %v", err)
	}
	for i, secret := range secrets.Items {
		if _, ok := c.Secrets[secret.UID]; ok {
			continue
		}
		c.Secrets[secret.UID] = &secrets.Items[i]
		c.logger.Debugf("secret loaded, uid: %q", secret.UID)
	}

	ss, err := c.KubeClient.StatefulSets(ns).Get(c.statefulSetName(), metav1.GetOptions{})
	if err == nil {
		c.Statefulset = ss
	} else if !k8sutil.ResourceNotFound(err) {
		c.logger.Errorf("could not get statefulset: %v", err)
	}

	pdb, err := c.KubeClient.PodDisruptionBudgets(ns).Get(c.podDisruptionBudgetName(), metav1.GetOptions{})
	if err == nil {
		c.PodDisruptionBudget = pdb
	} else if !k8sutil.ResourceNotFound(err) {
		c.logger.Errorf("could not get pod disruption budget: %v", err)
	}

	return nil
}

func (c *Cluster) listResources() error {
	if c.PodDisruptionBudget != nil {
		c.logger.Infof("found pod disruption budget: %q (uid: %q)", util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta), c.PodDisruptionBudget.UID)
	}

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
	c.setProcessName("creating statefulset")
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

func getPodIndex(podName string) (int32, error) {
	parts := strings.Split(podName, "-")
	if len(parts) == 0 {
		return 0, fmt.Errorf("pod has no index part")
	}

	postfix := parts[len(parts)-1]
	res, err := strconv.ParseInt(postfix, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("could not parse pod index: %v", err)
	}

	return int32(res), nil
}

func (c *Cluster) preScaleDown(newStatefulSet *v1beta1.StatefulSet) error {
	masterPod, err := c.getRolePods(Master)
	if err != nil {
		return fmt.Errorf("could not get master pod: %v", err)
	}

	podNum, err := getPodIndex(masterPod[0].Name)
	if err != nil {
		return fmt.Errorf("could not get pod number: %v", err)
	}

	//Check if scale down affects current master pod
	if *newStatefulSet.Spec.Replicas >= podNum+1 {
		return nil
	}

	podName := fmt.Sprintf("%s-0", c.Statefulset.Name)
	masterCandidatePod, err := c.KubeClient.Pods(c.OpConfig.Namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get master candidate pod: %v", err)
	}

	// some sanity check
	if !util.MapContains(masterCandidatePod.Labels, c.OpConfig.ClusterLabels) ||
		!util.MapContains(masterCandidatePod.Labels, map[string]string{c.OpConfig.ClusterNameLabel: c.Name}) {
		return fmt.Errorf("pod %q does not belong to cluster", podName)
	}

	if err := c.patroni.Failover(&masterPod[0], masterCandidatePod.Name); err != nil {
		return fmt.Errorf("could not failover: %v", err)
	}

	return nil
}

func (c *Cluster) updateStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	c.setProcessName("updating statefulset")
	if c.Statefulset == nil {
		return fmt.Errorf("there is no statefulset in the cluster")
	}
	statefulSetName := util.NameFromMeta(c.Statefulset.ObjectMeta)

	//scale down
	if *c.Statefulset.Spec.Replicas > *newStatefulSet.Spec.Replicas {
		if err := c.preScaleDown(newStatefulSet); err != nil {
			c.logger.Warningf("could not scale down: %v", err)
		}
	}
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

// replaceStatefulSet deletes an old StatefulSet and creates the new using spec in the PostgreSQL CRD.
func (c *Cluster) replaceStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	c.setProcessName("replacing statefulset")
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
		c.logger.Warningf("number of pods for the old and updated Statefulsets is not identical")
	}

	c.Statefulset = createdStatefulset
	return nil
}

func (c *Cluster) deleteStatefulSet() error {
	c.setProcessName("deleting statefulset")
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

func (c *Cluster) createService(role PostgresRole) (*v1.Service, error) {
	c.setProcessName("creating %v service", role)

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

func (c *Cluster) updateService(role PostgresRole, newService *v1.Service) error {
	c.setProcessName("updating %v service", role)
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

		if role == Master {
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
		if role == Master {
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

func (c *Cluster) deleteService(role PostgresRole) error {
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
	c.setProcessName("creating endpoint")
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

func (c *Cluster) createPodDisruptionBudget() (*policybeta1.PodDisruptionBudget, error) {
	if c.PodDisruptionBudget != nil {
		return nil, fmt.Errorf("pod disruption budget already exists in the cluster")
	}
	podDisruptionBudgetSpec := c.generatePodDisruptionBudget()
	podDisruptionBudget, err := c.KubeClient.
		PodDisruptionBudgets(podDisruptionBudgetSpec.Namespace).
		Create(podDisruptionBudgetSpec)

	if err != nil {
		return nil, err
	}
	c.PodDisruptionBudget = podDisruptionBudget

	return podDisruptionBudget, nil
}

func (c *Cluster) updatePodDisruptionBudget(pdb *policybeta1.PodDisruptionBudget) error {
	if c.podEventsQueue == nil {
		return fmt.Errorf("there is no pod disruption budget in the cluster")
	}

	newPdb, err := c.KubeClient.PodDisruptionBudgets(pdb.Namespace).Update(pdb)
	if err != nil {
		return fmt.Errorf("could not update pod disruption budget: %v", err)
	}
	c.PodDisruptionBudget = newPdb

	return nil
}

func (c *Cluster) deletePodDisruptionBudget() error {
	c.logger.Debug("deleting pod disruption budget")
	if c.PodDisruptionBudget == nil {
		return fmt.Errorf("there is no pod disruption budget in the cluster")
	}
	err := c.KubeClient.
		PodDisruptionBudgets(c.PodDisruptionBudget.Namespace).
		Delete(c.PodDisruptionBudget.Namespace, c.deleteOptions)
	if err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}
	c.logger.Infof("pod disruption budget %q has been deleted", util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta))
	c.PodDisruptionBudget = nil

	return nil
}

func (c *Cluster) deleteEndpoint() error {
	c.setProcessName("deleting endpoint")
	c.logger.Debugln("deleting endpoint")
	if c.Endpoint == nil {
		return fmt.Errorf("there is no endpoint in the cluster")
	}
	err := c.KubeClient.Endpoints(c.Endpoint.Namespace).Delete(c.Endpoint.Name, c.deleteOptions)
	if err != nil {
		return fmt.Errorf("could not delete endpoint: %v", err)
	}
	c.logger.Infof("endpoint %q has been deleted", util.NameFromMeta(c.Endpoint.ObjectMeta))
	c.Endpoint = nil

	return nil
}

func (c *Cluster) applySecrets() error {
	c.setProcessName("applying secrets")
	secrets := c.generateUserSecrets()

	for secretUsername, secretSpec := range secrets {
		secret, err := c.KubeClient.Secrets(secretSpec.Namespace).Create(secretSpec)
		if k8sutil.ResourceAlreadyExists(err) {
			var userMap map[string]spec.PgUser
			curSecret, err2 := c.KubeClient.Secrets(secretSpec.Namespace).Get(secretSpec.Name, metav1.GetOptions{})
			if err2 != nil {
				return fmt.Errorf("could not get current secret: %v", err2)
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
	c.setProcessName("deleting secret %q", util.NameFromMeta(secret.ObjectMeta))
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
	return c.Services[Master]
}

// GetServiceReplica returns cluster's kubernetes replica Service
func (c *Cluster) GetServiceReplica() *v1.Service {
	return c.Services[Replica]
}

// GetEndpoint returns cluster's kubernetes Endpoint
func (c *Cluster) GetEndpoint() *v1.Endpoints {
	return c.Endpoint
}

// GetStatefulSet returns cluster's kubernetes StatefulSet
func (c *Cluster) GetStatefulSet() *v1beta1.StatefulSet {
	return c.Statefulset
}

// GetPodDisruptionBudget returns cluster's kubernetes PodDisruptionBudget
func (c *Cluster) GetPodDisruptionBudget() *policybeta1.PodDisruptionBudget {
	return c.PodDisruptionBudget
}

func (c *Cluster) createDatabases() error {
	c.setProcessName("creating databases")

	if len(c.Spec.Databases) == 0 {
		return nil
	}

	if err := c.initDbConn(); err != nil {
		return fmt.Errorf("could not init database connection")
	}
	defer func() {
		if err := c.closeDbConn(); err != nil {
			c.logger.Errorf("could not close database connection: %v", err)
		}
	}()

	for datname, owner := range c.Spec.Databases {
		if err := c.executeCreateDatabase(datname, owner); err != nil {
			return err
		}
	}
	return nil
}
