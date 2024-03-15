package cluster

import (
	"fmt"
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
							EventType:         "stream-type-b",
							RecoveryEventType: "stream-type-b-dlq",
						},
					},
					EnableRecovery: util.True(),
					Filter: map[string]*string{
						"data.bar": k8sutil.StringToPointer("[?(@.source.txId > 500 && @.source.lsn > 123456)]"),
					},
					BatchSize: k8sutil.UInt32ToPointer(uint32(100)),
				},
			},
			TeamID: "acid",
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
			Name:      fmt.Sprintf("%s-12345", clusterName),
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
					EventStreamRecovery: zalandov1.EventStreamRecovery{
						Type: constants.EventStreamRecoveryDLQType,
						Sink: &zalandov1.EventStreamSink{
							EventType:    fmt.Sprintf("%s-%s", "stream-type-a", constants.EventStreamRecoverySuffix),
							MaxBatchSize: k8sutil.UInt32ToPointer(uint32(100)),
							Type:         constants.EventStreamSinkNakadiType,
						},
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
					EventStreamRecovery: zalandov1.EventStreamRecovery{
						Type: constants.EventStreamRecoveryDLQType,
						Sink: &zalandov1.EventStreamSink{
							EventType:    "stream-type-b-dlq",
							MaxBatchSize: k8sutil.UInt32ToPointer(uint32(100)),
							Type:         constants.EventStreamSinkNakadiType,
						},
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

func TestGatherApplicationIds(t *testing.T) {
	testAppIds := []string{appId}
	appIds := gatherApplicationIds(pg.Spec.Streams)

	if !util.IsEqualIgnoreOrder(testAppIds, appIds) {
		t.Errorf("gathered applicationIds do not match, expected %#v, got %#v", testAppIds, appIds)
	}
}

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

	// create the streams
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	// compare generated stream with expected stream
	result := cluster.generateFabricEventStream(appId)
	if match, _ := sameStreams(result.Spec.EventStreams, fes.Spec.EventStreams); !match {
		t.Errorf("malformed FabricEventStream, expected %#v, got %#v", fes, result)
	}

	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(true).String(),
	}
	streams, err := cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)

	// check if there is only one stream
	if len(streams.Items) > 1 {
		t.Errorf("too many stream CRDs found: got %d, but expected only one", len(streams.Items))
	}

	// compare stream returned from API with expected stream
	if match, _ := sameStreams(streams.Items[0].Spec.EventStreams, fes.Spec.EventStreams); !match {
		t.Errorf("malformed FabricEventStream returned from API, expected %#v, got %#v", fes, streams.Items[0])
	}

	// sync streams once again
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	streams, err = cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)

	// check if there is still only one stream
	if len(streams.Items) > 1 {
		t.Errorf("too many stream CRDs found after sync: got %d, but expected only one", len(streams.Items))
	}

	// compare stream resturned from API with generated stream
	if match, _ := sameStreams(streams.Items[0].Spec.EventStreams, result.Spec.EventStreams); !match {
		t.Errorf("returned FabricEventStream differs from generated one, expected %#v, got %#v", result, streams.Items[0])
	}
}

func TestSameStreams(t *testing.T) {
	testName := "TestSameStreams"

	stream1 := zalandov1.EventStream{
		EventStreamFlow:     zalandov1.EventStreamFlow{},
		EventStreamRecovery: zalandov1.EventStreamRecovery{},
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
		EventStreamFlow:     zalandov1.EventStreamFlow{},
		EventStreamRecovery: zalandov1.EventStreamRecovery{},
		EventStreamSink: zalandov1.EventStreamSink{
			EventType: "stream-type-b",
		},
		EventStreamSource: zalandov1.EventStreamSource{
			EventStreamTable: zalandov1.EventStreamTable{
				Name: "bar",
			},
		},
	}

	stream3 := zalandov1.EventStream{
		EventStreamFlow: zalandov1.EventStreamFlow{},
		EventStreamRecovery: zalandov1.EventStreamRecovery{
			Type: constants.EventStreamRecoveryNoneType,
		},
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
		{
			subTest:  "event stream recovery specs differ",
			streamsA: []zalandov1.EventStream{stream2},
			streamsB: []zalandov1.EventStream{stream3},
			match:    false,
			reason:   "event stream specs differ",
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

	// create statefulset to have ownerReference for streams
	_, err = cluster.createStatefulSet()
	assert.NoError(t, err)

	// now create the stream
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	// change specs of streams and patch CRD
	for i, stream := range pg.Spec.Streams {
		if stream.ApplicationId == appId {
			streamTable := stream.Tables["data.bar"]
			streamTable.EventType = "stream-type-c"
			stream.Tables["data.bar"] = streamTable
			stream.BatchSize = k8sutil.UInt32ToPointer(uint32(250))
			pg.Spec.Streams[i] = stream
		}
	}

	patchData, err := specPatch(pg.Spec)
	assert.NoError(t, err)

	pgPatched, err := cluster.KubeClient.Postgresqls(namespace).Patch(
		context.TODO(), cluster.Name, types.MergePatchType, patchData, metav1.PatchOptions{}, "spec")
	assert.NoError(t, err)

	cluster.Postgresql.Spec = pgPatched.Spec
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	// compare stream returned from API with expected stream
	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(true).String(),
	}
	streams, err := cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream(appId)
	if match, _ := sameStreams(streams.Items[0].Spec.EventStreams, result.Spec.EventStreams); !match {
		t.Errorf("Malformed FabricEventStream after updating manifest, expected %#v, got %#v", streams.Items[0], result)
	}

	// disable recovery
	for _, stream := range pg.Spec.Streams {
		if stream.ApplicationId == appId {
			stream.EnableRecovery = util.False()
		}
	}
	patchData, err = specPatch(pg.Spec)
	assert.NoError(t, err)

	pgPatched, err = cluster.KubeClient.Postgresqls(namespace).Patch(
		context.TODO(), cluster.Name, types.MergePatchType, patchData, metav1.PatchOptions{}, "spec")
	assert.NoError(t, err)

	cluster.Postgresql.Spec = pgPatched.Spec
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	result = cluster.generateFabricEventStream(appId)
	if match, _ := sameStreams(streams.Items[0].Spec.EventStreams, result.Spec.EventStreams); !match {
		t.Errorf("Malformed FabricEventStream after disabling event recovery, expected %#v, got %#v", streams.Items[0], result)
	}
}
