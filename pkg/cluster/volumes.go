package cluster

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/filesystems"
	"github.com/zalando/postgres-operator/pkg/util/volumes"
)

func (c *Cluster) syncVolumes() error {
	c.logger.Debugf("syncing volumes using %q storage resize mode", c.OpConfig.StorageResizeMode)
	var err error

	// check quantity string once, and do not bother with it anymore anywhere else
	_, err = resource.ParseQuantity(c.Spec.Volume.Size)
	if err != nil {
		return fmt.Errorf("could not parse volume size from the manifest: %v", err)
	}

	if c.OpConfig.StorageResizeMode == "mixed" {
		// mixed op uses AWS API to adjust size, throughput, iops, and calls pvc change for file system resize
		// in case of errors we proceed to let K8s do its work, favoring disk space increase of other adjustments

		err = c.populateVolumeMetaData()
		if err != nil {
			c.logger.Errorf("populating EBS meta data failed, skipping potential adjustements: %v", err)
		} else {
			err = c.syncUnderlyingEBSVolume()
			if err != nil {
				c.logger.Errorf("errors occured during EBS volume adjustments: %v", err)
			}
		}

		// resize pvc to adjust filesystem size until better K8s support
		if err = c.syncVolumeClaims(); err != nil {
			err = fmt.Errorf("could not sync persistent volume claims: %v", err)
			return err
		}
	} else if c.OpConfig.StorageResizeMode == "pvc" {
		if err = c.syncVolumeClaims(); err != nil {
			err = fmt.Errorf("could not sync persistent volume claims: %v", err)
			return err
		}
	} else if c.OpConfig.StorageResizeMode == "ebs" {
		// potentially enlarge volumes before changing the statefulset. By doing that
		// in this order we make sure the operator is not stuck waiting for a pod that
		// cannot start because it ran out of disk space.
		// TODO: handle the case of the cluster that is downsized and enlarged again
		// (there will be a volume from the old pod for which we can't act before the
		//  the statefulset modification is concluded)
		if err = c.syncEbsVolumes(); err != nil {
			err = fmt.Errorf("could not sync persistent volumes: %v", err)
			return err
		}
	} else {
		c.logger.Infof("Storage resize is disabled (storage_resize_mode is off). Skipping volume sync.")
	}

	return nil
}

func (c *Cluster) syncUnderlyingEBSVolume() error {
	c.logger.Infof("starting to sync EBS volumes: type, iops, throughput, and size")

	var err error

	targetValue := c.Spec.Volume
	newSize, err := resource.ParseQuantity(targetValue.Size)
	targetSize := quantityToGigabyte(newSize)

	awsGp3 := aws.String("gp3")
	awsIo2 := aws.String("io2")

	errors := []string{}

	for _, volume := range c.EBSVolumes {
		var modifyIops *int64
		var modifyThroughput *int64
		var modifySize *int64
		var modifyType *string

		if targetValue.Iops != nil {
			if volume.Iops != *targetValue.Iops {
				modifyIops = targetValue.Iops
			}
		}

		if targetValue.Throughput != nil {
			if volume.Throughput != *targetValue.Throughput {
				modifyThroughput = targetValue.Throughput
			}
		}

		if targetSize > volume.Size {
			modifySize = &targetSize
		}

		if modifyIops != nil || modifyThroughput != nil || modifySize != nil {
			if modifyIops != nil || modifyThroughput != nil {
				// we default to gp3 if iops and throughput are configured
				modifyType = awsGp3
				if targetValue.VolumeType == "io2" {
					modifyType = awsIo2
				}
			} else if targetValue.VolumeType == "gp3" && volume.VolumeType != "gp3" {
				modifyType = awsGp3
			} else {
				// do not touch type
				modifyType = nil
			}

			err = c.VolumeResizer.ModifyVolume(volume.VolumeID, modifyType, modifySize, modifyIops, modifyThroughput)
			if err != nil {
				errors = append(errors, fmt.Sprintf("modify volume failed: volume=%s size=%d iops=%d throughput=%d", volume.VolumeID, volume.Size, volume.Iops, volume.Throughput))
			}
		}
	}

	if len(errors) > 0 {
		for _, s := range errors {
			c.logger.Warningf(s)
		}
		// c.logger.Errorf("failed to modify %d of %d volumes", len(c.EBSVolumes), len(errors))
	}
	return nil
}

