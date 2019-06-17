package controller

import (
	"encoding/json"
	"fmt"

	"k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/cluster"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"gopkg.in/yaml.v2"
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
		PodServiceAccount:   c.PodServiceAccount,
	}
}

func (c *Controller) clusterWorkerID(clusterName spec.NamespacedName) uint32 {
	workerID, ok := c.clusterWorkers[clusterName]
	if ok {
		return workerID
	}

	c.clusterWorkers[clusterName] = c.curWorkerID

	if c.curWorkerID == c.opConfig.Workers-1 {
		c.curWorkerID = 0
	} else {
		c.curWorkerID++
	}

	return c.clusterWorkers[clusterName]
}

func (c *Controller) createOperatorCRD(crd *apiextv1beta1.CustomResourceDefinition) error {
	if _, err := c.KubeClient.CustomResourceDefinitions().Create(crd); err != nil {
		if k8sutil.ResourceAlreadyExists(err) {
			c.logger.Infof("customResourceDefinition %q is already registered and will only be updated", crd.Name)

			patch, err := json.Marshal(crd)
			if err != nil {
				return fmt.Errorf("could not marshal new customResourceDefintion: %v", err)
			}
			if _, err := c.KubeClient.CustomResourceDefinitions().Patch(crd.Name, types.MergePatchType, patch); err != nil {
				return fmt.Errorf("could not update customResourceDefinition: %v", err)
			}
		} else {
			c.logger.Errorf("could not create customResourceDefinition %q: %v", crd.Name, err)
		}
	} else {
		c.logger.Infof("customResourceDefinition %q has been registered", crd.Name)
	}

	return wait.Poll(c.config.CRDReadyWaitInterval, c.config.CRDReadyWaitTimeout, func() (bool, error) {
		c, err := c.KubeClient.CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, cond := range c.Status.Conditions {
			switch cond.Type {
			case apiextv1beta1.Established:
				if cond.Status == apiextv1beta1.ConditionTrue {
					return true, err
				}
			case apiextv1beta1.NamesAccepted:
				if cond.Status == apiextv1beta1.ConditionFalse {
					return false, fmt.Errorf("name conflict: %v", cond.Reason)
				}
			}
		}

		return false, err
	})
}

func (c *Controller) createPostgresCRD() error {
	return c.createOperatorCRD(acidv1.PostgresCRD())
}

func (c *Controller) createConfigurationCRD() error {
	return c.createOperatorCRD(acidv1.ConfigurationCRD())
}

func readDecodedRole(s string) (*spec.PgUser, error) {
	var result spec.PgUser
	if err := yaml.Unmarshal([]byte(s), &result); err != nil {
		return nil, fmt.Errorf("could not decode yaml role: %v", err)
	}
	return &result, nil
}

func (c *Controller) getInfrastructureRoles(rolesSecret *spec.NamespacedName) (map[string]spec.PgUser, error) {
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

	secretData := infraRolesSecret.Data
	result := make(map[string]spec.PgUser)
Users:
	// in worst case we would have one line per user
	for i := 1; i <= len(secretData); i++ {
		properties := []string{"user", "password", "inrole"}
		t := spec.PgUser{Origin: spec.RoleOriginInfrastructure}
		for _, p := range properties {
			key := fmt.Sprintf("%s%d", p, i)
			if val, present := secretData[key]; !present {
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
			delete(secretData, key)
		}

		if t.Name != "" {
			if t.Password == "" {
				c.logger.Warningf("infrastructure role %q has no password defined and is ignored", t.Name)
				continue
			}
			result[t.Name] = t
		}
	}

	// perhaps we have some map entries with usernames, passwords, let's check if we have those users in the configmap
	if infraRolesMap, err := c.KubeClient.ConfigMaps(rolesSecret.Namespace).Get(rolesSecret.Name, metav1.GetOptions{}); err == nil {
		// we have a configmap with username - json description, let's read and decode it
		for role, s := range infraRolesMap.Data {
			roleDescr, err := readDecodedRole(s)
			if err != nil {
				return nil, fmt.Errorf("could not decode role description: %v", err)
			}
			// check if we have a a password in a configmap
			c.logger.Debugf("found role description for role %q: %+v", role, roleDescr)
			if passwd, ok := secretData[role]; ok {
				roleDescr.Password = string(passwd)
				delete(secretData, role)
			} else {
				c.logger.Warningf("infrastructure role %q has no password defined and is ignored", role)
				continue
			}
			roleDescr.Name = role
			roleDescr.Origin = spec.RoleOriginInfrastructure
			result[role] = *roleDescr
		}
	}

	if len(secretData) > 0 {
		c.logger.Warningf("%d unprocessed entries in the infrastructure roles secret,"+
			" checking configmap %v", len(secretData), rolesSecret.Name)
		c.logger.Info(`infrastructure role entries should be in the {key}{id} format,` +
			` where {key} can be either of "user", "password", "inrole" and the {id}` +
			` a monotonically increasing integer starting with 1`)
		c.logger.Debugf("unprocessed entries: %#v", secretData)
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
