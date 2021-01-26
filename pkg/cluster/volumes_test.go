package cluster

import (
	"fmt"
	"testing"

	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/assert"
	"github.com/zalando/postgres-operator/mocks"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/volumes"
	"k8s.io/client-go/kubernetes/fake"
)

func newFakeK8sPVCclient() (k8sutil.KubernetesClient, *fake.Clientset) {
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		PersistentVolumeClaimsGetter: clientSet.CoreV1(),
		PersistentVolumesGetter:      clientSet.CoreV1(),
		PodsGetter:                   clientSet.CoreV1(),
	}, clientSet
}

func TestResizeVolumeClaim(t *testing.T) {
	testName := "test resizing of persistent volume claims"
	client, _ := newFakeK8sPVCclient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	newVolumeSize := "2Gi"

	storage1Gi, err := resource.ParseQuantity("1Gi")
	assert.NoError(t, err)

	// new cluster with pvc storage resize mode and configured labels
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
	pvcList := CreatePVCs(namespace, clusterName, filterLabels, 2, "1Gi")
	// add another PVC with different cluster name
	pvcList.Items = append(pvcList.Items, CreatePVCs(namespace, clusterName+"-2", labels.Set{}, 1, "1Gi").Items[0])

	for _, pvc := range pvcList.Items {
		cluster.KubeClient.PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})
	}

	// test resizing
	cluster.resizeVolumeClaims(acidv1.Volume{Size: newVolumeSize})

	pvcs, err := cluster.listPersistentVolumeClaims()
	assert.NoError(t, err)

	// check if listPersistentVolumeClaims returns only the PVCs matching the filter
	if len(pvcs) != len(pvcList.Items)-1 {
		t.Errorf("%s: could not find all PVCs, got %v, expected %v", testName, len(pvcs), len(pvcList.Items)-1)
	}

	// check if PVCs were correctly resized
	for _, pvc := range pvcs {
		newStorageSize := quantityToGigabyte(pvc.Spec.Resources.Requests[v1.ResourceStorage])
		expectedQuantity, err := resource.ParseQuantity(newVolumeSize)
		assert.NoError(t, err)
		expectedSize := quantityToGigabyte(expectedQuantity)
		if newStorageSize != expectedSize {
			t.Errorf("%s: resizing failed, got %v, expected %v", testName, newStorageSize, expectedSize)
		}
	}

	// check if other PVC was not resized
	pvc2, err := cluster.KubeClient.PersistentVolumeClaims(namespace).Get(context.TODO(), constants.DataVolumeName+"-"+clusterName+"-2-0", metav1.GetOptions{})
	assert.NoError(t, err)
	unchangedSize := quantityToGigabyte(pvc2.Spec.Resources.Requests[v1.ResourceStorage])
	expectedSize := quantityToGigabyte(storage1Gi)
	if unchangedSize != expectedSize {
		t.Errorf("%s: volume size changed, got %v, expected %v", testName, unchangedSize, expectedSize)
	}
}

func TestQuantityToGigabyte(t *testing.T) {
	tests := []struct {
		name        string
		quantityStr string
		expected    int64
	}{
		{
			"test with 1Gi",
			"1Gi",
			1,
		},
		{
			"test with float",
			"1.5Gi",
			int64(1),
		},
		{
			"test with 1000Mi",
			"1000Mi",
			int64(0),
		},
	}

	for _, tt := range tests {
		quantity, err := resource.ParseQuantity(tt.quantityStr)
		assert.NoError(t, err)
		gigabyte := quantityToGigabyte(quantity)
		if gigabyte != tt.expected {
			t.Errorf("%s: got %v, expected %v", tt.name, gigabyte, tt.expected)
		}
	}
}

func CreatePVCs(namespace string, clusterName string, labels labels.Set, n int, size string) v1.PersistentVolumeClaimList {
	// define and create PVCs for 1Gi volumes
	storage1Gi, _ := resource.ParseQuantity(size)
	pvcList := v1.PersistentVolumeClaimList{
		Items: []v1.PersistentVolumeClaim{},
	}

	for i := 0; i < n; i++ {
		pvc := v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%d", constants.DataVolumeName, clusterName, i),
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: v1.PersistentVolumeClaimSpec{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: storage1Gi,
					},
				},
				VolumeName: fmt.Sprintf("persistent-volume-%d", i),
			},
		}
		pvcList.Items = append(pvcList.Items, pvc)
	}

	return pvcList
}

