package k8sutil

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	apierrors "k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
)

func RestConfig(kubeConfig string, outOfCluster bool) (config *rest.Config, err error) {
	if outOfCluster {
		/* out-of-cluster process */
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		overrides := &clientcmd.ConfigOverrides{}
		rules.ExplicitPath = kubeConfig
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	} else {
		/* in-cluster pod */
		config, err = rest.InClusterConfig()
	}

	return
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
	c.APIPath = "/apis"
	c.GroupVersion = &unversioned.GroupVersion{
		Group:   constants.TPRVendor,
		Version: constants.TPRApiVersion,
	}
	c.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				*c.GroupVersion,
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
