package resources

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/config"

	rbacv1 "k8s.io/api/rbac/v1"
)

// Config contains operator-wide clients and configuration used from a cluster. TODO: remove struct duplication.
type Config struct {
	OpConfig                     config.Config
	RestConfig                   *rest.Config
	InfrastructureRoles          map[string]spec.PgUser // inherited from the controller
	PodServiceAccount            *v1.ServiceAccount
	PodServiceAccountRoleBinding *rbacv1.RoleBinding
}
