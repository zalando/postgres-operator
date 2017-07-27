package k8sutil

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	v1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	extensions "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
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
	RESTClient rest.Interface
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
	c.RESTClient = src.CoreV1().RESTClient()
	return
}

func RestConfig(kubeConfig string, outOfCluster bool) (*rest.Config, error) {
	if outOfCluster {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}

	return rest.InClusterConfig()
}

func ClientSet(config *rest.Config) (client *kubernetes.Clientset, err error) {
	return kubernetes.NewForConfig(config)
}

func ResourceAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func ResourceNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

func KubernetesRestClient(cfg rest.Config) (rest.Interface, error) {
	cfg.GroupVersion = &schema.GroupVersion{
		Group:   constants.TPRGroup,
		Version: constants.TPRApiVersion,
	}
	cfg.APIPath = constants.K8sAPIPath
	cfg.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	return rest.RESTClientFor(&cfg)
}

func WaitTPRReady(restclient rest.Interface, interval, timeout time.Duration, ns string) error {
	return retryutil.Retry(interval, timeout, func() (bool, error) {
		_, err := restclient.
			Get().
			Namespace(ns).
			Resource(constants.ResourceName).
			DoRaw()
		if err != nil {
			if ResourceNotFound(err) { // not set up yet. wait more.
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}
