package cluster

// Postgres ThirdPartyResource object i.e. Spilo

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/labels"
	"k8s.io/client-go/rest"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/retryutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/teams"
)

var (
	superuserName       = "postgres"
	replicationUsername = "replication"

	alphaNumericRegexp = regexp.MustCompile("^[a-zA-Z0-9]*$")
)

//TODO: remove struct duplication
type Config struct {
	ControllerNamespace string
	KubeClient          *kubernetes.Clientset //TODO: move clients to the better place?
	RestClient          *rest.RESTClient
	EtcdClient          etcdclient.KeysAPI
	TeamsAPIClient      *teams.TeamsAPI
}

type pgUser struct {
	name     string
	password string
	flags    []string
}

type Cluster struct {
	config      Config
	logger      *logrus.Entry
	etcdHost    string
	dockerImage string
	cluster     *spec.Postgresql
	pgUsers     map[string]pgUser

	pgDb *sql.DB
	mu   sync.Mutex
}

func New(cfg Config, spec *spec.Postgresql) *Cluster {
	lg := logrus.WithField("pkg", "cluster").WithField("cluster-name", spec.Metadata.Name)

	cluster := &Cluster{
		config:      cfg,
		cluster:     spec,
		logger:      lg,
		etcdHost:    constants.EtcdHost,
		dockerImage: constants.SpiloImage,
	}
	cluster.init()

	return cluster
}

func (c *Cluster) getReadonlyToken() (string, error) {
	// for some reason PlatformCredentialsSet creates secrets only in the default namespace
	credentialsSecret, err := c.config.KubeClient.Secrets(v1.NamespaceDefault).Get("postgresql-operator")

	if err != nil {
		return "", fmt.Errorf("Can't get credentials secret: %s", err)
	}
	data := credentialsSecret.Data

	if string(data["read-only-token-type"]) != "Bearer" {
		return "", fmt.Errorf("Wrong token type: %s", data["read-only-token-type"])
	}

	return string(data["read-only-token-secret"]), nil

}

func (c *Cluster) getTeamMembers() ([]string, error) {
	token, err := c.getReadonlyToken()
	if err != nil {
		return nil, fmt.Errorf("Can't get oauth token: %s", err)
	}

	c.config.TeamsAPIClient.OauthToken = token
	teamInfo, err := c.config.TeamsAPIClient.TeamInfo((*c.cluster.Spec).TeamId)
	if err != nil {
		return nil, fmt.Errorf("Can't get team info: %s", err)
	}

	return teamInfo.Members, nil
}

func (c *Cluster) labelsSet() labels.Set {
	return labels.Set{
		"application":   "spilo",
		"spilo-cluster": (*c.cluster).Metadata.Name,
	}
}

func (c *Cluster) credentialSecretName(username string) string {
	return fmt.Sprintf(
		"%s.%s.credentials.%s.%s",
		username,
		(*c.cluster).Metadata.Name,
		constants.TPRName,
		constants.TPRVendor)
}

func isValidUsername(username string) bool {
	return alphaNumericRegexp.MatchString(username)
}

func normalizeUserFlags(userFlags []string) (flags []string, err error) {
	uniqueFlags := make(map[string]bool)

	for _, flag := range userFlags {
		if !alphaNumericRegexp.MatchString(flag) {
			err = fmt.Errorf("User flag '%s' is not alphanumeric", flag)
			return
		} else {
			flag = strings.ToUpper(flag)
			if _, ok := uniqueFlags[flag]; !ok {
				uniqueFlags[flag] = true
			}
		}
	}

	flags = []string{}
	for k := range uniqueFlags {
		flags = append(flags, k)
	}

	return
}

func (c *Cluster) init() {
	users := (*c.cluster.Spec).Users
	c.pgUsers = make(map[string]pgUser, len(users)+2) // + [superuser and replication]

	c.pgUsers[superuserName] = pgUser{
		name:     superuserName,
		password: util.RandomPassword(constants.PasswordLength),
	}

	c.pgUsers[replicationUsername] = pgUser{
		name:     replicationUsername,
		password: util.RandomPassword(constants.PasswordLength),
	}

	for username, userFlags := range users {
		if !isValidUsername(username) {
			c.logger.Warningf("Invalid username: '%s'", username)
			continue
		}

		flags, err := normalizeUserFlags(userFlags)
		if err != nil {
			c.logger.Warningf("Invalid flags for user '%s': %s", username, err)
		}

		c.pgUsers[username] = pgUser{
			name:     username,
			password: util.RandomPassword(constants.PasswordLength),
			flags:    flags,
		}
	}

	teamMembers, err := c.getTeamMembers()
	if err != nil {
		c.logger.Errorf("Can't get list of team members: %s", err)
	} else {
		for _, username := range teamMembers {
			c.pgUsers[username] = pgUser{name: username}
		}
	}
}

func (c *Cluster) waitPodDelete() error {
	ls := c.labelsSet()

	listOptions := v1.ListOptions{
		LabelSelector: ls.String(),
	}
	return retryutil.Retry(
		constants.ResourceCheckInterval, int(constants.ResourceCheckTimeout/constants.ResourceCheckInterval),
		func() (bool, error) {
			pods, err := c.config.KubeClient.Pods((*c.cluster).Metadata.Namespace).List(listOptions)
			if err != nil {
				return false, err
			}

			return len(pods.Items) == 0, nil
		})
}

