package cluster

import (
	"crypto/md5"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

var NoActions []Action = []Action{}

type ActionHash = [16]byte

var orphanDependents bool = true

var deleteOptions *metav1.DeleteOptions = &metav1.DeleteOptions{
	OrphanDependents: &orphanDependents,
}

type SyncSecretsData struct {
	secrets map[string]*v1.Secret
}

type ServiceData struct {
	name    string
	role    PostgresRole
	service *v1.Service
}

type SyncVolumesData struct {
	volumeSpec spec.Volume
}

type ActionData struct {
	cluster   *Cluster
	namespace string
}

type CreateService struct {
	common  ActionData
	service ServiceData
}

type UpdateService struct {
	common  ActionData
	service ServiceData
}

type DeleteService struct {
	common  ActionData
	service ServiceData
}

type Action interface {
	Process() error
	Name() string
	Hash() ActionHash
	GetCommon() ActionData
	SetCluster(*Cluster)
}

func CheckAction(action Action) error {
	if action.GetCommon().cluster == nil {
		return fmt.Errorf("no valid cluster for %v", action)
	}

	return nil
}

func (action UpdateService) Process() error {
	var (
		err            error
		patchData      []byte
		updatedService *v1.Service
	)
	if err := CheckAction(action); err != nil {
		return err
	}
	common := action.GetCommon()
	service := action.service.service

	if len(service.ObjectMeta.Annotations) > 0 {
		patchData, err = servicePatchData(service.Spec, service.ObjectMeta.Annotations)
		if err != nil {
			msg := "could not prepare patch data with annotations for service %q: %v"
			return fmt.Errorf(msg, action.service.name, err)
		}
	} else {
		patchData, err = specPatch(service.Spec)
		if err != nil {
			msg := "could not prepare patch data for service %q: %v"
			return fmt.Errorf(msg, action.service.name, err)
		}
	}

	if updatedService, err = common.cluster.KubeClient.
		Services(common.namespace).
		Patch(
			action.service.name,
			types.MergePatchType,
			patchData,
			""); err != nil {
		return err
	}

	common.cluster.Services[action.service.role] = updatedService
	return nil
}

func (action CreateService) Process() error {
	var (
		err        error
		newService *v1.Service
	)

	if err := CheckAction(action); err != nil {
		return err
	}
	common := action.GetCommon()

	if newService, err = common.cluster.KubeClient.
		Services(common.namespace).
		Create(action.service.service); err != nil {
		return err
	}

	common.cluster.Services[action.service.role] = newService
	return nil
}

func (action DeleteService) Process() error {
	if err := CheckAction(action); err != nil {
		return err
	}
	common := action.GetCommon()

	if err := common.cluster.KubeClient.
		Services(common.namespace).
		Delete(action.service.name, deleteOptions); err != nil {
		return err
	}

	common.cluster.Services[action.service.role] = nil
	return nil
}

func (action UpdateService) SetCluster(client *Cluster) {
	action.common.cluster = client
}

func (action CreateService) SetCluster(client *Cluster) {
	action.common.cluster = client
}

func (action DeleteService) SetCluster(client *Cluster) {
	action.common.cluster = client
}

func (action UpdateService) GetCommon() ActionData {
	return action.common
}

func (action CreateService) GetCommon() ActionData {
	return action.common
}

func (action DeleteService) GetCommon() ActionData {
	return action.common
}

func (action UpdateService) Hash() ActionHash {
	return md5.Sum([]byte("update" + action.service.name))
}

func (action CreateService) Hash() ActionHash {
	return md5.Sum([]byte("create" + action.service.name))
}

func (action DeleteService) Hash() ActionHash {
	return md5.Sum([]byte("delete" + action.service.name))
}

func (action UpdateService) Name() string {
	return fmt.Sprintf("Update service %s", action.service.name)
}

func (action CreateService) Name() string {
	return fmt.Sprintf("Create a new service")
}

func (action DeleteService) Name() string {
	return fmt.Sprintf("Delete service %s", action.service.name)
}
