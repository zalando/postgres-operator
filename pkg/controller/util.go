package controller

import (
	"fmt"

	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/pkg/api/v1"

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

func (c *Controller) createCRD() error {
	crd := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: constants.CRDResource + "." + constants.CRDGroup,
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group:   constants.CRDGroup,
			Version: constants.CRDApiVersion,
			Names: apiextv1beta1.CustomResourceDefinitionNames{
				Plural:     constants.CRDResource,
				Singular:   constants.CRDKind,
				ShortNames: []string{constants.CRDShort},
				Kind:       constants.CRDKind,
				ListKind:   constants.CRDKind + "List",
			},
			Scope: apiextv1beta1.NamespaceScoped,
		},
	}

	if _, err := c.KubeClient.CustomResourceDefinitions().Create(crd); err != nil {
		if !k8sutil.ResourceAlreadyExists(err) {
			return fmt.Errorf("could not create customResourceDefinition: %v", err)
		}
		c.logger.Infof("customResourceDefinition %q is already registered", crd.Name)
	} else {
		c.logger.Infof("customResourceDefinition %q has been registered", crd.Name)
	}

	return wait.Poll(c.opConfig.CRD.ReadyWaitInterval, c.opConfig.CRD.ReadyWaitTimeout, func() (bool, error) {
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
		t := spec.PgUser{Origin: spec.RoleOriginInfrastructure}
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

			delete(data, key)
		}

		if t.Name != "" {
			result[t.Name] = t
		}
	}

	if len(data) != 0 {
		c.logger.Warningf("%d unprocessed entries in the infrastructure roles' secret", len(data))
		c.logger.Info(`infrastructure role entries should be in the {key}{id} format, where {key} can be either of "user", "password", "inrole" and the {id} a monotonically increasing integer starting with 1`)
		c.logger.Debugf("unprocessed entries: %#v", data)
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
