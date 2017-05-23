package cluster

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/util"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
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
	ns := c.Metadata.Namespace
	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return err
	}
	for _, pvc := range pvcs {
		c.logger.Debugf("Deleting PVC '%s'", util.NameFromMeta(pvc.ObjectMeta))
		if err := c.KubeClient.PersistentVolumeClaims(ns).Delete(pvc.Name, c.deleteOptions); err != nil {
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

// ListEC2VolumeIDs returns all EBS volume IDs belong to this cluster
func (c *Cluster) listPersistentVolumes() ([]*v1.PersistentVolume, error) {
	result := make([]*v1.PersistentVolume, 0)

	pvcs, err := c.listPersistentVolumeClaims()
	if err != nil {
		return nil, fmt.Errorf("could not list cluster's PersistentVolumeClaims: %v", err)
	}
	for _, pvc := range pvcs {
		if pvc.Annotations[constants.VolumeClaimStorageProvisionerAnnotation] != constants.EBSProvisioner {
			continue
		}
		pv, err := c.KubeClient.PersistentVolumes().Get(pvc.Spec.VolumeName)
		if err != nil {
			return nil, fmt.Errorf("could not get PersistentVolume: %v", err)
		}
		if pv.Annotations[constants.VolumeStorateProvisionerAnnotation] != constants.EBSProvisioner {
			return nil, fmt.Errorf("mismatched PersistentVolimeClaim and PersistentVolume provisioner annotations for the volume %s", pv.Name)
		}
		result = append(result, pv)
	}

	return result, nil
}
