package cluster

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	zalandov1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1"
	fakezalandov1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func newFakeK8sStreamClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	zalandoClientSet := fakezalandov1.NewSimpleClientset()
	clientSet := fake.NewSimpleClientset()

	return k8sutil.KubernetesClient{
		FabricEventStreamsGetter: zalandoClientSet.ZalandoV1(),
		PostgresqlsGetter:        zalandoClientSet.AcidV1(),
		PodsGetter:               clientSet.CoreV1(),
		StatefulSetsGetter:       clientSet.AppsV1(),
	}, clientSet
}

var (
	clusterName string = "acid-test-cluster"
	namespace   string = "default"
	appId       string = "test-app"
	dbName      string = "foo"
	fesUser     string = fmt.Sprintf("%s%s", constants.EventStreamSourceSlotPrefix, constants.UserRoleNameSuffix)
	fesName     string = fmt.Sprintf("%s-%s", clusterName, appId)
	slotName    string = fmt.Sprintf("%s_%s_%s", constants.EventStreamSourceSlotPrefix, dbName, strings.Replace(appId, "-", "_", -1))

	pg = acidv1.Postgresql{
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
				dbName: fmt.Sprintf("%s%s", dbName, constants.UserRoleNameSuffix),
			},
			Streams: []acidv1.Stream{
				{
					ApplicationId: appId,
					Database:      "foo",
					Tables: map[string]acidv1.StreamTable{
						"data.bar": acidv1.StreamTable{
							EventType:     "stream-type-a",
							IdColumn:      k8sutil.StringToPointer("b_id"),
							PayloadColumn: k8sutil.StringToPointer("b_payload"),
						},
						"data.foobar": acidv1.StreamTable{
							EventType: "stream-type-b",
						},
					},
					Filter: map[string]*string{
						"data.bar": k8sutil.StringToPointer("[?(@.source.txId > 500 && @.source.lsn > 123456)]"),
					},
					BatchSize: k8sutil.UInt32ToPointer(uint32(100)),
				},
			},
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	fes = &zalandov1.FabricEventStream{
		TypeMeta: metav1.TypeMeta{
			APIVersion: constants.EventStreamCRDApiVersion,
			Kind:       constants.EventStreamCRDKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fesName,
			Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{
				metav1.OwnerReference{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       "acid-test-cluster",
					Controller: util.True(),
				},
			},
		},
		Spec: zalandov1.FabricEventStreamSpec{
			ApplicationId: appId,
			EventStreams: []zalandov1.EventStream{
				zalandov1.EventStream{
					EventStreamFlow: zalandov1.EventStreamFlow{
						PayloadColumn: k8sutil.StringToPointer("b_payload"),
						Type:          constants.EventStreamFlowPgGenericType,
					},
					EventStreamSink: zalandov1.EventStreamSink{
						EventType:    "stream-type-a",
						MaxBatchSize: k8sutil.UInt32ToPointer(uint32(100)),
						Type:         constants.EventStreamSinkNakadiType,
					},
					EventStreamSource: zalandov1.EventStreamSource{
						Filter: k8sutil.StringToPointer("[?(@.source.txId > 500 && @.source.lsn > 123456)]"),
						Connection: zalandov1.Connection{
							DBAuth: zalandov1.DBAuth{
								Name:        fmt.Sprintf("fes-user.%s.credentials.postgresql.acid.zalan.do", clusterName),
								PasswordKey: "password",
								Type:        constants.EventStreamSourceAuthType,
								UserKey:     "username",
							},
							Url:        fmt.Sprintf("jdbc:postgresql://%s.%s/foo?user=%s&ssl=true&sslmode=require", clusterName, namespace, fesUser),
							SlotName:   slotName,
							PluginType: constants.EventStreamSourcePluginType,
						},
						Schema: "data",
						EventStreamTable: zalandov1.EventStreamTable{
							IDColumn: k8sutil.StringToPointer("b_id"),
							Name:     "bar",
						},
						Type: constants.EventStreamSourcePGType,
					},
				},
				zalandov1.EventStream{
					EventStreamFlow: zalandov1.EventStreamFlow{
						Type: constants.EventStreamFlowPgGenericType,
					},
					EventStreamSink: zalandov1.EventStreamSink{
						EventType:    "stream-type-b",
						MaxBatchSize: k8sutil.UInt32ToPointer(uint32(100)),
						Type:         constants.EventStreamSinkNakadiType,
					},
					EventStreamSource: zalandov1.EventStreamSource{
						Connection: zalandov1.Connection{
							DBAuth: zalandov1.DBAuth{
								Name:        fmt.Sprintf("fes-user.%s.credentials.postgresql.acid.zalan.do", clusterName),
								PasswordKey: "password",
								Type:        constants.EventStreamSourceAuthType,
								UserKey:     "username",
							},
							Url:        fmt.Sprintf("jdbc:postgresql://%s.%s/foo?user=%s&ssl=true&sslmode=require", clusterName, namespace, fesUser),
							SlotName:   slotName,
							PluginType: constants.EventStreamSourcePluginType,
						},
						Schema: "data",
						EventStreamTable: zalandov1.EventStreamTable{
							Name: "foobar",
						},
						Type: constants.EventStreamSourcePGType,
					},
				},
			},
		},
	}
)

