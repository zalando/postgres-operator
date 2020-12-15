package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/filesystems"
)

func (c *Cluster) listPersistentVolumeClaims() ([]v1.PersistentVolumeClaim, error) {
	ns := c.Namespace
	listOptions := metav1.ListOptions{
		LabelSelector: c.labelsSet(false).String(),
	}

	pvcs, err := c.KubeClient.PersistentVolumeClaims(ns).List(context.TODO(), listOptions)
	if err != nil {
		return nil, fmt.Errorf("could not list of PersistentVolumeClaims: %v", err)
	}
	return pvcs.Items, nil
}

func (c *Cluster) deletePersistentVolumeClaims() error {
	c.logger.Debugln("deleting PVCs")
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	for _, pvc := range pvcs {
		c.logger.Debugf("deleting PVC %q", util.NameFromMeta(pvc.ObjectMeta))
		if err := c.KubeClient.PersistentVolumeClaims(pvc.Namespace).Delete(context.TODO(), pvc.Name, c.deleteOptions); err != nil {
			c.logger.Warningf("could not delete PersistentVolumeClaim: %v", err)
		}
	}
	if len(pvcs) > 0 {
		c.logger.Debugln("PVCs have been deleted")
	} else {
		c.logger.Debugln("no PVCs to delete")
	}

	return nil
}

func (c *Cluster) resizeVolumeClaims(newVolume acidv1.Volume) error {
	c.logger.Debugln("resizing PVCs")
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	newQuantity, err := resource.ParseQuantity(newVolume.Size)
	if err != nil {
		return fmt.Errorf("could not parse volume size: %v", err)
	}
	newSize := quantityToGigabyte(newQuantity)
	for _, pvc := range pvcs {
		volumeSize := quantityToGigabyte(pvc.Spec.Resources.Requests[v1.ResourceStorage])
		if volumeSize >= newSize {
			if volumeSize > newSize {
				c.logger.Warningf("cannot shrink persistent volume")
			}
			continue
		}
		pvc.Spec.Resources.Requests[v1.ResourceStorage] = newQuantity
		c.logger.Debugf("updating persistent volume claim definition for volume %q", pvc.Name)
		if _, err := c.KubeClient.PersistentVolumeClaims(pvc.Namespace).Update(context.TODO(), &pvc, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("could not update persistent volume claim: %q", err)
		}
		c.logger.Debugf("successfully updated persistent volume claim %q", pvc.Name)
	}
	return nil
}

func (c *Cluster) listPersistentVolumes() ([]*v1.PersistentVolume, error) {
	result := make([]*v1.PersistentVolume, 0)

	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return nil, fmt.Errorf("could not list cluster's PersistentVolumeClaims: %v", err)
	}

	pods, err := c.listPods()
	if err != nil {
		return nil, fmt.Errorf("could not get list of running pods for resizing persistent volumes: %v", err)
	}

	lastPodIndex := len(pods) - 1

	for _, pvc := range pvcs {
		lastDash := strings.LastIndex(pvc.Name, "-")
		if lastDash > 0 && lastDash < len(pvc.Name)-1 {
			pvcNumber, err := strconv.Atoi(pvc.Name[lastDash+1:])
			if err != nil {
				return nil, fmt.Errorf("could not convert last part of the persistent volume claim name %q to a number", pvc.Name)
			}
			if pvcNumber > lastPodIndex {
				c.logger.Debugf("skipping persistent volume %q corresponding to a non-running pods", pvc.Name)
				continue
			}
		}
		pv, err := c.KubeClient.PersistentVolumes().Get(context.TODO(), pvc.Spec.VolumeName, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("could not get PersistentVolume: %v", err)
		}
		result = append(result, pv)
	}

	return result, nil
}

// resizeVolumes resize persistent volumes compatible with the given resizer interface
func (c *Cluster) resizeVolumes() error {
	if c.VolumeResizer == nil {
		return fmt.Errorf("no volume resizer set for EBS volume handling")
	}

	c.setProcessName("resizing EBS volumes")

	resizer := c.VolumeResizer
	var totalIncompatible int

	newQuantity, err := resource.ParseQuantity(c.Spec.Volume.Size)
	if err != nil {
		return fmt.Errorf("could not parse volume size: %v", err)
	}

	pvs, newSize, err := c.listVolumesWithManifestSize(c.Spec.Volume)
	if err != nil {
		return fmt.Errorf("could not list persistent volumes: %v", err)
	}

	for _, pv := range pvs {
		volumeSize := quantityToGigabyte(pv.Spec.Capacity[v1.ResourceStorage])
		if volumeSize >= newSize {
			if volumeSize > newSize {
				c.logger.Warningf("cannot shrink persistent volume")
			}
			continue
		}
		compatible := false

		if !resizer.VolumeBelongsToProvider(pv) {
			continue
		}
		compatible = true
		if !resizer.IsConnectedToProvider() {
			err := resizer.ConnectToProvider()
			if err != nil {
				return fmt.Errorf("could not connect to the volume provider: %v", err)
			}
			defer func() {
				if err := resizer.DisconnectFromProvider(); err != nil {
					c.logger.Errorf("%v", err)
				}
			}()
		}
		awsVolumeID, err := resizer.GetProviderVolumeID(pv)
		if err != nil {
			return err
		}
		c.logger.Debugf("updating persistent volume %q to %d", pv.Name, newSize)
		if err := resizer.ResizeVolume(awsVolumeID, newSize); err != nil {
			return fmt.Errorf("could not resize EBS volume %q: %v", awsVolumeID, err)
		}
		c.logger.Debugf("resizing the filesystem on the volume %q", pv.Name)
		podName := getPodNameFromPersistentVolume(pv)
		if err := c.resizePostgresFilesystem(podName, []filesystems.FilesystemResizer{&filesystems.Ext234Resize{}}); err != nil {
			return fmt.Errorf("could not resize the filesystem on pod %q: %v", podName, err)
		}
		c.logger.Debugf("filesystem resize successful on volume %q", pv.Name)
		pv.Spec.Capacity[v1.ResourceStorage] = newQuantity
		c.logger.Debugf("updating persistent volume definition for volume %q", pv.Name)
		if _, err := c.KubeClient.PersistentVolumes().Update(context.TODO(), pv, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("could not update persistent volume: %q", err)
		}
		c.logger.Debugf("successfully updated persistent volume %q", pv.Name)

		if !compatible {
			c.logger.Warningf("volume %q is incompatible with all available resizing providers, consider switching storage_resize_mode to pvc or off", pv.Name)
			totalIncompatible++
		}
	}
	if totalIncompatible > 0 {
		return fmt.Errorf("could not resize EBS volumes: some persistent volumes are not compatible with existing resizing providers")
	}
	return nil
}

