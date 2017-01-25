package controller

import (
	"fmt"
	"log"
	"time"

    "net/http"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
    apierrors "k8s.io/client-go/pkg/api/errors"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

    "github.bus.zalan.do/acid/postgres-operator/pkg/spec"
)

var (
	VENDOR       = "acid.zalan.do"
	VERSION      = "0.0.1.dev"
	resyncPeriod = 5 * time.Minute
)

type Options struct {
    KubeConfig string
}

func KubernetesConfig(options Options) (config *rest.Config, isInCluster bool) {
	var err     error
	isInCluster = (options.KubeConfig == "")
	if !isInCluster {
		/* out-of-cluster process */
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		overrides := &clientcmd.ConfigOverrides{}
		rules.ExplicitPath = options.KubeConfig
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	} else {
		/* in-cluster pod */
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("Couldn't get Kubernetes default config: %s", err)
	}
	return
}

func newKubernetesSpiloClient(c *rest.Config) (*rest.RESTClient, error) {
	c.APIPath = "/apis"
	c.GroupVersion = &unversioned.GroupVersion{
		Group:   VENDOR,
		Version: "v1",
	}
	c.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				*c.GroupVersion,
				&spec.Spilo{},
				&spec.SpiloList{},
				&api.ListOptions{},
				&api.DeleteOptions{},
			)
			return nil
		})
	schemeBuilder.AddToScheme(api.Scheme)

	return rest.RESTClientFor(c)
}

//TODO: Move to separate package
func IsKubernetesResourceNotFoundError(err error) bool {
    se, ok := err.(*apierrors.StatusError)
    if !ok {
        return false
    }
    if se.Status().Code == http.StatusNotFound && se.Status().Reason == unversioned.StatusReasonNotFound {
        return true
    }
    return false
}

func EnsureSpiloThirdPartyResource(client *kubernetes.Clientset) error {
	// The resource doesn't exist, so we create it.
	tpr := v1beta1.ThirdPartyResource{
		ObjectMeta: v1.ObjectMeta{
			Name: fmt.Sprintf("spilo.%s", VENDOR),
		},
		Description: "A specification of Spilo StatefulSets",
		Versions: []v1beta1.APIVersion{
			{Name: "v1"},
		},
	}

	_, err := client.ExtensionsV1beta1().ThirdPartyResources().Create(&tpr)

    if IsKubernetesResourceNotFoundError(err) {
        return err
    }

    return nil
}
