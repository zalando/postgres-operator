package volumes

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	v1 "k8s.io/api/core/v1"

	"github.com/zalando/postgres-operator/v2/pkg/util/constants"
	"github.com/zalando/postgres-operator/v2/pkg/util/retryutil"
)

// EBSVolumeResizer implements volume resizing interface for AWS EBS volumes.
type EBSVolumeResizer struct {
	connection *ec2.Client
	AWSRegion  string
}

// ConnectToProvider connects to AWS.
func (r *EBSVolumeResizer) ConnectToProvider() error {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(r.AWSRegion))
	if err != nil {
		return fmt.Errorf("could not establish AWS session: %v", err)
	}
	r.connection = ec2.NewFromConfig(cfg)
	return nil
}

// IsConnectedToProvider checks if AWS connection is established.
func (r *EBSVolumeResizer) IsConnectedToProvider() bool {
	return r.connection != nil
}

// VolumeBelongsToProvider checks if the given persistent volume is backed by EBS.
func (r *EBSVolumeResizer) VolumeBelongsToProvider(pv *v1.PersistentVolume) bool {
	return (pv.Spec.AWSElasticBlockStore != nil && pv.Annotations[constants.VolumeStorateProvisionerAnnotation] == constants.EBSProvisioner) ||
		(pv.Spec.CSI != nil && pv.Spec.CSI.Driver == constants.EBSDriver)
}

// ExtractVolumeID extracts volumeID from "aws://eu-central-1a/vol-075ddfc4a127d0bd4"
// or return only the vol-075ddfc4a127d0bd4 when it doesn't have "aws://"
func (r *EBSVolumeResizer) ExtractVolumeID(volumeID string) (string, error) {
	if (strings.HasPrefix(volumeID, "vol-")) && !(strings.HasPrefix(volumeID, "aws://")) {
		return volumeID, nil
	}
	idx := strings.LastIndex(volumeID, constants.EBSVolumeIDStart) + 1
	if idx == 0 {
		return "", fmt.Errorf("malformed EBS volume id %q", volumeID)
	}
	return volumeID[idx:], nil
}

// GetProviderVolumeID converts aws://eu-central-1b/vol-00f93d4827217c629 to vol-00f93d4827217c629 for EBS volumes
func (r *EBSVolumeResizer) GetProviderVolumeID(pv *v1.PersistentVolume) (string, error) {
	var volumeID string = ""
	if pv.Spec.CSI != nil {
		volumeID = pv.Spec.CSI.VolumeHandle
	} else if pv.Spec.AWSElasticBlockStore != nil {
		volumeID = pv.Spec.AWSElasticBlockStore.VolumeID
	}
	if volumeID == "" {
		return "", fmt.Errorf("got empty volume id for volume %v", pv)
	}

	return r.ExtractVolumeID(volumeID)
}

// DescribeVolumes ...
func (r *EBSVolumeResizer) DescribeVolumes(volumeIds []string) ([]VolumeProperties, error) {
	if !r.IsConnectedToProvider() {
		err := r.ConnectToProvider()
		if err != nil {
			return nil, err
		}
	}

	volumeOutput, err := r.connection.DescribeVolumes(context.TODO(), &ec2.DescribeVolumesInput{VolumeIds: volumeIds})
	if err != nil {
		return nil, err
	}

	p := []VolumeProperties{}
	if nil == volumeOutput.Volumes {
		return p, nil
	}

	for _, v := range volumeOutput.Volumes {
		vp := VolumeProperties{VolumeID: *v.VolumeId, Size: int32(*v.Size), VolumeType: string(v.VolumeType)}
		if v.Iops != nil {
			vp.Iops = int32(*v.Iops)
		}
		if v.Throughput != nil {
			vp.Throughput = int32(*v.Throughput)
		}
		p = append(p, vp)
	}

	return p, nil
}

