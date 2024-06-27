package volumes

import (
	"fmt"
	"testing"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetProviderVolumeID(t *testing.T) {
	tests := []struct {
		name     string
		pv       *v1.PersistentVolume
		expected string
		err      error
	}{
		{
			name: "CSI volume handle",
			pv: &v1.PersistentVolume{
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						CSI: &v1.CSIPersistentVolumeSource{
							VolumeHandle: "vol-075ddfc4a127d0bd5",
						},
					},
				},
			},
			expected: "vol-075ddfc4a127d0bd5",
			err:      nil,
		},
		{
			name: "AWS EBS volume handle",
			pv: &v1.PersistentVolume{
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						AWSElasticBlockStore: &v1.AWSElasticBlockStoreVolumeSource{
							VolumeID: "aws://eu-central-1a/vol-075ddfc4a127d0bd4",
						},
					},
				},
			},
			expected: "vol-075ddfc4a127d0bd4",
			err:      nil,
		},
		{
			name: "Empty volume handle",
			pv: &v1.PersistentVolume{
				Spec: v1.PersistentVolumeSpec{},
			},
			expected: "",
			err:      fmt.Errorf("got empty volume id for volume %v", &v1.PersistentVolume{}),
		},
	}

	resizer := EBSVolumeResizer{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volumeID, err := resizer.GetProviderVolumeID(tt.pv)
			if volumeID != tt.expected || (err != nil && err.Error() != tt.err.Error()) {
				t.Errorf("expected %v, got %v, expected err %v, got %v", tt.expected, volumeID, tt.err, err)
			}
		})
	}
}

func TestVolumeBelongsToProvider(t *testing.T) {
	tests := []struct {
		name     string
		pv       *v1.PersistentVolume
		expected bool
	}{
		{
			name: "CSI volume handle",
			pv: &v1.PersistentVolume{
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						CSI: &v1.CSIPersistentVolumeSource{
							Driver:       "ebs.csi.aws.com",
							VolumeHandle: "vol-075ddfc4a127d0bd5",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "AWS EBS volume handle",
			pv: &v1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string {
						"pv.kubernetes.io/provisioned-by": "kubernetes.io/aws-ebs",
					},
				},
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						AWSElasticBlockStore: &v1.AWSElasticBlockStoreVolumeSource{
							VolumeID: "aws://eu-central-1a/vol-075ddfc4a127d0bd4",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Empty volume source",
			pv: &v1.PersistentVolume{
				Spec: v1.PersistentVolumeSpec{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resizer := EBSVolumeResizer{}
			isProvider := resizer.VolumeBelongsToProvider(tt.pv)
			if isProvider != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, isProvider)
			}
		})
	}
}
