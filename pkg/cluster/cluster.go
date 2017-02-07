package cluster

// Postgres ThirdPartyResource object i.e. Spilo

import (
	"fmt"

	"github.com/Sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

var patroniUsers = []string{"superuser", "replication"}

//TODO: remove struct duplication
type Config struct {
	Namespace  string
	KubeClient *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient *rest.RESTClient
}

type Cluster struct {
	logger      *logrus.Entry
	config      Config
	etcdHost    string
	dockerImage string
	cluster     *spec.Postgresql
	pgUsers     []pgUser
}

type pgUser struct {
	username []byte
	password []byte
	flags    []string
}

func New(cfg Config, spec *spec.Postgresql) *Cluster {
	lg := logrus.WithField("pkg", "cluster").WithField("cluster-name", spec.Metadata.Name)

	//TODO: check if image exist
	dockerImage := fmt.Sprintf("registry.opensource.zalan.do/acid/spilo-%s", (*spec.Spec).PostgresqlParam.Version)

	cluster := &Cluster{
		config:      cfg,
		cluster:     spec,
		logger:      lg,
		etcdHost:    constants.EtcdHost,
		dockerImage: dockerImage,
	}
	cluster.init()

	return cluster
}

func (c *Cluster) labels() map[string]string {
	return map[string]string{
		"application":   "spilo",
		"spilo-cluster": (*c.cluster).Metadata.Name,
	}
}

func (c *Cluster) credentialSecretName(userName string) string {
	return fmt.Sprintf(
		"%s.%s.credentials.%s.%s",
		userName,
		(*c.cluster).Metadata.Name,
		constants.TPRName,
		constants.TPRVendor)
}

func (c *Cluster) init() {
	for _, userName := range patroniUsers {
		user := pgUser{
			username: []byte(userName),
			password: util.RandomPasswordBytes(constants.PasswordLength),
		}
		c.pgUsers = append(c.pgUsers, user)
	}

	for userName, userFlags := range (*c.cluster.Spec).Users {
		user := pgUser{
			username: []byte(userName),
			password: util.RandomPasswordBytes(constants.PasswordLength),
			flags:    userFlags,
		}
		c.pgUsers = append(c.pgUsers, user)
	}
}

func (c *Cluster) Create() error {
	c.createEndPoint()
	c.createService()
	c.applySecrets()
	c.createStatefulSet()

	//TODO: wait for "spilo-role" label to appear on each pod

	return nil
}

func (c *Cluster) Delete() error {
	clusterName := (*c.cluster).Metadata.Name
	nameSpace := c.config.Namespace
	orphanDependents := false
	deleteOptions := &v1.DeleteOptions{
		OrphanDependents: &orphanDependents,
	}

	listOptions := v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "spilo-cluster", clusterName),
	}

	kubeClient := c.config.KubeClient

	podList, err := kubeClient.Pods(nameSpace).List(listOptions)
	if err != nil {
		c.logger.Errorf("Error: %+v", err)
	}

	err = kubeClient.StatefulSets(nameSpace).Delete(clusterName, deleteOptions)
	if err != nil {
		c.logger.Errorf("Error: %+v", err)
	}
	c.logger.Infof("StatefulSet %s.%s has been deleted\n", nameSpace, clusterName)

	for _, pod := range podList.Items {
		err = kubeClient.Pods(nameSpace).Delete(pod.Name, deleteOptions)
		if err != nil {
			c.logger.Errorf("Error while deleting Pod %s: %+v", pod.Name, err)
			return err
		}

		c.logger.Infof("Pod %s.%s has been deleted\n", pod.Namespace, pod.Name)
	}

	serviceList, err := kubeClient.Services(nameSpace).List(listOptions)
	if err != nil {
		return err
	}

	for _, service := range serviceList.Items {
		err = kubeClient.Services(nameSpace).Delete(service.Name, deleteOptions)
		if err != nil {
			c.logger.Errorf("Error while deleting Service %s: %+v", service.Name, err)

			return err
		}

		c.logger.Infof("Service %s.%s has been deleted\n", service.Namespace, service.Name)
	}

	secretsList, err := kubeClient.Secrets(nameSpace).List(listOptions)
	if err != nil {
		return err
	}
	for _, secret := range secretsList.Items {
		err = kubeClient.Secrets(nameSpace).Delete(secret.Name, deleteOptions)
		if err != nil {
			c.logger.Errorf("Error while deleting Secret %s: %+v", secret.Name, err)

			return err
		}

		c.logger.Infof("Secret %s.%s has been deleted\n", secret.Namespace, secret.Name)
	}

	//TODO: delete key from etcd

	return nil
}
