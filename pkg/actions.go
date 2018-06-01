package actions

import (
	"github.com/zalando-incubator/postgres-operator/pkg/cluster/types"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"k8s.io/client-go/pkg/api/v1"
)

type ActionType int

const (
	UpdateService ActionType = iota
	RecreateService
	CreateService
	DeleteService
)

var NoActions []Action = []Action{}

type SyncSecretsData struct {
	secrets map[string]*v1.Secret
}

type ServiceData struct {
	name string
	role Role
	spec *v1.Service
}

type SyncVolumesData struct {
	volumeSpec spec.Volume
}

type Action struct {
	actionType actionType
	namespace  NamespacedName
	data       interface{}
}
