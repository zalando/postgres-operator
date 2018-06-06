package actions

import (
	"crypto/md5"
	"github.com/zalando-incubator/postgres-operator/pkg/cluster/types"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"k8s.io/client-go/pkg/api/v1"
)

var NoActions []Action = []Action{}

type ActionHash = [16]byte

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

type ActionData struct {
	namespace NamespacedName
}

type CreateService struct {
	common  ActionData
	service ServiceData
}

type UpdateService struct {
	common  ActionData
	service ServiceData
}

type RecreateService struct {
	common  ActionData
	service ServiceData
}

type DeleteService struct {
	common  ActionData
	service ServiceData
}

type Action interface {
	process() (bool, error)
	name() string
	hash() ActionHash
}

func (action UpdateService) process() (bool, error) {

}

func (action RecreateService) process() (bool, error) {
}

func (action CreateService) process() (bool, error) {

}

func (action DeleteService) process() (bool, error) {

}

func (action UpdateService) hash() ActionHash {
	return md5.Sum([]byte("update" + action.data.name))
}

func (action RecreateService) hash() ActionHash {
	return md5.Sum([]byte("recreate" + action.data.name))
}

func (action CreateService) hash() ActionHash {
	return md5.Sum([]byte("create" + action.data.name))
}

func (action DeleteService) hash() ActionHash {
	return md5.Sum([]byte("delete" + action.data.name))
}

func (action UpdateService) name() string {
	return fmt.Sprintf("Update service %s", action.service.name)
}

func (action RecreateService) name() string {
	return fmt.Sprintf("Recreate service %s", action.service.name)
}

func (action CreateService) name() string {
	return fmt.Sprintf("Create a new service")
}

func (action DeleteService) name() string {
	return fmt.Sprintf("Delete service %s", action.service.name)
}
