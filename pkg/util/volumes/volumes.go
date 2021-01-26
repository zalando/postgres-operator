package volumes

//go:generate mockgen -package mocks -destination=$PWD/mocks/$GOFILE -source=$GOFILE -build_flags=-mod=vendor

import v1 "k8s.io/api/core/v1"

// VolumeProperties ...
type VolumeProperties struct {
	VolumeID   string
	VolumeType string
	Size       int64
	Iops       int64
	Throughput int64
}

// VolumeResizer defines the set of methods used to implememnt provider-specific resizing of persistent volumes.
type VolumeResizer interface {
	ConnectToProvider() error
	IsConnectedToProvider() bool
	VolumeBelongsToProvider(pv *v1.PersistentVolume) bool
	GetProviderVolumeID(pv *v1.PersistentVolume) (string, error)
	ExtractVolumeID(volumeID string) (string, error)
	ResizeVolume(providerVolumeID string, newSize int64) error
	ModifyVolume(providerVolumeID string, newType *string, newSize *int64, iops *int64, throughput *int64) error
	DisconnectFromProvider() error
	DescribeVolumes(providerVolumesID []string) ([]VolumeProperties, error)
}
