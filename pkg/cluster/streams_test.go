package cluster

import (
	"encoding/json"
	"reflect"

	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakezalandov1alpha1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func newFakeK8sStreamClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	zalandoClientSet := fakezalandov1alpha1.NewSimpleClientset()
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		FabricEventStreamsGetter: zalandoClientSet.ZalandoV1alpha1(),
		PostgresqlsGetter:        zalandoClientSet.AcidV1(),
		PodsGetter:               clientSet.CoreV1(),
	}, clientSet
}

var (
	clusterName string = "acid-test-cluster"
	namespace   string = "default"
	pg                 = acidv1.Postgresql{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Postgresql",
			APIVersion: "acid.zalan.do/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			Databases: map[string]string{
				"foo": "foo_user",
			},
			Streams: []acidv1.Stream{
				{
					StreamType: "nakadi",
					Database:   "foo",
					Tables: map[string]string{
						"bar": "stream_type_a",
					},
					BatchSize: uint32(100),
				},
				{
					StreamType: "sqs",
					Database:   "foo",
					Tables: map[string]string{
						"bar": "stream_type_a",
					},
					SqsArn:    "arn:aws:sqs:eu-central-1:111122223333",
					QueueName: "foo-queue",
				},
			},
			Users: map[string]acidv1.UserFlags{
				"foo_user": []string{"replication"},
			},
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}
)

func TestGenerateFabricEventStream(t *testing.T) {
	client, _ := newFakeK8sStreamClient()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					DefaultCPURequest:    "300m",
					DefaultCPULimit:      "300m",
					DefaultMemoryRequest: "300Mi",
					DefaultMemoryLimit:   "300Mi",
					PodRoleLabel:         "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	err := cluster.syncStreams()
	assert.NoError(t, err)

	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), cluster.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream()
	if !reflect.DeepEqual(result, streamCRD) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", streamCRD, result)
	}
}

func TestUpdateFabricEventStream(t *testing.T) {
	client, _ := newFakeK8sStreamClient()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:        map[string]string{"application": "spilo"},
					ClusterNameLabel:     "cluster-name",
					DefaultCPURequest:    "300m",
					DefaultCPULimit:      "300m",
					DefaultMemoryRequest: "300Mi",
					DefaultMemoryLimit:   "300Mi",
					PodRoleLabel:         "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	_, err := cluster.KubeClient.Postgresqls(namespace).Create(
		context.TODO(), &pg, metav1.CreateOptions{})
	assert.NoError(t, err)
	err = cluster.syncStreams()
	assert.NoError(t, err)

	var pgSpec acidv1.PostgresSpec
	pgSpec.Streams = []acidv1.Stream{
		{
			StreamType: "nakadi",
			Database:   "foo",
			Tables: map[string]string{
				"bar": "stream_type_b",
			},
			BatchSize: uint32(250),
		},
	}
	patch, err := json.Marshal(struct {
		PostgresSpec interface{} `json:"spec"`
	}{&pgSpec})
	assert.NoError(t, err)

	pgPatched, err := cluster.KubeClient.Postgresqls(namespace).Patch(
		context.TODO(), cluster.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "spec")
	assert.NoError(t, err)

	cluster.Postgresql.Spec = pgPatched.Spec
	err = cluster.syncStreams()
	assert.NoError(t, err)

	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), cluster.Name, metav1.GetOptions{})
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream()
	if !reflect.DeepEqual(result, streamCRD) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", streamCRD, result)
	}
}
