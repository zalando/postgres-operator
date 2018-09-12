package cluster

import (
	"fmt"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"k8s.io/api/core/v1"
)

var NoActions []Action = []Action{}

type MetaData struct {
	cluster   *Cluster
	namespace string
}

type CreateSecret struct {
	ActionSecret
}

func NewCreateSecret(username string, secret *v1.Secret, cluster *Cluster) CreateSecret {
	return CreateSecret{ActionSecret{
		meta: MetaData{
			cluster: cluster,
		},
		secretUsername: username,
		secret:         secret,
	}}
}

func NewUpdateSecret(username string, secret *v1.Secret, cluster *Cluster) UpdateSecret {
	return UpdateSecret{ActionSecret{
		meta: MetaData{
			cluster: cluster,
		},
		secretUsername: username,
		secret:         secret,
	}}
}

type UpdateSecret struct {
	ActionSecret
}

type ActionSecret struct {
	meta           MetaData
	secretUsername string
	secret         *v1.Secret
}

type Action interface {
	Name() string
	Validate() error
	Apply() error
}

func (action CreateSecret) Apply() error {
	cluster := action.meta.cluster
	secret, err := cluster.KubeClient.
		Secrets(action.secret.Namespace).
		Create(action.secret)

	if err != nil {
		return fmt.Errorf("Cannot apply action %s: %v", action.Name(), err)
	}

	cluster.Secrets[secret.UID] = secret
	cluster.logger.Debugf(
		"created new secret %q, uid: %q",
		util.NameFromMeta(secret.ObjectMeta),
		secret.UID)

	return nil
}

func (action ActionSecret) Validate() error {
	if action.secret.Data["username"] == nil {
		return fmt.Errorf("Field 'username' is empty for %v", action.secret)
	}

	return nil
}

func (action CreateSecret) Name() string {
	return fmt.Sprintf("Create secret %v", action.secret)
}

func (action UpdateSecret) Apply() error {
	cluster := action.meta.cluster
	user := cluster.getSecretUser(action.secretUsername)

	// if this secret belongs to the infrastructure role and the password has
	// changed - replace it in the secret
	updateSecret := (user.Password != string(action.secret.Data["password"]) &&
		user.Origin == spec.RoleOriginInfrastructure)

	if updateSecret {
		msg := "Updating the secret %q from the infrastructure roles"
		cluster.logger.Debugf(msg, action.secret.Name)

		_, err := cluster.KubeClient.
			Secrets(action.secret.Namespace).
			Update(action.secret)

		if err != nil {
			msg = "Could not update infrastructure role secret for role %q: %v"
			return fmt.Errorf(msg, action.secretUsername, err)
		}
	} else {
		// for non-infrastructure role - update the role with the password from
		// the secret
		user.Password = string(action.secret.Data["password"])
		cluster.setSecretUser(action.secretUsername, user)
	}

	return nil
}

func (action UpdateSecret) Name() string {
	return fmt.Sprintf("Update secret %v", action.secret)
}
