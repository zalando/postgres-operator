package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/resources"
)

var orphanDependents = false
var deleteOptions = &v1.DeleteOptions{
	OrphanDependents: &orphanDependents,
}

func (c *Cluster) LoadResources() error {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	services, err := c.config.KubeClient.Services(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of services: %s", err)
	}
	for i, service := range services.Items {
		if _, ok := c.Services[service.UID]; ok {
			continue
		}
		c.Services[service.UID] = &services.Items[i]
	}

	endpoints, err := c.config.KubeClient.Endpoints(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of endpoints: %s", err)
	}
	for i, endpoint := range endpoints.Items {
		if _, ok := c.Endpoints[endpoint.UID]; ok {
			continue
		}
		c.Endpoints[endpoint.UID] = &endpoints.Items[i]
		c.logger.Debugf("Endpoint loaded, uid: %s", endpoint.UID)
	}

	secrets, err := c.config.KubeClient.Secrets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of secrets: %s", err)
	}
	for i, secret := range secrets.Items {
		if _, ok := c.Secrets[secret.UID]; ok {
			continue
		}
		c.Secrets[secret.UID] = &secrets.Items[i]
		c.logger.Debugf("Secret loaded, uid: %s", secret.UID)
	}

	statefulSets, err := c.config.KubeClient.StatefulSets(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of stateful sets: %s", err)
	}
	for i, statefulSet := range statefulSets.Items {
		if _, ok := c.Statefulsets[statefulSet.UID]; ok {
			continue
		}
		c.Statefulsets[statefulSet.UID] = &statefulSets.Items[i]
		c.logger.Debugf("StatefulSet loaded, uid: %s", statefulSet.UID)
	}

	return nil
}

func (c *Cluster) ListResources() error {
	for _, obj := range c.Statefulsets {
		c.logger.Infof("StatefulSet: %s", util.NameFromMeta(obj.ObjectMeta))
	}

	for _, obj := range c.Secrets {
		c.logger.Infof("Secret: %s", util.NameFromMeta(obj.ObjectMeta))
	}

	for _, obj := range c.Endpoints {
		c.logger.Infof("Endpoint: %s", util.NameFromMeta(obj.ObjectMeta))
	}

	for _, obj := range c.Services {
		c.logger.Infof("Service: %s", util.NameFromMeta(obj.ObjectMeta))
	}

	pods, err := c.clusterPods()
	if err != nil {
		return fmt.Errorf("Can't get pods: %s", err)
	}

	for _, obj := range pods {
		c.logger.Infof("Pod: %s", util.NameFromMeta(obj.ObjectMeta))
	}

	return nil
}