// ResizeVolume actually calls AWS API to resize the EBS volume if necessary.
func (r *EBSVolumeResizer) ResizeVolume(volumeID string, newSize int32) error {
	/* first check if the volume is already of a requested size */
	volumeOutput, err := r.connection.DescribeVolumes(context.TODO(), &ec2.DescribeVolumesInput{VolumeIds: []string{volumeID}})
	if err != nil {
		return fmt.Errorf("could not get information about the volume: %v", err)
	}
	vol := volumeOutput.Volumes[0]
	if *vol.VolumeId != volumeID {
		return fmt.Errorf("describe volume %q returned information about a non-matching volume %q", volumeID, *vol.VolumeId)
	}
	if *vol.Size == newSize {
		// nothing to do
		return nil
	}
	input := ec2.ModifyVolumeInput{Size: &newSize, VolumeId: &volumeID}
	output, err := r.connection.ModifyVolume(context.TODO(), &input)
	if err != nil {
		return fmt.Errorf("could not modify persistent volume: %v", err)
	}

	state := output.VolumeModification.ModificationState
	if state == constants.EBSVolumeStateFailed {
		return fmt.Errorf("could not modify persistent volume %q: modification state failed", volumeID)
	}
	if state == "" {
		return fmt.Errorf("received empty modification status")
	}
	if state == constants.EBSVolumeStateOptimizing || state == constants.EBSVolumeStateCompleted {
		return nil
	}
	// wait until the volume reaches the "optimizing" or "completed" state
	in := ec2.DescribeVolumesModificationsInput{VolumeIds: []string{volumeID}}
	return retryutil.Retry(constants.EBSVolumeResizeWaitInterval, constants.EBSVolumeResizeWaitTimeout,
		func() (bool, error) {
			out, err := r.connection.DescribeVolumesModifications(context.TODO(), &in)
			if err != nil {
				return false, fmt.Errorf("could not describe volume modification: %v", err)
			}
			if len(out.VolumesModifications) != 1 {
				return false, fmt.Errorf("describe volume modification didn't return one record for volume %q", volumeID)
			}
			if *out.VolumesModifications[0].VolumeId != volumeID {
				return false, fmt.Errorf("non-matching volume id when describing modifications: %q is different from %q",
					*out.VolumesModifications[0].VolumeId, volumeID)
			}
			return out.VolumesModifications[0].ModificationState != constants.EBSVolumeStateModifying, nil
		})
}

// ModifyVolume Modify EBS volume
func (r *EBSVolumeResizer) ModifyVolume(volumeID string, newType *string, newSize *int32, iops *int32, throughput *int32) error {
	input := ec2.ModifyVolumeInput{VolumeId: &volumeID}
	if newSize != nil {
		input.Size = newSize
	}
	if iops != nil {
		input.Iops = iops
	}
	if throughput != nil {
		input.Throughput = throughput
	}
	if newType != nil {
		input.VolumeType = types.VolumeType(*newType)
	}

	output, err := r.connection.ModifyVolume(context.TODO(), &input)
	if err != nil {
		return fmt.Errorf("could not modify persistent volume: %v", err)
	}

	state := output.VolumeModification.ModificationState
	if state == constants.EBSVolumeStateFailed {
		return fmt.Errorf("could not modify persistent volume %q: modification state failed", volumeID)
	}
	if state == "" {
		return fmt.Errorf("received empty modification status")
	}
	if state == constants.EBSVolumeStateOptimizing || state == constants.EBSVolumeStateCompleted {
		return nil
	}
	// wait until the volume reaches the "optimizing" or "completed" state
	in := ec2.DescribeVolumesModificationsInput{VolumeIds: []string{volumeID}}
	return retryutil.Retry(constants.EBSVolumeResizeWaitInterval, constants.EBSVolumeResizeWaitTimeout,
		func() (bool, error) {
			out, err := r.connection.DescribeVolumesModifications(context.TODO(), &in)
			if err != nil {
				return false, fmt.Errorf("could not describe volume modification: %v", err)
			}
			if len(out.VolumesModifications) != 1 {
				return false, fmt.Errorf("describe volume modification didn't return one record for volume %q", volumeID)
			}
			if *out.VolumesModifications[0].VolumeId != volumeID {
				return false, fmt.Errorf("non-matching volume id when describing modifications: %q is different from %q",
					*out.VolumesModifications[0].VolumeId, volumeID)
			}
			return out.VolumesModifications[0].ModificationState != constants.EBSVolumeStateModifying, nil
		})
}

// DisconnectFromProvider closes connection to the EC2 instance
func (r *EBSVolumeResizer) DisconnectFromProvider() error {
	r.connection = nil
	return nil
}
