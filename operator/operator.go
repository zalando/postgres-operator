package operator

import (
	"fmt"
	"log"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	etcdHostOutside string

	VENDOR       = "acid.zalan.do"
	VERSION      = "0.0.1.dev"
	resyncPeriod = 5 * time.Minute

	etcdKeyTemplate = "/service/%s"
)

type Options struct {
	KubeConfig string
}

type Pgconf struct {
	Parameter string `json:"param"`
	Value     string `json:"value"`
}

type SpiloSpec struct {
	EtcdHost              string   `json:"etcd_host"`
	VolumeSize            int      `json:"volume_size"`
	NumberOfInstances     int32    `json:"number_of_instances"`
	DockerImage           string   `json:"docker_image"`
	PostgresConfiguration []Pgconf `json:"postgres_configuration"`
	ResourceCPU           string   `json:"resource_cpu"`
	ResourceMemory        string   `json:"resource_memory"`
}

type Spilo struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             api.ObjectMeta `json:"metadata"`
	Spec                 SpiloSpec      `json:"spec"`
}

type SpiloList struct {
	unversioned.TypeMeta `json:",inline"`
	Metadata             unversioned.ListMeta `json:"metadata"`
	Items                []Spilo              `json:"items"`
}

func KubernetesConfig(options Options) *rest.Config {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}

	if options.KubeConfig != "" {
		rules.ExplicitPath = options.KubeConfig
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()

	etcdHostOutside = config.Host

	if err != nil {
		log.Fatalf("Couldn't get Kubernetes default config: %s", err)
	}

	return config
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
				&Spilo{},
				&SpiloList{},
				&api.ListOptions{},
				&api.DeleteOptions{},
			)
			return nil
		})
	schemeBuilder.AddToScheme(api.Scheme)

	return rest.RESTClientFor(c)
}

func EnsureSpiloThirdPartyResource(client *kubernetes.Clientset) error {
	_, err := client.ExtensionsV1beta1().ThirdPartyResources().Get(fmt.Sprintf("spilo.%s", VENDOR))
	if err == nil {
		return err
	}

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

	_, err = client.ExtensionsV1beta1().ThirdPartyResources().Create(&tpr)

	return err
}