func (c *Cluster) volumeClaimsNeedResizing(newVolume acidv1.Volume) (bool, error) {
	newSize, err := resource.ParseQuantity(newVolume.Size)
	manifestSize := quantityToGigabyte(newSize)
	if err != nil {
		return false, fmt.Errorf("could not parse volume size from the manifest: %v", err)
	}
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return false, fmt.Errorf("could not receive persistent volume claims: %v", err)
	}
	for _, pvc := range pvcs {
		currentSize := quantityToGigabyte(pvc.Spec.Resources.Requests[v1.ResourceStorage])
		if currentSize != manifestSize {
			return true, nil
		}
	}
	return false, nil
}

func (c *Cluster) volumesNeedResizing(newVolume acidv1.Volume) (bool, error) {
	vols, manifestSize, err := c.listVolumesWithManifestSize(newVolume)
	if err != nil {
		return false, err
	}
	for _, pv := range vols {
		currentSize := quantityToGigabyte(pv.Spec.Capacity[v1.ResourceStorage])
		if currentSize != manifestSize {
			return true, nil
		}
	}
	return false, nil
}

func (c *Cluster) listVolumesWithManifestSize(newVolume acidv1.Volume) ([]*v1.PersistentVolume, int64, error) {
	newSize, err := resource.ParseQuantity(newVolume.Size)
	if err != nil {
		return nil, 0, fmt.Errorf("could not parse volume size from the manifest: %v", err)
	}
	manifestSize := quantityToGigabyte(newSize)
	vols, err := c.listPersistentVolumes()
	if err != nil {
		return nil, 0, fmt.Errorf("could not list persistent volumes: %v", err)
	}
	return vols, manifestSize, nil
}

// getPodNameFromPersistentVolume returns a pod name that it extracts from the volume claim ref.
func getPodNameFromPersistentVolume(pv *v1.PersistentVolume) *spec.NamespacedName {
	namespace := pv.Spec.ClaimRef.Namespace
	name := pv.Spec.ClaimRef.Name[len(constants.DataVolumeName)+1:]
	return &spec.NamespacedName{Namespace: namespace, Name: name}
}

func quantityToGigabyte(q resource.Quantity) int64 {
	return q.ScaledValue(0) / (1 * constants.Gigabyte)
}

func (c *Cluster) executeEBSMigration() error {
	if !c.OpConfig.EnableEBSGp3Migration {
		return nil
	}
	c.logger.Infof("starting EBS gp2 to gp3 migration")

	pvs, _, err := c.listVolumesWithManifestSize(c.Spec.Volume)
	if err != nil {
		return fmt.Errorf("could not list persistent volumes: %v", err)
	}
	c.logger.Debugf("found %d volumes, size of known volumes %d", len(pvs), len(c.EBSVolumes))

	volumeIds := []string{}
	var volumeID string
	for _, pv := range pvs {
		volumeID, err = c.VolumeResizer.ExtractVolumeID(pv.Spec.AWSElasticBlockStore.VolumeID)
		if err != nil {
			continue
		}

		volumeIds = append(volumeIds, volumeID)
	}

	if len(volumeIds) == len(c.EBSVolumes) {
		hasGp2 := false
		for _, v := range c.EBSVolumes {
			if v.VolumeType == "gp2" {
				hasGp2 = true
			}
		}

		if !hasGp2 {
			c.logger.Infof("no EBS gp2 volumes left to migrate")
			return nil
		}
	}

	awsVolumes, err := c.VolumeResizer.DescribeVolumes(volumeIds)
	if nil != err {
		return err
	}

	for _, volume := range awsVolumes {
		if volume.VolumeType == "gp2" && volume.Size < c.OpConfig.EnableEBSGp3MigrationMaxSize {
			c.logger.Infof("modifying EBS volume %s to type gp3 migration (%d)", volume.VolumeID, volume.Size)
			err = c.VolumeResizer.ModifyVolume(volume.VolumeID, "gp3", volume.Size, 3000, 125)
			if nil != err {
				c.logger.Warningf("modifying volume %s failed: %v", volume.VolumeID, err)
			}
		} else {
			c.logger.Debugf("skipping EBS volume %s to type gp3 migration (%d)", volume.VolumeID, volume.Size)
		}
		c.EBSVolumes[volume.VolumeID] = volume
	}

	return nil
}
