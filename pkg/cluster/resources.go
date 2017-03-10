package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/resources"
)

var (
	deleteOptions    = &v1.DeleteOptions{OrphanDependents: &orphanDependents}
	orphanDependents = false
)

func getStatefulSet(clusterName spec.ClusterName, cSpec spec.PostgresSpec, etcdHost, dockerImage string) *v1beta1.StatefulSet {
	volumeSize := cSpec.Volume.Size
	volumeStorageClass := cSpec.Volume.StorageClass
	resourceList := resources.ResourceList(cSpec.Resources)
	template := resources.PodTemplate(clusterName, resourceList, dockerImage, cSpec.Version, etcdHost)
	volumeClaimTemplate := resources.VolumeClaimTemplate(volumeSize, volumeStorageClass)

	return resources.StatefulSet(clusterName, template, volumeClaimTemplate, cSpec.NumberOfInstances)
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
	if len(services.Items) > 1 {
		return fmt.Errorf("Too many(%d) Services for a cluster", len(services.Items))
	}
	c.Service = &services.Items[0]

	endpoints, err := c.config.KubeClient.Endpoints(ns).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of endpoints: %s", err)
	}
	if len(endpoints.Items) > 1 {
		return fmt.Errorf("Too many(%d) Endpoints for a cluster", len(endpoints.Items))
	}
	c.Endpoint = &endpoints.Items[0]

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
	if len(statefulSets.Items) > 1 {
		return fmt.Errorf("Too many(%d) StatefulSets for a cluster", len(statefulSets.Items))
	}
	c.Statefulset = &statefulSets.Items[0]

	return nil
}

func (c *Cluster) ListResources() error {
	c.logger.Infof("StatefulSet: %s (uid: %s)", util.NameFromMeta(c.Statefulset.ObjectMeta), c.Statefulset.UID)

	for _, obj := range c.Secrets {
		c.logger.Infof("Secret: %s (uid: %s)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	c.logger.Infof("Endpoint: %s (uid: %s)", util.NameFromMeta(c.Endpoint.ObjectMeta), c.Endpoint.UID)
	c.logger.Infof("Service: %s (uid: %s)", util.NameFromMeta(c.Service.ObjectMeta), c.Service.UID)

	pods, err := c.clusterPods()
	if err != nil {
		return fmt.Errorf("Can't get pods: %s", err)
	}

	for _, obj := range pods {
		c.logger.Infof("Pod: %s (uid: %s)", util.NameFromMeta(obj.ObjectMeta), obj.UID)
	}

	return nil
}

func (c *Cluster) createStatefulSet() (*v1beta1.StatefulSet, error) {
	if c.Statefulset != nil {
		return nil, fmt.Errorf("StatefulSet already exists in the cluster")
	}
	statefulSetSpec := getStatefulSet(c.ClusterName(), c.Spec, c.etcdHost, c.dockerImage)
	statefulSet, err := c.config.KubeClient.StatefulSets(statefulSetSpec.Namespace).Create(statefulSetSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("StatefulSet '%s' already exists", util.NameFromMeta(statefulSetSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Statefulset = statefulSet
	c.logger.Debugf("Created new StatefulSet, uid: %s", statefulSet.UID)

	return statefulSet, nil
}

func (c *Cluster) updateStatefulSet(newStatefulSet *v1beta1.StatefulSet) error {
	if c.Statefulset == nil {
		return fmt.Errorf("There is no StatefulSet in the cluster")
	}
	statefulSet, err := c.config.KubeClient.StatefulSets(newStatefulSet.Namespace).Update(newStatefulSet)
	if err != nil {
		return err
	}

	c.Statefulset = statefulSet

	return nil
}

func (c *Cluster) deleteStatefulSet() error {
	if c.Statefulset == nil {
		return fmt.Errorf("There is no StatefulSet in the cluster")
	}

	err := c.config.KubeClient.StatefulSets(c.Statefulset.Namespace).Delete(c.Statefulset.Name, deleteOptions)
	if err != nil {
		return err
	}
	c.Statefulset = nil

	return nil
}

func (c *Cluster) createService() (*v1.Service, error) {
	if c.Service != nil {
		return nil, fmt.Errorf("Service already exists in the cluster")
	}
	serviceSpec := resources.Service(c.ClusterName(), c.Spec.AllowedSourceRanges)

	service, err := c.config.KubeClient.Services(serviceSpec.Namespace).Create(serviceSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Service '%s' already exists", util.NameFromMeta(serviceSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Service = service

	return service, nil
}

func (c *Cluster) updateService(newService *v1.Service) error {
	if c.Service == nil {
		return fmt.Errorf("There is no Service in the cluster")
	}
	newService.ObjectMeta = c.Service.ObjectMeta
	newService.Spec.ClusterIP = c.Service.Spec.ClusterIP

	svc, err := c.config.KubeClient.Services(newService.Namespace).Update(newService)
	if err != nil {
		return err
	}
	c.Service = svc

	return nil
}

func (c *Cluster) deleteService() error {
	if c.Service == nil {
		return fmt.Errorf("There is no Service in the cluster")
	}
	err := c.config.KubeClient.Services(c.Service.Namespace).Delete(c.Service.Name, deleteOptions)
	if err != nil {
		return err
	}
	c.Service = nil

	return nil
}

func (c *Cluster) createEndpoint() (*v1.Endpoints, error) {
	if c.Endpoint != nil {
		return nil, fmt.Errorf("Endpoint already exists in the cluster")
	}
	endpointSpec := resources.Endpoint(c.ClusterName())

	endpoint, err := c.config.KubeClient.Endpoints(endpointSpec.Namespace).Create(endpointSpec)
	if k8sutil.ResourceAlreadyExists(err) {
		return nil, fmt.Errorf("Endpoint '%s' already exists", util.NameFromMeta(endpointSpec.ObjectMeta))
	}
	if err != nil {
		return nil, err
	}
	c.Endpoint = endpoint

	return endpoint, nil
}

func (c *Cluster) updateEndpoint(newEndpoint *v1.Endpoints) error {
	//TODO: to be implemented

	return nil
}

func (c *Cluster) deleteEndpoint() error {
	if c.Endpoint == nil {
		return fmt.Errorf("There is no Endpoint in the cluster")
	}
	err := c.config.KubeClient.Endpoints(c.Endpoint.Namespace).Delete(c.Endpoint.Name, deleteOptions)
	if err != nil {
		return err
	}
	c.Endpoint = nil

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

func (c *Cluster) createUsers() error {
	// TODO: figure out what to do with duplicate names (humans and robots) among pgUsers
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
			c.logger.Warnf("Can't create %s user '%s': %s", userType, username, err)
		}
	}

	return nil
}