func TestGenerateFabricEventStream(t *testing.T) {
	client, _ := newFakeK8sStreamClient()

	var cluster = New(
		Config{
			OpConfig: config.Config{
				Auth: config.Auth{
					SecretNameTemplate: "{username}.{cluster}.credentials.{tprkind}.{tprgroup}",
				},
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

	cluster.Name = clusterName
	cluster.Namespace = namespace

	// create statefulset to have ownerReference for streams
	_, err := cluster.createStatefulSet()
	assert.NoError(t, err)

	// createOrUpdateStreams will loop over existing apps
	cluster.streamApplications = []string{appId}

	// create the streams
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	// compare generated stream with expected stream
	result := cluster.generateFabricEventStream(appId)
	if match, _ := sameStreams(result.Spec.EventStreams, fes.Spec.EventStreams); !match {
		t.Errorf("malformed FabricEventStream, expected %#v, got %#v", fes, result)
	}

	// compare stream resturned from API with expected stream
	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
	assert.NoError(t, err)
	if match, _ := sameStreams(streamCRD.Spec.EventStreams, fes.Spec.EventStreams); !match {
		t.Errorf("malformed FabricEventStream returned from API, expected %#v, got %#v", fes, streamCRD)
	}

	// sync streams once again
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	// compare stream resturned from API with generated stream
	streamCRD, err = cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
	assert.NoError(t, err)
	if match, _ := sameStreams(streamCRD.Spec.EventStreams, result.Spec.EventStreams); !match {
		t.Errorf("returned FabricEventStream differs from generated one, expected %#v, got %#v", result, streamCRD)
	}
}

func TestSameStreams(t *testing.T) {
	testName := "TestSameStreams"

	stream1 := zalandov1.EventStream{
		EventStreamFlow: zalandov1.EventStreamFlow{},
		EventStreamSink: zalandov1.EventStreamSink{
			EventType: "stream-type-a",
		},
		EventStreamSource: zalandov1.EventStreamSource{
			EventStreamTable: zalandov1.EventStreamTable{
				Name: "foo",
			},
		},
	}

	stream2 := zalandov1.EventStream{
		EventStreamFlow: zalandov1.EventStreamFlow{},
		EventStreamSink: zalandov1.EventStreamSink{
			EventType: "stream-type-b",
		},
		EventStreamSource: zalandov1.EventStreamSource{
			EventStreamTable: zalandov1.EventStreamTable{
				Name: "bar",
			},
		},
	}

	tests := []struct {
		subTest  string
		streamsA []zalandov1.EventStream
		streamsB []zalandov1.EventStream
		match    bool
		reason   string
	}{
		{
			subTest:  "identical streams",
			streamsA: []zalandov1.EventStream{stream1, stream2},
			streamsB: []zalandov1.EventStream{stream1, stream2},
			match:    true,
			reason:   "",
		},
		{
			subTest:  "same streams different order",
			streamsA: []zalandov1.EventStream{stream1, stream2},
			streamsB: []zalandov1.EventStream{stream2, stream1},
			match:    true,
			reason:   "",
		},
		{
			subTest:  "same streams different order",
			streamsA: []zalandov1.EventStream{stream1},
			streamsB: []zalandov1.EventStream{stream1, stream2},
			match:    false,
			reason:   "number of defined streams is different",
		},
		{
			subTest:  "different number of streams",
			streamsA: []zalandov1.EventStream{stream1},
			streamsB: []zalandov1.EventStream{stream1, stream2},
			match:    false,
			reason:   "number of defined streams is different",
		},
		{
			subTest:  "event stream specs differ",
			streamsA: []zalandov1.EventStream{stream1, stream2},
			streamsB: fes.Spec.EventStreams,
			match:    false,
			reason:   "number of defined streams is different",
		},
	}

	for _, tt := range tests {
		streamsMatch, matchReason := sameStreams(tt.streamsA, tt.streamsB)
		if streamsMatch != tt.match {
			t.Errorf("%s %s: unexpected match result when comparing streams: got %s, epxected %s",
				testName, tt.subTest, matchReason, tt.reason)
		}
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

	// createOrUpdateStreams will loop over existing apps
	cluster.streamApplications = []string{appId}

	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	var pgSpec acidv1.PostgresSpec
	pgSpec.Streams = []acidv1.Stream{
		{
			ApplicationId: appId,
			Database:      dbName,
			Tables: map[string]acidv1.StreamTable{
				"data.bar": acidv1.StreamTable{
					EventType:     "stream-type-c",
					IdColumn:      k8sutil.StringToPointer("b_id"),
					PayloadColumn: k8sutil.StringToPointer("b_payload"),
				},
			},
			BatchSize: k8sutil.UInt32ToPointer(uint32(250)),
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
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream(appId)
	if !reflect.DeepEqual(result, streamCRD) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", streamCRD, result)
	}
}