func (c *Cluster) populateVolumeMetaData() error {
	c.logger.Infof("starting reading ebs meta data")

	pvs, err := c.listPersistentVolumes()
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

	currentVolumes, err := c.VolumeResizer.DescribeVolumes(volumeIds)
	if nil != err {
		return err
	}

	if len(currentVolumes) != len(c.EBSVolumes) {
		c.logger.Debugf("number of ebs volumes (%d) discovered differs from already known volumes (%d)", len(currentVolumes), len(c.EBSVolumes))
	}

	// reset map, operator is not responsible for dangling ebs volumes
	c.EBSVolumes = make(map[string]volumes.VolumeProperties)
	for _, volume := range currentVolumes {
		c.EBSVolumes[volume.VolumeID] = volume
	}

	return nil
}

// syncVolumeClaims reads all persistent volume claims and checks that their size matches the one declared in the statefulset.
func (c *Cluster) syncVolumeClaims() error {
	c.setProcessName("syncing volume claims")

	needsResizing, err := c.volumeClaimsNeedResizing(c.Spec.Volume)
	if err != nil {
		return fmt.Errorf("could not compare size of the volume claims: %v", err)
	}

	if !needsResizing {
		c.logger.Infof("volume claims do not require changes")
		return nil
	}

	if err := c.resizeVolumeClaims(c.Spec.Volume); err != nil {
		return fmt.Errorf("could not sync volume claims: %v", err)
	}

	c.logger.Infof("volume claims have been synced successfully")

	return nil
}

// syncVolumes reads all persistent volumes and checks that their size matches the one declared in the statefulset.
func (c *Cluster) syncEbsVolumes() error {
	c.setProcessName("syncing EBS and Claims volumes")

	act, err := c.volumesNeedResizing()
	if err != nil {
		return fmt.Errorf("could not compare size of the volumes: %v", err)
	}
	if !act {
		return nil
	}

	if err := c.resizeVolumes(); err != nil {
		return fmt.Errorf("could not sync volumes: %v", err)
	}

	c.logger.Infof("volumes have been synced successfully")

	return nil
}

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

	newQuantity, err := resource.ParseQuantity(c.Spec.Volume.Size)
	if err != nil {
		return fmt.Errorf("could not parse volume size: %v", err)
	}

	newSize := quantityToGigabyte(newQuantity)
	resizer := c.VolumeResizer
	var totalIncompatible int

	pvs, err := c.listPersistentVolumes()
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

func (c *Cluster) volumesNeedResizing() (bool, error) {
	newQuantity, _ := resource.ParseQuantity(c.Spec.Volume.Size)
	newSize := quantityToGigabyte(newQuantity)

	vols, err := c.listPersistentVolumes()
	if err != nil {
		return false, err
	}
	for _, pv := range vols {
		currentSize := quantityToGigabyte(pv.Spec.Capacity[v1.ResourceStorage])
		if currentSize != newSize {
			return true, nil
		}
	}
	return false, nil
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

	pvs, err := c.listPersistentVolumes()
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

	var i3000 int64 = 3000
	var i125 int64 = 125

	for _, volume := range awsVolumes {
		if volume.VolumeType == "gp2" && volume.Size < c.OpConfig.EnableEBSGp3MigrationMaxSize {
			c.logger.Infof("modifying EBS volume %s to type gp3 migration (%d)", volume.VolumeID, volume.Size)
			err = c.VolumeResizer.ModifyVolume(volume.VolumeID, aws.String("gp3"), &volume.Size, &i3000, &i125)
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
