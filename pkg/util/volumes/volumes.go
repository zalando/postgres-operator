package volumes

import (
	"k8s.io/client-go/pkg/api/v1"
)

type VolumeResizer interface {
	ConnectToProvider() error
	IsConnectedToProvider() bool
	VolumeBelongsToProvider(pv *v1.PersistentVolume) bool
	GetProviderVolumeID(pv *v1.PersistentVolume) (string, error)
	ResizeVolume(providerVolumeId string, newSize int64) error
	DisconnectFromProvider() error
}