func (c *Cluster) waitStatefulsetReady() error {
	return retryutil.Retry(constants.ResourceCheckInterval, int(constants.ResourceCheckTimeout/constants.ResourceCheckInterval),
		func() (bool, error) {
			listOptions := v1.ListOptions{
				LabelSelector: c.labelsSet().String(),
			}
			ss, err := c.config.KubeClient.StatefulSets((*c.cluster).Metadata.Namespace).List(listOptions)
			if err != nil {
				return false, err
			}

			if len(ss.Items) != 1 {
				return false, fmt.Errorf("StatefulSet is not found")
			}

			return *ss.Items[0].Spec.Replicas == ss.Items[0].Status.Replicas, nil
		})
}

func (c *Cluster) waitPodLabelsReady() error {
	ls := c.labelsSet()
	namespace := (*c.cluster).Metadata.Namespace

	listOptions := v1.ListOptions{
		LabelSelector: ls.String(),
	}
	masterListOption := v1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{"spilo-role": "master"}).String(),
	}
	replicaListOption := v1.ListOptions{
		LabelSelector: labels.Merge(ls, labels.Set{"spilo-role": "replica"}).String(),
	}
	pods, err := c.config.KubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return err
	}
	podsNumber := len(pods.Items)

	return retryutil.Retry(
		constants.ResourceCheckInterval, int(constants.ResourceCheckTimeout/constants.ResourceCheckInterval),
		func() (bool, error) {
			masterPods, err := c.config.KubeClient.Pods(namespace).List(masterListOption)
			if err != nil {
				return false, err
			}
			replicaPods, err := c.config.KubeClient.Pods(namespace).List(replicaListOption)
			if err != nil {
				return false, err
			}
			if len(masterPods.Items) > 1 {
				return false, fmt.Errorf("Too many masters")
			}

			return len(masterPods.Items)+len(replicaPods.Items) == podsNumber, nil
		})
}

func (c *Cluster) Create() error {
	c.createEndpoint()
	c.createService()
	c.applySecrets()
	c.createStatefulSet()

	c.logger.Info("Waiting for cluster being ready")
	err := c.waitClusterReady()
	if err != nil {
		c.logger.Errorf("Failed to create cluster: %s", err)
		return err
	}
	c.logger.Info("Cluster is ready")

	err = c.initDbConn()
	if err != nil {
		return fmt.Errorf("Failed to init db connection: %s", err)
	}

	c.createUsers()

	return nil
}

func (c *Cluster) waitClusterReady() error {
	// TODO: wait for the first Pod only
	err := c.waitStatefulsetReady()
	if err != nil {
		return fmt.Errorf("Statuful set error: %s", err)
	}

	// TODO: wait only for master
	err = c.waitPodLabelsReady()
	if err != nil {
		return fmt.Errorf("Pod labels error: %s", err)
	}

	return nil
}

func (c *Cluster) Delete() error {
	clusterName := (*c.cluster).Metadata.Name
	namespace := (*c.cluster).Metadata.Namespace
	orphanDependents := false
	deleteOptions := &v1.DeleteOptions{
		OrphanDependents: &orphanDependents,
	}

	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	kubeClient := c.config.KubeClient

	podList, err := kubeClient.Pods(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of pods: %s", err)
	}

	err = kubeClient.StatefulSets(namespace).Delete(clusterName, deleteOptions)
	if err != nil {
		return fmt.Errorf("Can't delete statefulset: %s", err)
	}
	c.logger.Infof("Statefulset '%s' has been deleted", util.FullObjectName(namespace, clusterName))

	for _, pod := range podList.Items {
		err = kubeClient.Pods(namespace).Delete(pod.Name, deleteOptions)
		if err != nil {
			return fmt.Errorf("Error while deleting pod '%s': %s", util.FullObjectName(pod.Namespace, pod.Name), err)
		}

		c.logger.Infof("Pod '%s' has been deleted", util.FullObjectName(pod.Namespace, pod.Name))
	}

	serviceList, err := kubeClient.Services(namespace).List(listOptions)
	if err != nil {
		return fmt.Errorf("Can't get list of the services: %s", err)
	}

	for _, service := range serviceList.Items {
		err = kubeClient.Services(namespace).Delete(service.Name, deleteOptions)
		if err != nil {
			return fmt.Errorf("Can't delete service '%s': %s", util.FullObjectName(service.Namespace, service.Name), err)
		}

		c.logger.Infof("Service '%s' has been deleted", util.FullObjectName(service.Namespace, service.Name))
	}

	secretsList, err := kubeClient.Secrets(namespace).List(listOptions)
	if err != nil {
		return err
	}
	for _, secret := range secretsList.Items {
		err = kubeClient.Secrets(namespace).Delete(secret.Name, deleteOptions)
		if err != nil {
			return fmt.Errorf("Can't delete secret '%s': %s", util.FullObjectName(secret.Namespace, secret.Name), err)
		}

		c.logger.Infof("Secret '%s' has been deleted", util.FullObjectName(secret.Namespace, secret.Name))
	}

	c.waitPodDelete()

	etcdKey := fmt.Sprintf("/service/%s", clusterName)

	resp, err := c.config.EtcdClient.Delete(context.Background(),
		etcdKey,
		&etcdclient.DeleteOptions{Recursive: true})

	if err != nil {
		return fmt.Errorf("Can't delete etcd key: %s", err)
	}

	if resp == nil {
		c.logger.Warningf("No response from etcd cluster")
	}

	c.logger.Infof("Etcd key '%s' has been deleted", etcdKey)

	//TODO: Ensure objects are deleted

	return nil
}
