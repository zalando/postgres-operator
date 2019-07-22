package cluster

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/api/apps/v1beta1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	v1 "k8s.io/api/core/v1"
	policybeta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/retryutil"
)

const (
	rollingUpdateStatefulsetAnnotationKey = "zalando-postgres-operator-rolling-update-required"
)

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

	for role, endpoint := range c.Endpoints {
		c.logger.Infof("found %s endpoint: %q (uid: %q)", role, util.NameFromMeta(endpoint.ObjectMeta), endpoint.UID)
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
	statefulSetSpec, err := c.generateStatefulSet(&c.Spec)
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
	if len(masterPod) == 0 {
		return fmt.Errorf("no master pod is running in the cluster")
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
	masterCandidatePod, err := c.KubeClient.Pods(c.clusterNamespace()).Get(podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("could not get master candidate pod: %v", err)
	}

	// some sanity check
	if !util.MapContains(masterCandidatePod.Labels, c.OpConfig.ClusterLabels) ||
		!util.MapContains(masterCandidatePod.Labels, map[string]string{c.OpConfig.ClusterNameLabel: c.Name}) {
		return fmt.Errorf("pod %q does not belong to cluster", podName)
	}

	if err := c.patroni.Switchover(&masterPod[0], masterCandidatePod.Name); err != nil {
		return fmt.Errorf("could not failover: %v", err)
	}

	return nil
}

// setRollingUpdateFlagForStatefulSet sets the indicator or the rolling update requirement
// in the StatefulSet annotation.
func (c *Cluster) setRollingUpdateFlagForStatefulSet(sset *v1beta1.StatefulSet, val bool) {
	anno := sset.GetAnnotations()
	if anno == nil {
		anno = make(map[string]string)
	}

	anno[rollingUpdateStatefulsetAnnotationKey] = strconv.FormatBool(val)
	sset.SetAnnotations(anno)
	c.logger.Debugf("statefulset's rolling update annotation has been set to %t", val)
}

// applyRollingUpdateFlagforStatefulSet sets the rolling update flag for the cluster's StatefulSet
// and applies that setting to the actual running cluster.
func (c *Cluster) applyRollingUpdateFlagforStatefulSet(val bool) error {
	c.setRollingUpdateFlagForStatefulSet(c.Statefulset, val)
	sset, err := c.updateStatefulSetAnnotations(c.Statefulset.GetAnnotations())
	if err != nil {
		return err
	}
	c.Statefulset = sset
	return nil
}

// getRollingUpdateFlagFromStatefulSet returns the value of the rollingUpdate flag from the passed
// StatefulSet, reverting to the default value in case of errors
func (c *Cluster) getRollingUpdateFlagFromStatefulSet(sset *v1beta1.StatefulSet, defaultValue bool) (flag bool) {
	anno := sset.GetAnnotations()
	flag = defaultValue

	stringFlag, exists := anno[rollingUpdateStatefulsetAnnotationKey]
	if exists {
		var err error
		if flag, err = strconv.ParseBool(stringFlag); err != nil {
			c.logger.Warnf("error when parsing %q annotation for the statefulset %q: expected boolean value, got %q\n",
				rollingUpdateStatefulsetAnnotationKey,
				types.NamespacedName{Namespace: sset.Namespace, Name: sset.Name},
				stringFlag)
			flag = defaultValue
		}
	}
	return flag
}

// mergeRollingUpdateFlagUsingCache returns the value of the rollingUpdate flag from the passed
// statefulset, however, the value can be cleared if there is a cached flag in the cluster that
// is set to false (the discrepancy could be a result of a failed StatefulSet update)
func (c *Cluster) mergeRollingUpdateFlagUsingCache(runningStatefulSet *v1beta1.StatefulSet) bool {
	var (
		cachedStatefulsetExists, clearRollingUpdateFromCache, podsRollingUpdateRequired bool
	)

	if c.Statefulset != nil {
		// if we reset the rolling update flag in the statefulset structure in memory but didn't manage to update
		// the actual object in Kubernetes for some reason we want to avoid doing an unnecessary update by relying
		// on the 'cached' in-memory flag.
		cachedStatefulsetExists = true
		clearRollingUpdateFromCache = !c.getRollingUpdateFlagFromStatefulSet(c.Statefulset, true)
		c.logger.Debugf("cached StatefulSet value exists, rollingUpdate flag is %t", clearRollingUpdateFromCache)
	}

	if podsRollingUpdateRequired = c.getRollingUpdateFlagFromStatefulSet(runningStatefulSet, false); podsRollingUpdateRequired {
		if cachedStatefulsetExists && clearRollingUpdateFromCache {
			c.logger.Infof("clearing the rolling update flag based on the cached information")
			podsRollingUpdateRequired = false
		} else {
			c.logger.Infof("found a statefulset with an unfinished rolling update of the pods")

		}
	}
	return podsRollingUpdateRequired
}

func (c *Cluster) updateStatefulSetAnnotations(annotations map[string]string) (*v1beta1.StatefulSet, error) {
	c.logger.Debugf("updating statefulset annotations")
	patchData, err := metaAnnotationsPatch(annotations)
	if err != nil {
		return nil, fmt.Errorf("could not form patch for the statefulset metadata: %v", err)
	}
	result, err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Patch(
		c.Statefulset.Name,
		types.MergePatchType,
		[]byte(patchData), "")
	if err != nil {
		return nil, fmt.Errorf("could not patch statefulset annotations %q: %v", patchData, err)
	}
	return result, nil

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
		return fmt.Errorf("could not patch statefulset spec %q: %v", statefulSetName, err)
	}

	if newStatefulSet.Annotations != nil {
		statefulSet, err = c.updateStatefulSetAnnotations(newStatefulSet.Annotations)
		if err != nil {
			return err
		}
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
	deletePropagationPolicy := metav1.DeletePropagationOrphan
	oldStatefulset := c.Statefulset

	options := metav1.DeleteOptions{PropagationPolicy: &deletePropagationPolicy}
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
	endpointName := util.NameFromMeta(c.Endpoints[role].ObjectMeta)
	// TODO: check if it possible to change the service type with a patch in future versions of Kubernetes
	if newService.Spec.Type != c.Services[role].Spec.Type {
		// service type has changed, need to replace the service completely.
		// we cannot use just patch the current service, since it may contain attributes incompatible with the new type.
		var (
			currentEndpoint *v1.Endpoints
			err             error
		)

		if role == Master {
			// for the master service we need to re-create the endpoint as well. Get the up-to-date version of
			// the addresses stored in it before the service is deleted (deletion of the service removes the endpoint)
			currentEndpoint, err = c.KubeClient.Endpoints(c.Namespace).Get(c.endpointName(role), metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("could not get current cluster %s endpoints: %v", role, err)
			}
		}
		err = c.KubeClient.Services(serviceName.Namespace).Delete(serviceName.Name, c.deleteOptions)
		if err != nil {
			return fmt.Errorf("could not delete service %q: %v", serviceName, err)
		}

		c.Endpoints[role] = nil
		svc, err := c.KubeClient.Services(serviceName.Namespace).Create(newService)
		if err != nil {
			return fmt.Errorf("could not create service %q: %v", serviceName, err)
		}

		c.Services[role] = svc
		if role == Master {
			// create the new endpoint using the addresses obtained from the previous one
			endpointSpec := c.generateEndpoint(role, currentEndpoint.Subsets)
			ep, err := c.KubeClient.Endpoints(endpointSpec.Namespace).Create(endpointSpec)
			if err != nil {
				return fmt.Errorf("could not create endpoint %q: %v", endpointName, err)
			}

			c.Endpoints[role] = ep
		}

		return nil
	}

	// update the service annotation in order to propagate ELB notation.
	if len(newService.ObjectMeta.Annotations) > 0 {
		if annotationsPatchData, err := metaAnnotationsPatch(newService.ObjectMeta.Annotations); err == nil {
			_, err = c.KubeClient.Services(serviceName.Namespace).Patch(
				serviceName.Name,
				types.MergePatchType,
				[]byte(annotationsPatchData), "")

			if err != nil {
				return fmt.Errorf("could not replace annotations for the service %q: %v", serviceName, err)
			}
		} else {
			return fmt.Errorf("could not form patch for the service metadata: %v", err)
		}
	}

	patchData, err := specPatch(newService.Spec)
	if err != nil {
		return fmt.Errorf("could not form patch for the service %q: %v", serviceName, err)
	}

	// update the service spec
	svc, err := c.KubeClient.Services(serviceName.Namespace).Patch(
		serviceName.Name,
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

	service, ok := c.Services[role]
	if !ok {
		c.logger.Debugf("No service for %s role was found, nothing to delete", role)
		return nil
	}

	if err := c.KubeClient.Services(service.Namespace).Delete(service.Name, c.deleteOptions); err != nil {
		return err
	}

	c.logger.Infof("%s service %q has been deleted", role, util.NameFromMeta(service.ObjectMeta))
	c.Services[role] = nil

	return nil
}

func (c *Cluster) createEndpoint(role PostgresRole) (*v1.Endpoints, error) {
	var (
		subsets []v1.EndpointSubset
	)
	c.setProcessName("creating endpoint")
	if !c.isNewCluster() {
		subsets = c.generateEndpointSubsets(role)
	} else {
		// Patroni will populate the master endpoint for the new cluster
		// The replica endpoint will be filled-in by the service selector.
		subsets = make([]v1.EndpointSubset, 0)
	}
	endpointsSpec := c.generateEndpoint(role, subsets)

	endpoints, err := c.KubeClient.Endpoints(endpointsSpec.Namespace).Create(endpointsSpec)
	if err != nil {
		return nil, fmt.Errorf("could not create %s endpoint: %v", role, err)
	}

	c.Endpoints[role] = endpoints

	return endpoints, nil
}

func (c *Cluster) generateEndpointSubsets(role PostgresRole) []v1.EndpointSubset {
	result := make([]v1.EndpointSubset, 0)
	pods, err := c.getRolePods(role)
	if err != nil {
		if role == Master {
			c.logger.Warningf("could not obtain the address for %s pod: %v", role, err)
		} else {
			c.logger.Warningf("could not obtain the addresses for %s pods: %v", role, err)
		}
		return result
	}

	endPointAddresses := make([]v1.EndpointAddress, 0)
	for _, pod := range pods {
		endPointAddresses = append(endPointAddresses, v1.EndpointAddress{IP: pod.Status.PodIP})
	}
	if len(endPointAddresses) > 0 {
		result = append(result, v1.EndpointSubset{
			Addresses: endPointAddresses,
			Ports:     []v1.EndpointPort{{Name: "postgresql", Port: 5432, Protocol: "TCP"}},
		})
	} else if role == Master {
		c.logger.Warningf("master is not running, generated master endpoint does not contain any addresses")
	}

	return result
}

func (c *Cluster) createPodDisruptionBudget() (*policybeta1.PodDisruptionBudget, error) {
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
	if c.PodDisruptionBudget == nil {
		return fmt.Errorf("there is no pod disruption budget in the cluster")
	}

	if err := c.deletePodDisruptionBudget(); err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}

	newPdb, err := c.KubeClient.
		PodDisruptionBudgets(pdb.Namespace).
		Create(pdb)
	if err != nil {
		return fmt.Errorf("could not create pod disruption budget: %v", err)
	}
	c.PodDisruptionBudget = newPdb

	return nil
}

func (c *Cluster) deletePodDisruptionBudget() error {
	c.logger.Debug("deleting pod disruption budget")
	if c.PodDisruptionBudget == nil {
		return fmt.Errorf("there is no pod disruption budget in the cluster")
	}

	pdbName := util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta)
	err := c.KubeClient.
		PodDisruptionBudgets(c.PodDisruptionBudget.Namespace).
		Delete(c.PodDisruptionBudget.Name, c.deleteOptions)
	if err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}
	c.logger.Infof("pod disruption budget %q has been deleted", util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta))
	c.PodDisruptionBudget = nil

	err = retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			_, err2 := c.KubeClient.PodDisruptionBudgets(pdbName.Namespace).Get(pdbName.Name, metav1.GetOptions{})
			if err2 == nil {
				return false, nil
			}
			if k8sutil.ResourceNotFound(err2) {
				return true, nil
			}
			return false, err2
		})
	if err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}

	return nil
}

