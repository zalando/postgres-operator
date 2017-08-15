package cluster

import (
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

type postgresRole string

const (
	master  postgresRole = "master"
	replica postgresRole = "replica"
)

type Interface interface {
	ExecCommand(*spec.NamespacedName, ...string) (string, error)
	GetStatus() *spec.ClusterStatus
	GetServiceMaster() *v1.Service
	GetServiceReplica() *v1.Service
	GetEndpoint() *v1.Endpoints
	GetStatefulSet() *v1beta1.StatefulSet
	ReceivePodEvent(spec.PodEvent)

	Sync() error
	Create() error
	Update(*spec.Postgresql) error
	Delete() error
	Migration() error

	Run(<-chan struct{})
}
