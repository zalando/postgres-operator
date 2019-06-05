package volumes

import (
	"k8s.io/api/core/v1"
)

// VolumeResizer defines the set of methods used to implememnt provider-specific resizing of persistent volumes.
type VolumeResizer interface {
	ConnectToProvider() error
	IsConnectedToProvider() bool
	VolumeBelongsToProvider(pv *v1.PersistentVolume) bool
	GetProviderVolumeID(pv *v1.PersistentVolume) (string, error)
	ResizeVolume(providerVolumeID string, newSize int64) error
	DisconnectFromProvider() error
}
