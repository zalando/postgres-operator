package volumes

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"k8s.io/api/core/v1"

	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/retryutil"
)

// EBSVolumeResizer implements volume resizing interface for AWS EBS volumes.
type EBSVolumeResizer struct {
	connection *ec2.EC2
	AWSRegion  string
}

// ConnectToProvider connects to AWS.
func (c *EBSVolumeResizer) ConnectToProvider() error {
	sess, err := session.NewSession(&aws.Config{Region: aws.String(c.AWSRegion)})
	if err != nil {
		return fmt.Errorf("could not establish AWS session: %v", err)
	}
	c.connection = ec2.New(sess)
	return nil
}

// IsConnectedToProvider checks if AWS connection is established.
func (c *EBSVolumeResizer) IsConnectedToProvider() bool {
	return c.connection != nil
}

// VolumeBelongsToProvider checks if the given persistent volume is backed by EBS.
func (c *EBSVolumeResizer) VolumeBelongsToProvider(pv *v1.PersistentVolume) bool {
	return pv.Spec.AWSElasticBlockStore != nil && pv.Annotations[constants.VolumeStorateProvisionerAnnotation] == constants.EBSProvisioner
}

// GetProviderVolumeID converts aws://eu-central-1b/vol-00f93d4827217c629 to vol-00f93d4827217c629 for EBS volumes
func (c *EBSVolumeResizer) GetProviderVolumeID(pv *v1.PersistentVolume) (string, error) {
	volumeID := pv.Spec.AWSElasticBlockStore.VolumeID
	if volumeID == "" {
		return "", fmt.Errorf("volume id is empty for volume %q", pv.Name)
	}
	idx := strings.LastIndex(volumeID, constants.EBSVolumeIDStart) + 1
	if idx == 0 {
		return "", fmt.Errorf("malfored EBS volume id %q", volumeID)
	}
	return volumeID[idx:], nil
}

// ResizeVolume actually calls AWS API to resize the EBS volume if necessary.
func (c *EBSVolumeResizer) ResizeVolume(volumeID string, newSize int64) error {
	/* first check if the volume is already of a requested size */
	volumeOutput, err := c.connection.DescribeVolumes(&ec2.DescribeVolumesInput{VolumeIds: []*string{&volumeID}})
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
	output, err := c.connection.ModifyVolume(&input)
	if err != nil {
		return fmt.Errorf("could not modify persistent volume: %v", err)
	}

	state := *output.VolumeModification.ModificationState
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
	in := ec2.DescribeVolumesModificationsInput{VolumeIds: []*string{&volumeID}}
	return retryutil.Retry(constants.EBSVolumeResizeWaitInterval, constants.EBSVolumeResizeWaitTimeout,
		func() (bool, error) {
			out, err := c.connection.DescribeVolumesModifications(&in)
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
			return *out.VolumesModifications[0].ModificationState != constants.EBSVolumeStateModifying, nil
		})
}

// DisconnectFromProvider closes connection to the EC2 instance
func (c *EBSVolumeResizer) DisconnectFromProvider() error {
	c.connection = nil
	return nil
}
