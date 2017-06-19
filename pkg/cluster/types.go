package cluster

import (
	"k8s.io/client-go/kubernetes"
	v1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	extensions "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
)

type PostgresRole string

const (
	Master  PostgresRole = "master"
	Replica PostgresRole = "replica"
)

type KubernetesClient struct {
	v1core.SecretsGetter
	v1core.ServicesGetter
	v1core.EndpointsGetter
	v1core.PodsGetter
	v1core.PersistentVolumesGetter
	v1core.PersistentVolumeClaimsGetter
	v1core.ConfigMapsGetter
	v1beta1.StatefulSetsGetter
	extensions.ThirdPartyResourcesGetter
}

func NewFromKubernetesInterface(src kubernetes.Interface) (c KubernetesClient) {
	c = KubernetesClient{}
	c.PodsGetter = src.CoreV1()
	c.ServicesGetter = src.CoreV1()
	c.EndpointsGetter = src.CoreV1()
	c.SecretsGetter = src.CoreV1()
	c.ConfigMapsGetter = src.CoreV1()
	c.PersistentVolumeClaimsGetter = src.CoreV1()
	c.PersistentVolumesGetter = src.CoreV1()
	c.StatefulSetsGetter = src.AppsV1beta1()
	c.ThirdPartyResourcesGetter = src.ExtensionsV1beta1()
	return
}
