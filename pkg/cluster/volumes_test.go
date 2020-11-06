package cluster

import (
	"testing"

	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"k8s.io/client-go/kubernetes/fake"
)

func NewFakeKubernetesClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		PersistentVolumeClaimsGetter: clientSet.CoreV1(),
	}, clientSet
}

func TestResizeVolumeClaim(t *testing.T) {
	testName := "test resizing of persistent volume claims"
	client, _ := NewFakeKubernetesClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	newVolumeSize := "2Gi"

	// new cluster with pvc resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
				StorageResizeMode: "pvc",
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	// set metadata, so that labels will get correct values
	cluster.Name = clusterName
	cluster.Namespace = namespace
	filterLabels := cluster.labelsSet(false)

	// define and create PVCs for 1Gi volumes
	parsedStorage, err := resource.ParseQuantity("1Gi")
	assert.NoError(t, err)

	pvcList := &v1.PersistentVolumeClaimList{
		Items: []v1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pgdata-" + clusterName + "-0",
					Namespace: namespace,
					Labels:    filterLabels,
				},
				Spec: v1.PersistentVolumeClaimSpec{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceStorage: parsedStorage,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pgdata-" + clusterName + "-1",
					Namespace: namespace,
					Labels:    filterLabels,
				},
				Spec: v1.PersistentVolumeClaimSpec{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceStorage: parsedStorage,
						},
					},
				},
			},
		},
	}

	for _, pvc := range pvcList.Items {
		cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	}

	// test resizing
	cluster.resizeVolumeClaims(acidv1.Volume{Size: newVolumeSize})

	pvcs, err := cluster.listPersistentVolumeClaims()
	assert.NoError(t, err)

	if len(pvcs) != len(pvcList.Items) {
		t.Errorf("%s: could not find all PVCs, got %v, expected %v", testName, len(pvcs), len(pvcList.Items))
	}

	for _, pvc := range pvcs {
		newStorageSize := quantityToGigabyte(pvc.Spec.Resources.Requests[v1.ResourceStorage])
		expectedQuantity, err := resource.ParseQuantity(newVolumeSize)
		assert.NoError(t, err)
		expectedSize := quantityToGigabyte(expectedQuantity)
		if newStorageSize != expectedSize {
			t.Errorf("%s: resizing failed, got %v, expected %v", testName, newStorageSize, expectedSize)
		}
	}
}
