package controller

import (
	"fmt"
	"hash/crc32"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/pkg/api/v1"
	extv1beta "k8s.io/client-go/pkg/apis/extensions/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/cluster"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/config"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

func (c *Controller) makeClusterConfig() cluster.Config {
	infrastructureRoles := make(map[string]spec.PgUser)
	for k, v := range c.config.InfrastructureRoles {
		infrastructureRoles[k] = v
	}

	return cluster.Config{
		RestConfig:          c.config.RestConfig,
		OpConfig:            config.Copy(c.opConfig),
		InfrastructureRoles: infrastructureRoles,
	}
}

func thirdPartyResource(TPRName string) *extv1beta.ThirdPartyResource {
	return &extv1beta.ThirdPartyResource{
		ObjectMeta: metav1.ObjectMeta{
			//ThirdPartyResources are cluster-wide
			Name: TPRName,
		},
		Versions: []extv1beta.APIVersion{
			{Name: constants.TPRApiVersion},
		},
		Description: constants.TPRDescription,
	}
}

func (c *Controller) clusterWorkerID(clusterName spec.NamespacedName) uint32 {
	return crc32.ChecksumIEEE([]byte(clusterName.String())) % c.opConfig.Workers
}

func (c *Controller) createTPR() error {
	tpr := thirdPartyResource(constants.TPRName)

	_, err := c.KubeClient.ThirdPartyResources().Create(tpr)
	if err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return err
		}
		c.logger.Infof("thirdPartyResource %q is already registered", constants.TPRName)
	} else {
		c.logger.Infof("thirdPartyResource %q' has been registered", constants.TPRName)
	}

	return k8sutil.WaitTPRReady(c.RestClient, c.opConfig.TPR.ReadyWaitInterval, c.opConfig.TPR.ReadyWaitTimeout, c.opConfig.Namespace)
}

func (c *Controller) getInfrastructureRoles(rolesSecret *spec.NamespacedName) (result map[string]spec.PgUser, err error) {
	if *rolesSecret == (spec.NamespacedName{}) {
		// we don't have infrastructure roles defined, bail out
		return nil, nil
	}

	infraRolesSecret, err := c.KubeClient.
		Secrets(rolesSecret.Namespace).
		Get(rolesSecret.Name, metav1.GetOptions{})
	if err != nil {
		c.logger.Debugf("infrastructure roles secret name: %q", *rolesSecret)
		return nil, fmt.Errorf("could not get infrastructure roles secret: %v", err)
	}

	data := infraRolesSecret.Data
	result = make(map[string]spec.PgUser)
Users:
	// in worst case we would have one line per user
	for i := 1; i <= len(data); i++ {
		properties := []string{"user", "password", "inrole"}
		t := spec.PgUser{}
		for _, p := range properties {
			key := fmt.Sprintf("%s%d", p, i)
			if val, present := data[key]; !present {
				if p == "user" {
					// exit when the user name with the next sequence id is absent
					break Users
				}
			} else {
				s := string(val)
				switch p {
				case "user":
					t.Name = s
				case "password":
					t.Password = s
				case "inrole":
					t.MemberOf = append(t.MemberOf, s)
				default:
					c.logger.Warningf("unknown key %q", p)
				}
			}
		}

		if t.Name != "" {
			result[t.Name] = t
		}
	}

	return result, nil
}

func (c *Controller) podClusterName(pod *v1.Pod) spec.NamespacedName {
	if name, ok := pod.Labels[c.opConfig.ClusterNameLabel]; ok {
		return spec.NamespacedName{
			Namespace: pod.Namespace,
			Name:      name,
		}
	}

	return spec.NamespacedName{}
}

func (c *Controller) getClustersToMigrate(nodeName string) ([]spec.NamespacedName, error) {
	clusters := make([]spec.NamespacedName, 0)

	opts := metav1.ListOptions{
		LabelSelector: labels.Set(c.opConfig.ClusterLabels).String(),
	}
	pods, err := c.KubeClient.Pods(c.opConfig.Namespace).List(opts)
	if err != nil {
		return nil, fmt.Errorf("could not fetch list of pods: %v", err)
	}

forLoop:
	for _, pod := range pods.Items {
		role := cluster.PostgresRole(pod.Labels[c.opConfig.PodRoleLabel])

		if pod.Spec.NodeName != nodeName {
			continue
		}

		if role != cluster.Master {
			continue
		}

		podClusterName := c.podClusterName(&pod)

		for _, cl := range clusters {
			if cl == podClusterName {
				continue forLoop
			}
		}

		clusters = append(clusters, podClusterName)
	}

	return clusters, nil
}
