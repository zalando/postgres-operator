package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"
	
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

func (c *Cluster) loadResources() error {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	services, err := c.KubeClient.Services(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of Services: %s", err)
	}
	if len(services.Items) > 1 {
		return fmt.Errorf("Too many(%d) Services for a cluster", len(services.Items))
	} else if len(services.Items) == 1 {
		c.Service = &services.Items[0]
	}

	endpoints, err := c.KubeClient.Endpoints(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of Endpoints: %s", err)
	}
	if len(endpoints.Items) > 1 {
		return fmt.Errorf("Too many(%d) Endpoints for a cluster", len(endpoints.Items))
	} else if len(endpoints.Items) == 1 {
		c.Endpoint = &endpoints.Items[0]
	}

	secrets, err := c.KubeClient.Secrets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of Secrets: %s", err)
	}
	for i, secret := range secrets.Items {
		if _, ok := c.Secrets[secret.UID]; ok {
			continue
		}
		c.Secrets[secret.UID] = &secrets.Items[i]
		c.logger.Debugf("Secret loaded, uid: %s", secret.UID)
	}

	statefulSets, err := c.KubeClient.StatefulSets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of StatefulSets: %s", err)
	}
	if len(statefulSets.Items) > 1 {
		return fmt.Errorf("Too many(%d) StatefulSets for a cluster", len(statefulSets.Items))
	} else if len(statefulSets.Items) == 1 {
		c.Statefulset = &statefulSets.Items[0]
	}

	return nil
}

