package cluster

import (
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/filesystems"
	"github.com/zalando-incubator/postgres-operator/pkg/util/volumes"
)

func (c *Cluster) listPersistentVolumeClaims() ([]v1.PersistentVolumeClaim, error) {
	ns := c.Metadata.Namespace
	listOptions := v1.ListOptions{
		LabelSelector: c.labelsSet().String(),
	}

	pvcs, err := c.KubeClient.PersistentVolumeClaims(ns).List(listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not list of PersistentVolumeClaims: %v", err)
	}
	return pvcs.Items, nil
}

func (c *Cluster) deletePersistenVolumeClaims() error {
	c.logger.Debugln("Deleting PVCs")
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	for _, pvc := range pvcs {
		c.logger.Debugf("Deleting PVC '%s'", util.NameFromMeta(pvc.ObjectMeta))
		if err := c.KubeClient.PersistentVolumeClaims(pvc.Namespace).Delete(pvc.Name, c.deleteOptions); err != nil {
			c.logger.Warningf("could not delete PersistentVolumeClaim: %v", err)
		}
	}
	if len(pvcs) > 0 {
		c.logger.Debugln("PVCs have been deleted")
	} else {
		c.logger.Debugln("No PVCs to delete")
	}

	return nil
}

func (c *Cluster) listPersistentVolumes() ([]*v1.PersistentVolume, error) {
	result := make([]*v1.PersistentVolume, 0)

	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return nil, fmt.Errorf("could not list cluster's PersistentVolumeClaims: %v", err)
	}
	lastPodIndex := *c.Statefulset.Spec.Replicas - 1
	for _, pvc := range pvcs {
		lastDash := strings.LastIndex(pvc.Name, "-")
		if lastDash > 0 && lastDash < len(pvc.Name)-1 {
			if pvcNumber, err := strconv.Atoi(pvc.Name[lastDash+1:]); err != nil {
				return nil, fmt.Errorf("could not convert last part of the persistent volume claim name %s to a number", pvc.Name)
			} else {
				if int32(pvcNumber) > lastPodIndex {
					c.logger.Debugf("Skipping persistent volume %s corresponding to a non-running pods", pvc.Name)
					continue
				}
			}

		}
		pv, err := c.KubeClient.PersistentVolumes().Get(pvc.Spec.VolumeName)
		if err != nil {
			return nil, fmt.Errorf("could not get PersistentVolume: %v", err)
		}
		result = append(result, pv)
	}

	return result, nil
}

// resizeVolumes resize persistent volumes compatible with the given resizer interface
func (c *Cluster) resizeVolumes(newVolume spec.Volume, resizers []volumes.VolumeResizer) error {
	totalCompatible := 0
	newQuantity, err := resource.ParseQuantity(newVolume.Size)
	if err != nil {
		return fmt.Errorf("could not parse volume size: %v", err)
	}
	pvs, newSize, err := c.listVolumesWithManifestSize(newVolume)
	if err != nil {
		return fmt.Errorf("could not list persistent volumes: %v", err)
	}
	for _, pv := range pvs {
		volumeSize := quantityToGigabyte(pv.Spec.Capacity[v1.ResourceStorage])
		if volumeSize > newSize {
			return fmt.Errorf("cannot shrink persistent volume")
		}
		if volumeSize == newSize {
			continue
		}
		for _, resizer := range resizers {
			if !resizer.VolumeBelongsToProvider(pv) {
				continue
			}
			totalCompatible += 1
			if !resizer.IsConnectedToProvider() {
				err := resizer.ConnectToProvider()
				if err != nil {
					return fmt.Errorf("could not connect to the volume provider: %v", err)
				}
				defer resizer.DisconnectFromProvider()
			}
			awsVolumeId, err := resizer.GetProviderVolumeID(pv)
			if err != nil {
				return err
			}
			c.logger.Debugf("updating persistent volume %s to %d", pv.Name, newSize)
			if err := resizer.ResizeVolume(awsVolumeId, newSize); err != nil {
				return fmt.Errorf("could not resize EBS volume %s: %v", awsVolumeId, err)
			}
			c.logger.Debugf("resizing the filesystem on the volume %s", pv.Name)
			podName := getPodNameFromPersistentVolume(pv)
			if err := c.resizePostgresFilesystem(podName, []filesystems.FilesystemResizer{&filesystems.Ext234Resize{}}); err != nil {
				return fmt.Errorf("could not resize the filesystem on pod '%s': %v", podName, err)
			}
			c.logger.Debugf("filesystem resize successfull on volume %s", pv.Name)
			pv.Spec.Capacity[v1.ResourceStorage] = newQuantity
			c.logger.Debugf("updating persistent volume definition for volume %s", pv.Name)
			if _, err := c.KubeClient.PersistentVolumes().Update(pv); err != nil {
				return fmt.Errorf("could not update persistent volume: %s", err)
			}
			c.logger.Debugf("successfully updated persistent volume %s", pv.Name)
		}
	}
	if len(pvs) > 0 && totalCompatible == 0 {
		return fmt.Errorf("could not resize EBS volumes: persistent volumes are not compatible with existing resizing providers")
	}
	return nil
}

func (c *Cluster) VolumesNeedResizing(newVolume spec.Volume) (bool, error) {
	volumes, manifestSize, err := c.listVolumesWithManifestSize(newVolume)
	if err != nil {
		return false, err
	}
	for _, pv := range volumes {
		currentSize := quantityToGigabyte(pv.Spec.Capacity[v1.ResourceStorage])
		if currentSize != manifestSize {
			return true, nil
		}
	}
	return false, nil
}

func (c *Cluster) listVolumesWithManifestSize(newVolume spec.Volume) ([]*v1.PersistentVolume, int64, error) {
	newSize, err := resource.ParseQuantity(newVolume.Size)
	if err != nil {
		return nil, 0, fmt.Errorf("could not parse volume size from the manifest: %v", err)
	}
	manifestSize := quantityToGigabyte(newSize)
	volumes, err := c.listPersistentVolumes()
	if err != nil {
		return nil, 0, fmt.Errorf("could not list persistent volumes: %v", err)
	}
	return volumes, manifestSize, nil
}

// getPodNameFromPersistentVolume returns a pod name that it extracts from the volume claim ref.
func getPodNameFromPersistentVolume(pv *v1.PersistentVolume) *spec.NamespacedName {
	namespace := pv.Spec.ClaimRef.Namespace
	name := pv.Spec.ClaimRef.Name[len(constants.DataVolumeName)+1:]
	return &spec.NamespacedName{namespace, name}
}

func quantityToGigabyte(q resource.Quantity) int64 {
	return q.ScaledValue(0) / (1 * constants.Gigabyte)
}
