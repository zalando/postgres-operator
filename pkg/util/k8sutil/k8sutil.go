package k8sutil

import (
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	apierrors "k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
	"github.com/zalando-incubator/postgres-operator/pkg/util/retryutil"
)

func RestConfig(kubeConfig string, outOfCluster bool) (*rest.Config, error) {
	if outOfCluster {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}
	return rest.InClusterConfig()
}

func KubernetesClient(config *rest.Config) (client *kubernetes.Clientset, err error) {
	return kubernetes.NewForConfig(config)
}

func ResourceAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func ResourceNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

func KubernetesRestClient(c *rest.Config) (*rest.RESTClient, error) {
	c.GroupVersion = &unversioned.GroupVersion{Version: constants.K8sVersion}
	c.APIPath = constants.K8sApiPath
	c.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				unversioned.GroupVersion{
					Group:   constants.TPRVendor,
					Version: constants.TPRApiVersion,
				},
				&spec.Postgresql{},
				&spec.PostgresqlList{},
				&api.ListOptions{},
				&api.DeleteOptions{},
			)
			return nil
		})
	schemeBuilder.AddToScheme(api.Scheme)

	return rest.RESTClientFor(c)
}

func WaitTPRReady(restclient rest.Interface, interval, timeout time.Duration, ns string) error {
	return retryutil.Retry(interval, timeout, func() (bool, error) {
		_, err := restclient.Get().RequestURI(fmt.Sprintf(constants.ListClustersURITemplate, ns)).DoRaw()
		if err != nil {
			if ResourceNotFound(err) { // not set up yet. wait more.
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}
