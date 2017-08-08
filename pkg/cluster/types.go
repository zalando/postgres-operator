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

type Status struct {
	Team           string
	Cluster        string
	MasterService  *v1.Service
	ReplicaService *v1.Service
	Endpoint       *v1.Endpoints
	StatefulSet    *v1beta1.StatefulSet

	Config Config
	Status spec.PostgresStatus
	Spec   spec.PostgresSpec
	Error  error
}