func (c *Cluster) createStatefulSet() (*v1beta1.StatefulSet, error) {
	cSpec := c.Spec
	volumeSize := cSpec.Volume.Size
	volumeStorageClass := cSpec.Volume.StorageClass
	clusterName := c.ClusterName()
	resourceList := resources.ResourceList(cSpec.Resources)
	template := resources.PodTemplate(clusterName, resourceList, c.dockerImage, cSpec.Version, c.etcdHost)
	volumeClaimTemplate := resources.VolumeClaimTemplate(volumeSize, volumeStorageClass)
	statefulSetSpec := resources.StatefulSet(clusterName, template, volumeClaimTemplate, cSpec.NumberOfInstances)
	statefulSet, err := c.config.KubeClient.StatefulSets(statefulSetSpec.Namespace).Create(statefulSetSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("StatefulSet '%s' already exists", util.NameFromMeta(statefulSetSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Statefulsets[statefulSet.UID] = statefulSet
	c.logger.Debugf("Created new StatefulSet, uid: %s", statefulSet.UID)

	return statefulSet, nil
}

func (c *Cluster) updateStatefulSet(statefulSet *v1beta1.StatefulSet) error {
	statefulSet, err := c.config.KubeClient.StatefulSets(statefulSet.Namespace).Update(statefulSet)
	if err != nil {
		c.Statefulsets[statefulSet.UID] = statefulSet
	}

	return err
}

func (c *Cluster) deleteStatefulSet(statefulSet *v1beta1.StatefulSet) error {
	err := c.config.KubeClient.
		StatefulSets(statefulSet.Namespace).
		Delete(statefulSet.Name, deleteOptions)

	if err != nil {
		return err
	}
	delete(c.Statefulsets, statefulSet.UID)

	return nil
}

func (c *Cluster) createEndpoint() (*v1.Endpoints, error) {
	endpointSpec := resources.Endpoint(c.ClusterName())

	endpoint, err := c.config.KubeClient.Endpoints(endpointSpec.Namespace).Create(endpointSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Endpoint '%s' already exists", util.NameFromMeta(endpointSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Endpoints[endpoint.UID] = endpoint
	c.logger.Debugf("Created new endpoint, uid: %s", endpoint.UID)

	return endpoint, nil
}

func (c *Cluster) deleteEndpoint(endpoint *v1.Endpoints) error {
	err := c.config.KubeClient.Endpoints(endpoint.Namespace).Delete(endpoint.Name, deleteOptions)
	if err != nil {
		return err
	}
	delete(c.Endpoints, endpoint.UID)

	return nil
}

func (c *Cluster) createService() (*v1.Service, error) {
	serviceSpec := resources.Service(c.ClusterName(), c.Spec.AllowedSourceRanges)

	service, err := c.config.KubeClient.Services(serviceSpec.Namespace).Create(serviceSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Service '%s' already exists", util.NameFromMeta(serviceSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Services[service.UID] = service
	c.logger.Debugf("Created new service, uid: %s", service.UID)

	return service, nil
}

func (c *Cluster) deleteService(service *v1.Service) error {
	err := c.config.KubeClient.Services(service.Namespace).Delete(service.Name, deleteOptions)
	if err != nil {
		return err
	}
	delete(c.Services, service.UID)

	return nil
}

func (c *Cluster) createUsers() error {
	for username, user := range c.pgUsers {
		if username == constants.SuperuserName || username == constants.ReplicationUsername {
			continue
		}

		isHuman, err := c.createPgUser(user)
		var userType string
		if isHuman {
			userType = "human"
		} else {
			userType = "robot"
		}
		if err != nil {
			return fmt.Errorf("Can't create %s user '%s': %s", userType, username, err)
		}
	}

	return nil
}

func (c *Cluster) applySecrets() error {
	secrets, err := resources.UserSecrets(c.ClusterName(), c.pgUsers)

	if err != nil {
		return fmt.Errorf("Can't get user secrets")
	}

	for secretUsername, secretSpec := range secrets {
		secret, err := c.config.KubeClient.Secrets(secretSpec.Namespace).Create(secretSpec)
		if k8sutil.ResourceAlreadyExists(err) {
			curSecrets, err := c.config.KubeClient.Secrets(secretSpec.Namespace).Get(secretSpec.Name)
			if err != nil {
				return fmt.Errorf("Can't get current secret: %s", err)
			}
			pwdUser := c.pgUsers[secretUsername]
			pwdUser.Password = string(curSecrets.Data["password"])
			c.pgUsers[secretUsername] = pwdUser

			continue
		} else {
			if err != nil {
				return fmt.Errorf("Can't create secret for user '%s': %s", secretUsername, err)
			}
			c.Secrets[secret.UID] = secret
			c.logger.Debugf("Created new secret, uid: %s", secret.UID)
		}
	}

	return nil
}

func (c *Cluster) deleteSecret(secret *v1.Secret) error {
	err := c.config.KubeClient.Secrets(secret.Namespace).Delete(secret.Name, deleteOptions)
	if err != nil {
		return err
	}
	delete(c.Secrets, secret.UID)

	return err
}