func (c *Cluster) deleteEndpoint(role PostgresRole) error {
	c.setProcessName("deleting endpoint")
	c.logger.Debugln("deleting endpoint")
	if c.Endpoints[role] == nil {
		return fmt.Errorf("there is no %s endpoint in the cluster", role)
	}

	if err := c.KubeClient.Endpoints(c.Endpoints[role].Namespace).Delete(c.Endpoints[role].Name, c.deleteOptions); err != nil {
		return fmt.Errorf("could not delete endpoint: %v", err)
	}

	c.logger.Infof("endpoint %q has been deleted", util.NameFromMeta(c.Endpoints[role].ObjectMeta))

	c.Endpoints[role] = nil

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
	return c.syncRoles()
}

func (c *Cluster) createLogicalBackupJob() (err error) {

	c.setProcessName("creating a k8s cron job for logical backups")

	logicalBackupJobSpec, err := c.generateLogicalBackupJob()
	if err != nil {
		return fmt.Errorf("could not generate k8s cron job spec: %v", err)
	}
	c.logger.Debugf("Generated cronJobSpec: %v", logicalBackupJobSpec)

	_, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Create(logicalBackupJobSpec)
	if err != nil {
		return fmt.Errorf("could not create k8s cron job: %v", err)
	}

	return nil
}