func TestMigrateEBS(t *testing.T) {
	client, _ := newFakeK8sPVCclient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	// new cluster with pvc storage resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
				StorageResizeMode:            "pvc",
				EnableEBSGp3Migration:        true,
				EnableEBSGp3MigrationMaxSize: 1000,
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)
	cluster.Spec.Volume.Size = "1Gi"

	// set metadata, so that labels will get correct values
	cluster.Name = clusterName
	cluster.Namespace = namespace
	filterLabels := cluster.labelsSet(false)

	testVolumes := []testVolume{
		{
			size: 100,
		},
		{
			size: 100,
		},
	}

	initTestVolumesAndPods(cluster.KubeClient, namespace, clusterName, filterLabels, testVolumes)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resizer := mocks.NewMockVolumeResizer(ctrl)

	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-1")).Return("ebs-volume-1", nil)
	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-2")).Return("ebs-volume-2", nil)

	resizer.EXPECT().DescribeVolumes(gomock.Eq([]string{"ebs-volume-1", "ebs-volume-2"})).Return(
		[]volumes.VolumeProperties{
			{VolumeID: "ebs-volume-1", VolumeType: "gp2", Size: 100},
			{VolumeID: "ebs-volume-2", VolumeType: "gp3", Size: 100}}, nil)

	// expect only gp2 volume to be modified
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-1"), gomock.Eq(aws.String("gp3")), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	cluster.VolumeResizer = resizer
	cluster.executeEBSMigration()
}

type testVolume struct {
	iops        int64
	throughtput int64
	size        int64
	volType     string
}

func initTestVolumesAndPods(client k8sutil.KubernetesClient, namespace, clustername string, labels labels.Set, volumes []testVolume) {
	i := 0
	for _, v := range volumes {
		storage1Gi, _ := resource.ParseQuantity(fmt.Sprintf("%d", v.size))

		ps := v1.PersistentVolumeSpec{}
		ps.AWSElasticBlockStore = &v1.AWSElasticBlockStoreVolumeSource{}
		ps.AWSElasticBlockStore.VolumeID = fmt.Sprintf("aws://eu-central-1b/ebs-volume-%d", i+1)

		pv := v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("persistent-volume-%d", i),
			},
			Spec: ps,
		}

		client.PersistentVolumes().Create(context.TODO(), &pv, metav1.CreateOptions{})

		pvc := v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s-%d", constants.DataVolumeName, clustername, i),
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: v1.PersistentVolumeClaimSpec{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: storage1Gi,
					},
				},
				VolumeName: fmt.Sprintf("persistent-volume-%d", i),
			},
		}

		client.PersistentVolumeClaims(namespace).Create(context.TODO(), &pvc, metav1.CreateOptions{})

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("%s-%d", clustername, i),
				Labels: labels,
			},
			Spec: v1.PodSpec{},
		}

		client.Pods(namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})

		i = i + 1
	}
}

func TestMigrateGp3Support(t *testing.T) {
	client, _ := newFakeK8sPVCclient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	// new cluster with pvc storage resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
				StorageResizeMode:            "mixed",
				EnableEBSGp3Migration:        false,
				EnableEBSGp3MigrationMaxSize: 1000,
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Spec.Volume.Size = "150Gi"
	cluster.Spec.Volume.Iops = aws.Int64(6000)
	cluster.Spec.Volume.Throughput = aws.Int64(275)

	// set metadata, so that labels will get correct values
	cluster.Name = clusterName
	cluster.Namespace = namespace
	filterLabels := cluster.labelsSet(false)

	testVolumes := []testVolume{
		{
			size: 100,
		},
		{
			size: 100,
		},
		{
			size: 100,
		},
	}

	initTestVolumesAndPods(cluster.KubeClient, namespace, clusterName, filterLabels, testVolumes)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resizer := mocks.NewMockVolumeResizer(ctrl)

	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-1")).Return("ebs-volume-1", nil)
	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-2")).Return("ebs-volume-2", nil)
	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-3")).Return("ebs-volume-3", nil)

	resizer.EXPECT().DescribeVolumes(gomock.Eq([]string{"ebs-volume-1", "ebs-volume-2", "ebs-volume-3"})).Return(
		[]volumes.VolumeProperties{
			{VolumeID: "ebs-volume-1", VolumeType: "gp3", Size: 100, Iops: 3000},
			{VolumeID: "ebs-volume-2", VolumeType: "gp3", Size: 105, Iops: 4000},
			{VolumeID: "ebs-volume-3", VolumeType: "gp3", Size: 151, Iops: 6000, Throughput: 275}}, nil)

	// expect only gp2 volume to be modified
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-1"), gomock.Eq(aws.String("gp3")), gomock.Eq(aws.Int64(150)), gomock.Eq(aws.Int64(6000)), gomock.Eq(aws.Int64(275))).Return(nil)
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-2"), gomock.Eq(aws.String("gp3")), gomock.Eq(aws.Int64(150)), gomock.Eq(aws.Int64(6000)), gomock.Eq(aws.Int64(275))).Return(nil)
	// resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-3"), gomock.Eq(aws.String("gp3")), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	cluster.VolumeResizer = resizer
	cluster.syncVolumes()
}

