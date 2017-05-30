package constants

import "time"

const (
	AWS_REGION       = "eu-central-1"
	EBSVolumeIDStart = "/vol-"
	EBSProvisioner   = "kubernetes.io/aws-ebs"
	//https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_VolumeModification.html
	EBSVolumeStateModifying     = "modifying"
	EBSVolumeStateOptimizing    = "optimizing"
	EBSVolumeStateFailed        = "failed"
	EBSVolumeStateCompleted     = "completed"
	EBSVolumeResizeWaitInterval = 2 * time.Second
	EBSVolumeResizeWaitTimeout  = 30 * time.Second
)
