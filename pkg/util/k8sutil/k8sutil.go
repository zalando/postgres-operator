package k8sutil

import (
	"fmt"
	"reflect"

	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextbeta1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	policyv1beta1 "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	policybeta1 "k8s.io/client-go/pkg/apis/policy/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/zalando-incubator/postgres-operator/pkg/util/constants"
)

// KubernetesClient describes getters for Kubernetes objects
type KubernetesClient struct {
	v1core.SecretsGetter
	v1core.ServicesGetter
	v1core.EndpointsGetter
	v1core.PodsGetter
	v1core.PersistentVolumesGetter
	v1core.PersistentVolumeClaimsGetter
	v1core.ConfigMapsGetter
	v1core.NodesGetter
	v1core.NamespacesGetter
	v1core.ServiceAccountsGetter
	v1beta1.StatefulSetsGetter
	policyv1beta1.PodDisruptionBudgetsGetter
	apiextbeta1.CustomResourceDefinitionsGetter

	RESTClient rest.Interface
	CRDREST    rest.Interface
}

// RestConfig creates REST config
func RestConfig(kubeConfig string, outOfCluster bool) (*rest.Config, error) {
	if outOfCluster {
		return clientcmd.BuildConfigFromFlags("", kubeConfig)
	}

	return rest.InClusterConfig()
}

// ResourceAlreadyExists checks if error corresponds to Already exists error
func ResourceAlreadyExists(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

// ResourceNotFound checks if error corresponds to Not found error
func ResourceNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

// NewFromConfig create Kubernets Interface using REST config
func NewFromConfig(cfg *rest.Config) (KubernetesClient, error) {
	kubeClient := KubernetesClient{}

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return kubeClient, fmt.Errorf("could not get clientset: %v", err)
	}

	kubeClient.PodsGetter = client.CoreV1()
	kubeClient.ServicesGetter = client.CoreV1()
	kubeClient.EndpointsGetter = client.CoreV1()
	kubeClient.SecretsGetter = client.CoreV1()
	kubeClient.ServiceAccountsGetter = client.CoreV1()
	kubeClient.ConfigMapsGetter = client.CoreV1()
	kubeClient.PersistentVolumeClaimsGetter = client.CoreV1()
	kubeClient.PersistentVolumesGetter = client.CoreV1()
	kubeClient.NodesGetter = client.CoreV1()
	kubeClient.NamespacesGetter = client.CoreV1()
	kubeClient.StatefulSetsGetter = client.AppsV1beta1()
	kubeClient.PodDisruptionBudgetsGetter = client.PolicyV1beta1()
	kubeClient.RESTClient = client.CoreV1().RESTClient()

	cfg2 := *cfg
	cfg2.GroupVersion = &schema.GroupVersion{
		Group:   constants.CRDGroup,
		Version: constants.CRDApiVersion,
	}
	cfg2.APIPath = constants.K8sAPIPath
	cfg2.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	crd, err := rest.RESTClientFor(&cfg2)
	if err != nil {
		return kubeClient, fmt.Errorf("could not get rest client: %v", err)
	}
	kubeClient.CRDREST = crd

	apiextClient, err := apiextclient.NewForConfig(cfg)
	if err != nil {
		return kubeClient, fmt.Errorf("could not create api client:%v", err)
	}

	kubeClient.CustomResourceDefinitionsGetter = apiextClient.ApiextensionsV1beta1()

	return kubeClient, nil
}

// SameService compares the Services
func SameService(cur, new *v1.Service) (match bool, reason string) {
	//TODO: improve comparison
	if cur.Spec.Type != new.Spec.Type {
		return false, fmt.Sprintf("new service's type %q doesn't match the current one %q",
			new.Spec.Type, cur.Spec.Type)
	}

	oldSourceRanges := cur.Spec.LoadBalancerSourceRanges
	newSourceRanges := new.Spec.LoadBalancerSourceRanges

	/* work around Kubernetes 1.6 serializing [] as nil. See https://github.com/kubernetes/kubernetes/issues/43203 */
	if (len(oldSourceRanges) == 0) && (len(newSourceRanges) == 0) {
		return true, ""
	}
	if !reflect.DeepEqual(oldSourceRanges, newSourceRanges) {
		return false, "new service's LoadBalancerSourceRange doesn't match the current one"
	}

	oldDNSAnnotation := cur.Annotations[constants.ZalandoDNSNameAnnotation]
	newDNSAnnotation := new.Annotations[constants.ZalandoDNSNameAnnotation]
	if oldDNSAnnotation != newDNSAnnotation {
		return false, fmt.Sprintf("new service's %q annotation doesn't match the current one", constants.ZalandoDNSNameAnnotation)
	}

	return true, ""
}

// SamePDB compares the PodDisruptionBudgets
func SamePDB(cur, new *policybeta1.PodDisruptionBudget) (match bool, reason string) {
	//TODO: improve comparison
	match = reflect.DeepEqual(new.Spec, cur.Spec)
	if !match {
		reason = "new service spec doesn't match the current one"
	}

	return
}
