package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/retryutil"
)

const (
	rollingUpdatePodAnnotationKey = "zalando-postgres-operator-rolling-update-required"
)

func (c *Cluster) listResources() error {
	if c.PodDisruptionBudget != nil {
		c.logger.Infof("found pod disruption budget: %q (uid: %q)", util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta), c.PodDisruptionBudget.UID)
	}

	if c.Statefulset != nil {
		c.logger.Infof("found statefulset: %q (uid: %q)", util.NameFromMeta(c.Statefulset.ObjectMeta), c.Statefulset.UID)
	}

	for _, obj := range c.Secrets {
		c.logger.Infof("found secret: %q (uid: %q) namesapce: %s", util.NameFromMeta(obj.ObjectMeta), obj.UID, obj.ObjectMeta.Namespace)
	}

	if !c.patroniKubernetesUseConfigMaps() {
		for role, endpoint := range c.Endpoints {
			c.logger.Infof("found %s endpoint: %q (uid: %q)", role, util.NameFromMeta(endpoint.ObjectMeta), endpoint.UID)
		}
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

func (c *Cluster) createStatefulSet() (*appsv1.StatefulSet, error) {
	c.setProcessName("creating statefulset")
	// check if it's allowed that spec contains initContainers
	if c.Spec.InitContainers != nil && len(c.Spec.InitContainers) > 0 &&
		c.OpConfig.EnableInitContainers != nil && !(*c.OpConfig.EnableInitContainers) {
		return nil, fmt.Errorf("initContainers specified but disabled in configuration")
	}
	// check if it's allowed that spec contains sidecars
	if c.Spec.Sidecars != nil && len(c.Spec.Sidecars) > 0 &&
		c.OpConfig.EnableSidecars != nil && !(*c.OpConfig.EnableSidecars) {
		return nil, fmt.Errorf("sidecar containers specified but disabled in configuration")
	}

	statefulSetSpec, err := c.generateStatefulSet(&c.Spec)
	if err != nil {
		return nil, fmt.Errorf("could not generate statefulset: %v", err)
	}
	statefulSet, err := c.KubeClient.StatefulSets(statefulSetSpec.Namespace).Create(
		context.TODO(),
		statefulSetSpec,
		metav1.CreateOptions{})
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

func (c *Cluster) preScaleDown(newStatefulSet *appsv1.StatefulSet) error {
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
	masterCandidatePod, err := c.KubeClient.Pods(c.clusterNamespace()).Get(context.TODO(), podName, metav1.GetOptions{})
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

func (c *Cluster) updateStatefulSet(newStatefulSet *appsv1.StatefulSet) error {
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
		context.TODO(),
		c.Statefulset.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
		"")
	if err != nil {
		return fmt.Errorf("could not patch statefulset spec %q: %v", statefulSetName, err)
	}

	c.Statefulset = statefulSet

	return nil
}

// replaceStatefulSet deletes an old StatefulSet and creates the new using spec in the PostgreSQL CRD.
func (c *Cluster) replaceStatefulSet(newStatefulSet *appsv1.StatefulSet) error {
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
	err := c.KubeClient.StatefulSets(oldStatefulset.Namespace).Delete(context.TODO(), oldStatefulset.Name, options)
	if err != nil {
		return fmt.Errorf("could not delete statefulset %q: %v", statefulSetName, err)
	}
	// make sure we clear the stored statefulset status if the subsequent create fails.
	c.Statefulset = nil
	// wait until the statefulset is truly deleted
	c.logger.Debugf("waiting for the statefulset to be deleted")

	err = retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			_, err2 := c.KubeClient.StatefulSets(oldStatefulset.Namespace).Get(context.TODO(), oldStatefulset.Name, metav1.GetOptions{})
			if err2 == nil {
				return false, nil
			}
			if k8sutil.ResourceNotFound(err2) {
				return true, nil
			}
			return false, err2
		})
	if err != nil {
		return fmt.Errorf("could not delete statefulset: %v", err)
	}

	// create the new statefulset with the desired spec. It would take over the remaining pods.
	createdStatefulset, err := c.KubeClient.StatefulSets(newStatefulSet.Namespace).Create(context.TODO(), newStatefulSet, metav1.CreateOptions{})
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

	err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Delete(context.TODO(), c.Statefulset.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("statefulset %q has been deleted", util.NameFromMeta(c.Statefulset.ObjectMeta))
	c.Statefulset = nil

	if err := c.deletePods(); err != nil {
		return fmt.Errorf("could not delete pods: %v", err)
	}

	if err := c.deletePersistentVolumeClaims(); err != nil {
		return fmt.Errorf("could not delete PersistentVolumeClaims: %v", err)
	}

	return nil
}

func (c *Cluster) createService(role PostgresRole) (*v1.Service, error) {
	c.setProcessName("creating %v service", role)

	serviceSpec := c.generateService(role, &c.Spec)
	service, err := c.KubeClient.Services(serviceSpec.Namespace).Create(context.TODO(), serviceSpec, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	c.Services[role] = service
	return service, nil
}

func (c *Cluster) updateService(role PostgresRole, oldService *v1.Service, newService *v1.Service) (*v1.Service, error) {
	var (
		svc *v1.Service
		err error
	)

	c.setProcessName("updating %v service", role)

	serviceName := util.NameFromMeta(oldService.ObjectMeta)

	// update the service annotation in order to propagate ELB notation.
	if len(newService.ObjectMeta.Annotations) > 0 {
		if annotationsPatchData, err := metaAnnotationsPatch(newService.ObjectMeta.Annotations); err == nil {
			_, err = c.KubeClient.Services(serviceName.Namespace).Patch(
				context.TODO(),
				serviceName.Name,
				types.MergePatchType,
				[]byte(annotationsPatchData),
				metav1.PatchOptions{},
				"")

			if err != nil {
				return nil, fmt.Errorf("could not replace annotations for the service %q: %v", serviceName, err)
			}
		} else {
			return nil, fmt.Errorf("could not form patch for the service metadata: %v", err)
		}
	}

	// now, patch the service spec, but when disabling LoadBalancers do update instead
	// patch does not work because of LoadBalancerSourceRanges field (even if set to nil)
	oldServiceType := oldService.Spec.Type
	newServiceType := newService.Spec.Type
	if newServiceType == "ClusterIP" && newServiceType != oldServiceType {
		newService.ResourceVersion = oldService.ResourceVersion
		newService.Spec.ClusterIP = oldService.Spec.ClusterIP
		svc, err = c.KubeClient.Services(serviceName.Namespace).Update(context.TODO(), newService, metav1.UpdateOptions{})
		if err != nil {
			return nil, fmt.Errorf("could not update service %q: %v", serviceName, err)
		}
	} else {
		patchData, err := specPatch(newService.Spec)
		if err != nil {
			return nil, fmt.Errorf("could not form patch for the service %q: %v", serviceName, err)
		}

		svc, err = c.KubeClient.Services(serviceName.Namespace).Patch(
			context.TODO(), serviceName.Name, types.MergePatchType, patchData, metav1.PatchOptions{}, "")
		if err != nil {
			return nil, fmt.Errorf("could not patch service %q: %v", serviceName, err)
		}
	}

	return svc, nil
}

func (c *Cluster) deleteService(role PostgresRole) error {
	c.logger.Debugf("deleting service %s", role)

	service, ok := c.Services[role]
	if !ok {
		c.logger.Debugf("No service for %s role was found, nothing to delete", role)
		return nil
	}

	if err := c.KubeClient.Services(service.Namespace).Delete(context.TODO(), service.Name, c.deleteOptions); err != nil {
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

	endpoints, err := c.KubeClient.Endpoints(endpointsSpec.Namespace).Create(context.TODO(), endpointsSpec, metav1.CreateOptions{})
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

func (c *Cluster) createPodDisruptionBudget() (*policyv1.PodDisruptionBudget, error) {
	podDisruptionBudgetSpec := c.generatePodDisruptionBudget()
	podDisruptionBudget, err := c.KubeClient.
		PodDisruptionBudgets(podDisruptionBudgetSpec.Namespace).
		Create(context.TODO(), podDisruptionBudgetSpec, metav1.CreateOptions{})

	if err != nil {
		return nil, err
	}
	c.PodDisruptionBudget = podDisruptionBudget

	return podDisruptionBudget, nil
}

func (c *Cluster) updatePodDisruptionBudget(pdb *policyv1.PodDisruptionBudget) error {
	if c.PodDisruptionBudget == nil {
		return fmt.Errorf("there is no pod disruption budget in the cluster")
	}

	if err := c.deletePodDisruptionBudget(); err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}

	newPdb, err := c.KubeClient.
		PodDisruptionBudgets(pdb.Namespace).
		Create(context.TODO(), pdb, metav1.CreateOptions{})
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
		Delete(context.TODO(), c.PodDisruptionBudget.Name, c.deleteOptions)
	if err != nil {
		return fmt.Errorf("could not delete pod disruption budget: %v", err)
	}
	c.logger.Infof("pod disruption budget %q has been deleted", util.NameFromMeta(c.PodDisruptionBudget.ObjectMeta))
	c.PodDisruptionBudget = nil

	err = retryutil.Retry(c.OpConfig.ResourceCheckInterval, c.OpConfig.ResourceCheckTimeout,
		func() (bool, error) {
			_, err2 := c.KubeClient.PodDisruptionBudgets(pdbName.Namespace).Get(context.TODO(), pdbName.Name, metav1.GetOptions{})
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

	if err := c.KubeClient.Endpoints(c.Endpoints[role].Namespace).Delete(
		context.TODO(), c.Endpoints[role].Name, c.deleteOptions); err != nil {
		return fmt.Errorf("could not delete endpoint: %v", err)
	}

	c.logger.Infof("endpoint %q has been deleted", util.NameFromMeta(c.Endpoints[role].ObjectMeta))

	c.Endpoints[role] = nil

	return nil
}

func (c *Cluster) deleteSecrets() error {
	c.setProcessName("deleting secrets")
	errors := make([]string, 0)

	for uid, secret := range c.Secrets {
		err := c.deleteSecret(uid, *secret)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%v", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("could not delete all secrets: %v", strings.Join(errors, `', '`))
	}

	return nil
}

func (c *Cluster) deleteSecret(uid types.UID, secret v1.Secret) error {
	c.setProcessName("deleting secret")
	secretName := util.NameFromMeta(secret.ObjectMeta)
	c.logger.Debugf("deleting secret %q", secretName)
	err := c.KubeClient.Secrets(secret.Namespace).Delete(context.TODO(), secret.Name, c.deleteOptions)
	if err != nil {
		return fmt.Errorf("could not delete secret %q: %v", secretName, err)
	}
	c.logger.Infof("secret %q has been deleted", secretName)
	c.Secrets[uid] = nil

	return nil
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

	_, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Create(context.TODO(), logicalBackupJobSpec, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("could not create k8s cron job: %v", err)
	}

	return nil
}

func (c *Cluster) patchLogicalBackupJob(newJob *batchv1.CronJob) error {
	c.setProcessName("patching logical backup job")

	patchData, err := specPatch(newJob.Spec)
	if err != nil {
		return fmt.Errorf("could not form patch for the logical backup job: %v", err)
	}

	// update the backup job spec
	_, err = c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Patch(
		context.TODO(),
		c.getLogicalBackupJobName(),
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{},
		"")
	if err != nil {
		return fmt.Errorf("could not patch logical backup job: %v", err)
	}

	return nil
}

func (c *Cluster) deleteLogicalBackupJob() error {

	c.logger.Info("removing the logical backup job")

	return c.KubeClient.CronJobsGetter.CronJobs(c.Namespace).Delete(context.TODO(), c.getLogicalBackupJobName(), c.deleteOptions)
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

// GetEndpointReplica returns cluster's kubernetes replica Endpoint
func (c *Cluster) GetEndpointReplica() *v1.Endpoints {
	return c.Endpoints[Replica]
}

// GetStatefulSet returns cluster's kubernetes StatefulSet
func (c *Cluster) GetStatefulSet() *appsv1.StatefulSet {
	return c.Statefulset
}

// GetPodDisruptionBudget returns cluster's kubernetes PodDisruptionBudget
func (c *Cluster) GetPodDisruptionBudget() *policyv1.PodDisruptionBudget {
	return c.PodDisruptionBudget
}
