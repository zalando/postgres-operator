package k8sutil

import (
	"fmt"
	"reflect"

	b64 "encoding/base64"

	batchv1beta1 "k8s.io/api/batch/v1beta1"
	clientbatchv1beta1 "k8s.io/client-go/kubernetes/typed/batch/v1beta1"

	"github.com/zalando/postgres-operator/pkg/util/constants"
	v1 "k8s.io/api/core/v1"
	policybeta1 "k8s.io/api/policy/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextbeta1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	policyv1beta1 "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	rbacv1beta1 "k8s.io/client-go/kubernetes/typed/rbac/v1beta1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	acidv1client "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	rbacv1beta1.RoleBindingsGetter
	policyv1beta1.PodDisruptionBudgetsGetter
	apiextbeta1.CustomResourceDefinitionsGetter
	clientbatchv1beta1.CronJobsGetter

	RESTClient      rest.Interface
	AcidV1ClientSet *acidv1client.Clientset
}

type mockSecret struct {
	v1core.SecretInterface
}

type MockSecretGetter struct {
}

type mockConfigMap struct {
	v1core.ConfigMapInterface
}

type MockConfigMapsGetter struct {
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
	kubeClient.RoleBindingsGetter = client.RbacV1beta1()
	kubeClient.CronJobsGetter = client.BatchV1beta1()

	apiextClient, err := apiextclient.NewForConfig(cfg)
	if err != nil {
		return kubeClient, fmt.Errorf("could not create api client:%v", err)
	}

	kubeClient.CustomResourceDefinitionsGetter = apiextClient.ApiextensionsV1beta1()
	kubeClient.AcidV1ClientSet = acidv1client.NewForConfigOrDie(cfg)

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
	if (len(oldSourceRanges) != 0) || (len(newSourceRanges) != 0) {
		if !reflect.DeepEqual(oldSourceRanges, newSourceRanges) {
			return false, "new service's LoadBalancerSourceRange doesn't match the current one"
		}
	}

	oldDNSAnnotation := cur.Annotations[constants.ZalandoDNSNameAnnotation]
	newDNSAnnotation := new.Annotations[constants.ZalandoDNSNameAnnotation]
	oldELBAnnotation := cur.Annotations[constants.ElbTimeoutAnnotationName]
	newELBAnnotation := new.Annotations[constants.ElbTimeoutAnnotationName]

	if oldDNSAnnotation != newDNSAnnotation {
		return false, fmt.Sprintf("new service's %q annotation value %q doesn't match the current one %q",
			constants.ZalandoDNSNameAnnotation, newDNSAnnotation, oldDNSAnnotation)
	}
	if oldELBAnnotation != newELBAnnotation {
		return false, fmt.Sprintf("new service's %q annotation value %q doesn't match the current one %q",
			constants.ElbTimeoutAnnotationName, oldELBAnnotation, newELBAnnotation)
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

func getJobImage(cronJob *batchv1beta1.CronJob) string {
	return cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Image
}

// SameLogicalBackupJob compares Specs of logical backup cron jobs
func SameLogicalBackupJob(cur, new *batchv1beta1.CronJob) (match bool, reason string) {

	if cur.Spec.Schedule != new.Spec.Schedule {
		return false, fmt.Sprintf("new job's schedule %q doesn't match the current one %q",
			new.Spec.Schedule, cur.Spec.Schedule)
	}

	newImage := getJobImage(new)
	curImage := getJobImage(cur)
	if newImage != curImage {
		return false, fmt.Sprintf("new job's image %q doesn't match the current one %q",
			newImage, curImage)
	}

	return true, ""
}

func (c *mockSecret) Get(name string, options metav1.GetOptions) (*v1.Secret, error) {
	if name != "infrastructureroles-test" {
		return nil, fmt.Errorf("NotFound")
	}
	secret := &v1.Secret{}
	secret.Name = "testcluster"
	secret.Data = map[string][]byte{
		"user1":     []byte("testrole"),
		"password1": []byte("testpassword"),
		"inrole1":   []byte("testinrole"),
		"foobar":    []byte(b64.StdEncoding.EncodeToString([]byte("password"))),
	}
	return secret, nil

}

func (c *mockConfigMap) Get(name string, options metav1.GetOptions) (*v1.ConfigMap, error) {
	if name != "infrastructureroles-test" {
		return nil, fmt.Errorf("NotFound")
	}
	configmap := &v1.ConfigMap{}
	configmap.Name = "testcluster"
	configmap.Data = map[string]string{
		"foobar": "{}",
	}
	return configmap, nil
}

// Secrets to be mocked
func (c *MockSecretGetter) Secrets(namespace string) v1core.SecretInterface {
	return &mockSecret{}
}

// ConfigMaps to be mocked
func (c *MockConfigMapsGetter) ConfigMaps(namespace string) v1core.ConfigMapInterface {
	return &mockConfigMap{}
}

// NewMockKubernetesClient for other tests
func NewMockKubernetesClient() KubernetesClient {
	return KubernetesClient{
		SecretsGetter:    &MockSecretGetter{},
		ConfigMapsGetter: &MockConfigMapsGetter{},
	}
}