func (c *Cluster) patchLogicalBackupJob(newJob *batchv1beta1.CronJob) error {
	c.setProcessName("patching logical backup job")

	patchData, err := specPatch(newJob.Spec)
	if err != nil {
		return fmt.Errorf("could not form patch for the logical backup job: %v", err)
	}

	// update the backup job spec
	_, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Patch(
		c.getLogicalBackupJobName(),
		types.MergePatchType,
		patchData, "")
	if err != nil {
		return fmt.Errorf("could not patch logical backup job: %v", err)
	}

	return nil
}

func (c *Cluster) deleteLogicalBackupJob() error {

	c.logger.Info("removing the logical backup job")

	return c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Delete(c.getLogicalBackupJobName(), c.deleteOptions)
}

// GetServiceMaster returns cluster's kubernetes master Service
func (c *Cluster) GetServiceMaster() *v1.Service {
	return c.Services[Master]
}

// GetServiceReplica returns cluster's kubernetes replica Service
func (c *Cluster) GetServiceReplica() *v1.Service {
	return c.Services[Replica]
}

// GetEndpointMaster returns cluster's kubernetes master Endpoint
func (c *Cluster) GetEndpointMaster() *v1.Endpoints {
	return c.Endpoints[Master]
}

// GetEndpointReplica returns cluster's kubernetes master Endpoint
func (c *Cluster) GetEndpointReplica() *v1.Endpoints {
	return c.Endpoints[Replica]
}

// GetStatefulSet returns cluster's kubernetes StatefulSet
func (c *Cluster) GetStatefulSet() *v1beta1.StatefulSet {
	return c.Statefulset
}

// GetPodDisruptionBudget returns cluster's kubernetes PodDisruptionBudget
func (c *Cluster) GetPodDisruptionBudget() *policybeta1.PodDisruptionBudget {
	return c.PodDisruptionBudget
}
