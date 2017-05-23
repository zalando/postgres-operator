package volumes

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"

	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

const (
	AWS_REGION = "eu-central-1"
)

func ConnectToEC2() (*ec2.EC2, error) {
	sess, err := session.NewSession(&aws.Config{Region: aws.String(AWS_REGION)})
	if err != nil {
		return nil, fmt.Errorf("could not establish AWS session: %v", err)
	}
	return ec2.New(sess), nil
}

func ResizeVolume(svc *ec2.EC2, volumeId string, newSize int64) error {
	input := ec2.ModifyVolumeInput{Size: &newSize, VolumeId: &volumeId}
	output, err := svc.ModifyVolume(&input)
	if err != nil {
		return fmt.Errorf("could not modify persistent volume: %v", err)
	}

	state := *output.VolumeModification.ModificationState
	if state == constants.EBSVolumeStateFailed {
		return fmt.Errorf("could not modify persistent volume %s: modification state failed", volumeId)
	}
	if state == "" {
		return fmt.Errorf("received empty modification status")
	}
	if state == constants.EBSVolumeStateOptimizing || state == constants.EBSVolumeStateCompleted {
		return nil
	}
	// wait until the volume reaches the "optimizing" or "completed" state
	in := ec2.DescribeVolumesModificationsInput{VolumeIds: []*string{&volumeId}}
	return retryutil.Retry(constants.EBSVolumeResizeWaitInterval, constants.EBSVolumeResizeWaitTimeout,
		func() (bool, error) {
			out, err := svc.DescribeVolumesModifications(&in)
			if err != nil {
				return false, fmt.Errorf("could not describe volume modification: %v", err)
			}
			if len(out.VolumesModifications) != 1 {
				return false, fmt.Errorf("describe volume modification didn't return one record for volume \"%s\"", volumeId)
			}
			if *out.VolumesModifications[0].VolumeId != volumeId {
				return false, fmt.Errorf("non-matching volume id when describing modifications: \"%s\" is different from \"%s\"")
			}
			return *out.VolumesModifications[0].ModificationState != constants.EBSVolumeStateModifying, nil
		})
}