func TestManualGp2Gp3Support(t *testing.T) {
	client, _ := newFakeK8sPVCclient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	// new cluster with pvc storage resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
				StorageResizeMode:            "mixed",
				EnableEBSGp3Migration:        false,
				EnableEBSGp3MigrationMaxSize: 1000,
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Spec.Volume.Size = "150Gi"
	cluster.Spec.Volume.Iops = aws.Int64(6000)
	cluster.Spec.Volume.Throughput = aws.Int64(275)

	// set metadata, so that labels will get correct values
	cluster.Name = clusterName
	cluster.Namespace = namespace
	filterLabels := cluster.labelsSet(false)

	testVolumes := []testVolume{
		{
			size: 100,
		},
		{
			size: 100,
		},
	}

	initTestVolumesAndPods(cluster.KubeClient, namespace, clusterName, filterLabels, testVolumes)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resizer := mocks.NewMockVolumeResizer(ctrl)

	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-1")).Return("ebs-volume-1", nil)
	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-2")).Return("ebs-volume-2", nil)

	resizer.EXPECT().DescribeVolumes(gomock.Eq([]string{"ebs-volume-1", "ebs-volume-2"})).Return(
		[]volumes.VolumeProperties{
			{VolumeID: "ebs-volume-1", VolumeType: "gp2", Size: 150, Iops: 3000},
			{VolumeID: "ebs-volume-2", VolumeType: "gp2", Size: 150, Iops: 4000},
		}, nil)

	// expect only gp2 volume to be modified
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-1"), gomock.Eq(aws.String("gp3")), gomock.Nil(), gomock.Eq(aws.Int64(6000)), gomock.Eq(aws.Int64(275))).Return(nil)
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-2"), gomock.Eq(aws.String("gp3")), gomock.Nil(), gomock.Eq(aws.Int64(6000)), gomock.Eq(aws.Int64(275))).Return(nil)

	cluster.VolumeResizer = resizer
	cluster.syncVolumes()
}

func TestDontTouchType(t *testing.T) {
	client, _ := newFakeK8sPVCclient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	// new cluster with pvc storage resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
				StorageResizeMode:            "mixed",
				EnableEBSGp3Migration:        false,
				EnableEBSGp3MigrationMaxSize: 1000,
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Spec.Volume.Size = "177Gi"

	// set metadata, so that labels will get correct values
	cluster.Name = clusterName
	cluster.Namespace = namespace
	filterLabels := cluster.labelsSet(false)

	testVolumes := []testVolume{
		{
			size: 150,
		},
		{
			size: 150,
		},
	}

	initTestVolumesAndPods(cluster.KubeClient, namespace, clusterName, filterLabels, testVolumes)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resizer := mocks.NewMockVolumeResizer(ctrl)

	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-1")).Return("ebs-volume-1", nil)
	resizer.EXPECT().ExtractVolumeID(gomock.Eq("aws://eu-central-1b/ebs-volume-2")).Return("ebs-volume-2", nil)

	resizer.EXPECT().DescribeVolumes(gomock.Eq([]string{"ebs-volume-1", "ebs-volume-2"})).Return(
		[]volumes.VolumeProperties{
			{VolumeID: "ebs-volume-1", VolumeType: "gp2", Size: 150, Iops: 3000},
			{VolumeID: "ebs-volume-2", VolumeType: "gp2", Size: 150, Iops: 4000},
		}, nil)

	// expect only gp2 volume to be modified
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-1"), gomock.Nil(), gomock.Eq(aws.Int64(177)), gomock.Nil(), gomock.Nil()).Return(nil)
	resizer.EXPECT().ModifyVolume(gomock.Eq("ebs-volume-2"), gomock.Nil(), gomock.Eq(aws.Int64(177)), gomock.Nil(), gomock.Nil()).Return(nil)

	cluster.VolumeResizer = resizer
	cluster.syncVolumes()
}
