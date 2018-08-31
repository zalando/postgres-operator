package cluster

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/api/core/v1"
)

var NoActions []Action = []Action{}

type ActionHash [16]byte

var orphanDependents bool = true

var deleteOptions *metav1.DeleteOptions = &metav1.DeleteOptions{
	OrphanDependents: &orphanDependents,
}

type CreateService struct {
	ActionService
}

type UpdateService struct {
	ActionService
}

type DeleteService struct {
	ActionService
}

type MetaData struct {
	cluster   *Cluster
	namespace string
}

type ActionService struct {
	meta    MetaData
	name    string
	role    PostgresRole
	service *v1.Service
}

type Action interface {
	Process() error
	Name() string
	GetMeta() MetaData
	SetCluster(*Cluster)
}

func CheckAction(action Action) error {
	if action.GetMeta().cluster == nil {
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
	meta := action.GetMeta()
	service := action.service

	if len(service.ObjectMeta.Annotations) > 0 {
		patchData, err = servicePatchData(service.Spec, service.ObjectMeta.Annotations)
		if err != nil {
			msg := "could not prepare patch data with annotations for service %q: %v"
			return fmt.Errorf(msg, action.name, err)
		}
	} else {
		patchData, err = specPatch(service.Spec)
		if err != nil {
			msg := "could not prepare patch data for service %q: %v"
			return fmt.Errorf(msg, action.name, err)
		}
	}

	if updatedService, err = meta.cluster.KubeClient.
		Services(meta.namespace).
		Patch(action.name, types.MergePatchType, patchData, ""); err != nil {
		return err
	}

	meta.cluster.Services[action.role] = updatedService
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
	meta := action.GetMeta()

	if newService, err = meta.cluster.KubeClient.
		Services(meta.namespace).
		Create(action.service); err != nil {
		return err
	}

	meta.cluster.Services[action.role] = newService
	return nil
}

func (action DeleteService) Process() error {
	if err := CheckAction(action); err != nil {
		return err
	}
	meta := action.GetMeta()

	if err := meta.cluster.KubeClient.
		Services(meta.namespace).
		Delete(action.name, deleteOptions); err != nil {
		return err
	}

	meta.cluster.Services[action.role] = nil
	return nil
}

func (action ActionService) SetCluster(client *Cluster) {
	action.meta.cluster = client
}

func (action ActionService) GetMeta() MetaData {
	return action.meta
}

func (action UpdateService) Name() string {
	return fmt.Sprintf("Update service %s", action.name)
}

func (action CreateService) Name() string {
	return fmt.Sprintf("Create a new service")
}

func (action DeleteService) Name() string {
	return fmt.Sprintf("Delete service %s", action.name)
}