func (c *Cluster) ListResources() error {
	if c.Statefulset != nil {
		c.logger.Infof("Found StatefulSet: %s (uid: %s)", util.NameFromMeta(c.Statefulset.ObjectMeta), c.Statefulset.UID)
	}

	for _, obj := range c.Secrets {
		c.logger.Infof("Found Secret: %s (uid: %s)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	if c.Endpoint != nil {
		c.logger.Infof("Found Endpoint: %s (uid: %s)", util.NameFromMeta(c.Endpoint.ObjectMeta), c.Endpoint.UID)
	}

	if c.Service != nil {
		c.logger.Infof("Found Service: %s (uid: %s)", util.NameFromMeta(c.Service.ObjectMeta), c.Service.UID)
	}

	pods, err := c.listPods()
	if err != nil {
		return fmt.Errorf("Can't get the list of Pods: %s", err)
	}

	for _, obj := range pods {
		c.logger.Infof("Found Pod: %s (uid: %s)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return fmt.Errorf("Can't get the list of PVCs: %s", err)
	}

	for _, obj := range pvcs {
		c.logger.Infof("Found PVC: %s (uid: %s)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	return nil
}

func (c *Cluster) createStatefulSet() (*v1beta1.StatefulSet, error) {
	if c.Statefulset != nil {
		return nil, fmt.Errorf("StatefulSet already exists in the cluster")
	}
	statefulSetSpec := c.genStatefulSet(c.Spec)
	statefulSet, err := c.KubeClient.StatefulSets(statefulSetSpec.Namespace).Create(statefulSetSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("StatefulSet '%s' already exists", util.NameFromMeta(statefulSetSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Statefulset = statefulSet
	c.logger.Debugf("Created new StatefulSet '%s', uid: %s", util.NameFromMeta(statefulSet.ObjectMeta), statefulSet.UID)

	return statefulSet, nil
}

func (c *Cluster) updateStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	if c.Statefulset == nil {
		return fmt.Errorf("There is no StatefulSet in the cluster")
	}
	statefulSetName := util.NameFromMeta(c.Statefulset.ObjectMeta)

	patchData, err := specPatch(newStatefulSet.Spec)
	if err != nil {
		return fmt.Errorf("Can't form patch for the StatefulSet '%s': %s", statefulSetName, err)
	}

	statefulSet, err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Patch(
		c.Statefulset.Name,
		api.MergePatchType,
		patchData, "")
	if err != nil {
		return fmt.Errorf("Can't patch StatefulSet '%s': %s", statefulSetName, err)
	}
	c.Statefulset = statefulSet

	return nil
}

func (c *Cluster) deleteStatefulSet() error {
	c.logger.Debugln("Deleting StatefulSet")
	if c.Statefulset == nil {
		return fmt.Errorf("There is no StatefulSet in the cluster")
	}

	err := c.KubeClient.StatefulSets(c.Statefulset.Namespace).Delete(c.Statefulset.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("StatefulSet '%s' has been deleted", util.NameFromMeta(c.Statefulset.ObjectMeta))
	c.Statefulset = nil

	if err := c.deletePods(); err != nil {
		return fmt.Errorf("Can't delete Pods: %s", err)
	}

	if err := c.deletePersistenVolumeClaims(); err != nil {
		return fmt.Errorf("Can't delete PersistentVolumeClaims: %s", err)
	}

	return nil
}

func (c *Cluster) createService() (*v1.Service, error) {
	if c.Service != nil {
		return nil, fmt.Errorf("Service already exists in the cluster")
	}
	serviceSpec := c.genService(c.Spec.AllowedSourceRanges)

	service, err := c.KubeClient.Services(serviceSpec.Namespace).Create(serviceSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Service '%s' already exists", util.NameFromMeta(serviceSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Service = service

	return service, nil
}

func (c *Cluster) 	updateService(newService *v1.Service) error {
	if c.Service == nil {
		return fmt.Errorf("There is no Service in the cluster")
	}
	serviceName := util.NameFromMeta(c.Service.ObjectMeta)

	patchData, err := specPatch(newService.Spec)
	if err != nil {
		return fmt.Errorf("Can't form patch for the Service '%s': %s", serviceName, err)
	}

	svc, err := c.KubeClient.Services(c.Service.Namespace).Patch(
		c.Service.Name,
		api.MergePatchType,
		patchData, "")
	if err != nil {
		return fmt.Errorf("Can't patch Service '%s': %s", serviceName, err)
	}
	c.Service = svc

	return nil
}

func (c *Cluster) deleteService() error {
	c.logger.Debugln("Deleting Service")

	if c.Service == nil {
		return fmt.Errorf("There is no Service in the cluster")
	}
	err := c.KubeClient.Services(c.Service.Namespace).Delete(c.Service.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("Service '%s' has been deleted", util.NameFromMeta(c.Service.ObjectMeta))
	c.Service = nil

	return nil
}

func (c *Cluster) createEndpoint() (*v1.Endpoints, error) {
	if c.Endpoint != nil {
		return nil, fmt.Errorf("Endpoint already exists in the cluster")
	}
	endpointsSpec := c.genEndpoints()

	endpoints, err := c.KubeClient.Endpoints(endpointsSpec.Namespace).Create(endpointsSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Endpoint '%s' already exists", util.NameFromMeta(endpointsSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Endpoint = endpoints

	return endpoints, nil
}

func (c *Cluster) updateEndpoint(newEndpoint *v1.Endpoints) error {
	//TODO: to be implemented

	return nil
}

func (c *Cluster) deleteEndpoint() error {
	c.logger.Debugln("Deleting Endpoint")
	if c.Endpoint == nil {
		return fmt.Errorf("There is no Endpoint in the cluster")
	}
	err := c.KubeClient.Endpoints(c.Endpoint.Namespace).Delete(c.Endpoint.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("Endpoint '%s' has been deleted", util.NameFromMeta(c.Endpoint.ObjectMeta))
	c.Endpoint = nil

	return nil
}

func (c *Cluster) applySecrets() error {
	secrets := c.genUserSecrets()

	for secretUsername, secretSpec := range secrets {
		secret, err := c.KubeClient.Secrets(secretSpec.Namespace).Create(secretSpec)
		if k8sutil.ResourceAlreadyExists(err) {
			var userMap map[string]spec.PgUser
			curSecret, err := c.KubeClient.Secrets(secretSpec.Namespace).Get(secretSpec.Name)
			if err != nil {
				return fmt.Errorf("Can't get current Secret: %s", err)
			}
			c.logger.Debugf("Secret '%s' already exists, fetching it's password", util.NameFromMeta(curSecret.ObjectMeta))
			if secretUsername == c.systemUsers[constants.SuperuserKeyName].Name {
				secretUsername = constants.SuperuserKeyName
				userMap = c.systemUsers
			} else if secretUsername == c.systemUsers[constants.ReplicationUserKeyName].Name  {
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
				return fmt.Errorf("Can't create Secret for user '%s': %s", secretUsername, err)
			}
			c.Secrets[secret.UID] = secret
			c.logger.Debugf("Created new Secret '%s', uid: %s", util.NameFromMeta(secret.ObjectMeta), secret.UID)
		}
	}

	return nil
}

func (c *Cluster) deleteSecret(secret *v1.Secret) error {
	c.logger.Debugf("Deleting Secret '%s'", util.NameFromMeta(secret.ObjectMeta))
	err := c.KubeClient.Secrets(secret.Namespace).Delete(secret.Name, c.deleteOptions)
	if err != nil {
		return err
	}
	c.logger.Infof("Secret '%s' has been deleted", util.NameFromMeta(secret.ObjectMeta))
	delete(c.Secrets, secret.UID)

	return err
}

func (c *Cluster) createUsers() (err error) {
	// TODO: figure out what to do with duplicate names (humans and robots) among pgUsers
	reqs := c.userSyncStrategy.ProduceSyncRequests(nil, c.pgUsers)
	err = c.userSyncStrategy.ExecuteSyncRequests(reqs, c.pgDb)
	if err != nil {
		return err
	}

	return nil
}
