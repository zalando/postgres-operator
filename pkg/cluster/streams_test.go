package cluster

import (
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
)

var (
	clusterName string = "acid-stream-cluster"
	namespace   string = "default"
	appId       string = "test-app"
	dbName      string = "foo"
	fesUser     string = fmt.Sprintf("%s%s", constants.EventStreamSourceSlotPrefix, constants.UserRoleNameSuffix)
	slotName    string = fmt.Sprintf("%s_%s_%s", constants.EventStreamSourceSlotPrefix, dbName, strings.Replace(appId, "-", "_", -1))

	zalandoClientSet = fakezalandov1.NewSimpleClientset()

	client = k8sutil.KubernetesClient{
		FabricEventStreamsGetter: zalandoClientSet.ZalandoV1(),
		PostgresqlsGetter:        zalandoClientSet.AcidV1(),
		PodsGetter:               clientSet.CoreV1(),
		StatefulSetsGetter:       clientSet.AppsV1(),
	}

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
						"data.bar": {
							EventType:     "stream-type-a",
							IdColumn:      k8sutil.StringToPointer("b_id"),
							PayloadColumn: k8sutil.StringToPointer("b_payload"),
						},
						"data.foobar": {
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
			Labels: map[string]string{
				"application":  "spilo",
				"cluster-name": fmt.Sprintf("%s-2", clusterName),
				"team":         "acid",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
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
				{
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
				{
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

	cluster = New(
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
)

func TestGatherApplicationIds(t *testing.T) {
	testAppIds := []string{appId}
	appIds := getDistinctApplicationIds(pg.Spec.Streams)

	if !util.IsEqualIgnoreOrder(testAppIds, appIds) {
		t.Errorf("list of applicationIds does not match, expected %#v, got %#v", testAppIds, appIds)
	}
}

func TestHasSlotsInSync(t *testing.T) {
	cluster.Name = clusterName
	cluster.Namespace = namespace

	appId2 := fmt.Sprintf("%s-2", appId)
	dbNotExists := "dbnotexists"
	slotNotExists := fmt.Sprintf("%s_%s_%s", constants.EventStreamSourceSlotPrefix, dbNotExists, strings.Replace(appId, "-", "_", -1))
	slotNotExistsAppId2 := fmt.Sprintf("%s_%s_%s", constants.EventStreamSourceSlotPrefix, dbNotExists, strings.Replace(appId2, "-", "_", -1))

	tests := []struct {
		subTest       string
		applicationId string
		expectedSlots map[string]map[string]zalandov1.Slot
		actualSlots   map[string]map[string]string
		slotsInSync   bool
	}{
		{
			subTest:       fmt.Sprintf("slots in sync for applicationId %s", appId),
			applicationId: appId,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbName: {
					slotName: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{
				slotName: {
					"databases": dbName,
					"plugin":    constants.EventStreamSourcePluginType,
					"type":      "logical",
				},
			},
			slotsInSync: true,
		}, {
			subTest:       fmt.Sprintf("slots empty for applicationId %s after create or update of publication failed", appId),
			applicationId: appId,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbNotExists: {
					slotNotExists: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{},
			slotsInSync: false,
		}, {
			subTest:       fmt.Sprintf("slot with empty definition for applicationId %s after publication git deleted", appId),
			applicationId: appId,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbNotExists: {
					slotNotExists: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{
				slotName: nil,
			},
			slotsInSync: false,
		}, {
			subTest:       fmt.Sprintf("one slot not in sync for applicationId %s because database does not exist", appId),
			applicationId: appId,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbName: {
					slotName: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
				dbNotExists: {
					slotNotExists: zalandov1.Slot{
						Slot: map[string]string{
							"databases": "dbnotexists",
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test2": {
								EventType: "stream-type-b",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{
				slotName: {
					"databases": dbName,
					"plugin":    constants.EventStreamSourcePluginType,
					"type":      "logical",
				},
			},
			slotsInSync: false,
		}, {
			subTest:       fmt.Sprintf("slots in sync for applicationId %s, but not for %s - checking %s should return true", appId, appId2, appId),
			applicationId: appId,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbName: {
					slotName: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
				dbNotExists: {
					slotNotExistsAppId2: zalandov1.Slot{
						Slot: map[string]string{
							"databases": "dbnotexists",
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test2": {
								EventType: "stream-type-b",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{
				slotName: {
					"databases": dbName,
					"plugin":    constants.EventStreamSourcePluginType,
					"type":      "logical",
				},
			},
			slotsInSync: true,
		}, {
			subTest:       fmt.Sprintf("slots in sync for applicationId %s, but not for %s - checking %s should return false", appId, appId2, appId2),
			applicationId: appId2,
			expectedSlots: map[string]map[string]zalandov1.Slot{
				dbName: {
					slotName: zalandov1.Slot{
						Slot: map[string]string{
							"databases": dbName,
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test1": {
								EventType: "stream-type-a",
							},
						},
					},
				},
				dbNotExists: {
					slotNotExistsAppId2: zalandov1.Slot{
						Slot: map[string]string{
							"databases": "dbnotexists",
							"plugin":    constants.EventStreamSourcePluginType,
							"type":      "logical",
						},
						Publication: map[string]acidv1.StreamTable{
							"test2": {
								EventType: "stream-type-b",
							},
						},
					},
				},
			},
			actualSlots: map[string]map[string]string{
				slotName: {
					"databases": dbName,
					"plugin":    constants.EventStreamSourcePluginType,
					"type":      "logical",
				},
			},
			slotsInSync: false,
		},
	}

	for _, tt := range tests {
		result := hasSlotsInSync(tt.applicationId, tt.expectedSlots, tt.actualSlots)
		if result != tt.slotsInSync {
			t.Errorf("%s: unexpected result for slot test of applicationId: %v, expected slots %#v, actual slots %#v", tt.subTest, tt.applicationId, tt.expectedSlots, tt.actualSlots)
		}
	}
}

func TestGenerateFabricEventStream(t *testing.T) {
	cluster.Name = clusterName
	cluster.Namespace = namespace

	// create the streams
	err := cluster.syncStream(appId)
	assert.NoError(t, err)

	// compare generated stream with expected stream
	result := cluster.generateFabricEventStream(appId)
	if match, _ := cluster.compareStreams(result, fes); !match {
		t.Errorf("malformed FabricEventStream, expected %#v, got %#v", fes, result)
	}

	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(true).String(),
	}
	streams, err := cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	assert.Equalf(t, 1, len(streams.Items), "unexpected number of streams found: got %d, but expected only one", len(streams.Items))

	// compare stream returned from API with expected stream
	if match, _ := cluster.compareStreams(&streams.Items[0], fes); !match {
		t.Errorf("malformed FabricEventStream returned from API, expected %#v, got %#v", fes, streams.Items[0])
	}

	// sync streams once again
	err = cluster.syncStream(appId)
	assert.NoError(t, err)

	streams, err = cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	assert.Equalf(t, 1, len(streams.Items), "unexpected number of streams found: got %d, but expected only one", len(streams.Items))

	// compare stream resturned from API with generated stream
	if match, _ := cluster.compareStreams(&streams.Items[0], result); !match {
		t.Errorf("returned FabricEventStream differs from generated one, expected %#v, got %#v", result, streams.Items[0])
	}
}

func newFabricEventStream(streams []zalandov1.EventStream, annotations map[string]string) *zalandov1.FabricEventStream {
	return &zalandov1.FabricEventStream{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-12345", clusterName),
			Annotations: annotations,
		},
		Spec: zalandov1.FabricEventStreamSpec{
			ApplicationId: appId,
			EventStreams:  streams,
		},
	}
}

func TestSyncStreams(t *testing.T) {
	pg.Name = fmt.Sprintf("%s-2", pg.Name)
	var cluster = New(
		Config{
			OpConfig: config.Config{
				PodManagementPolicy: "ordered_ready",
				Resources: config.Resources{
					ClusterLabels:         map[string]string{"application": "spilo"},
					ClusterNameLabel:      "cluster-name",
					DefaultCPURequest:     "300m",
					DefaultCPULimit:       "300m",
					DefaultMemoryRequest:  "300Mi",
					DefaultMemoryLimit:    "300Mi",
					EnableOwnerReferences: util.True(),
					PodRoleLabel:          "spilo-role",
				},
			},
		}, client, pg, logger, eventRecorder)

	_, err := cluster.KubeClient.Postgresqls(namespace).Create(
		context.TODO(), &pg, metav1.CreateOptions{})
	assert.NoError(t, err)

	// create the stream
	err = cluster.syncStream(appId)
	assert.NoError(t, err)

	// create a second stream with same spec but with different name
	createdStream, err := cluster.KubeClient.FabricEventStreams(namespace).Create(
		context.TODO(), fes, metav1.CreateOptions{})
	assert.NoError(t, err)
	assert.Equal(t, createdStream.Spec.ApplicationId, appId)

	// check that two streams exist
	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(true).String(),
	}
	streams, err := cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	assert.Equalf(t, 2, len(streams.Items), "unexpected number of streams found: got %d, but expected only 2", len(streams.Items))

	// sync the stream which should remove the redundant stream
	err = cluster.syncStream(appId)
	assert.NoError(t, err)

	// check that only one stream remains after sync
	streams, err = cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)
	assert.Equalf(t, 1, len(streams.Items), "unexpected number of streams found: got %d, but expected only 1", len(streams.Items))

	// check owner references
	if !reflect.DeepEqual(streams.Items[0].OwnerReferences, cluster.ownerReferences()) {
		t.Errorf("unexpected owner references, expected %#v, got %#v", cluster.ownerReferences(), streams.Items[0].OwnerReferences)
	}
}

func TestSameStreams(t *testing.T) {
	testName := "TestSameStreams"
	annotationsA := map[string]string{"owned-by": "acid"}
	annotationsB := map[string]string{"owned-by": "foo"}

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
		streamsA *zalandov1.FabricEventStream
		streamsB *zalandov1.FabricEventStream
		match    bool
		reason   string
	}{
		{
			subTest:  "identical streams",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, annotationsA),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, annotationsA),
			match:    true,
			reason:   "",
		},
		{
			subTest:  "same streams different order",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, nil),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream2, stream1}, nil),
			match:    true,
			reason:   "",
		},
		{
			subTest:  "same streams different order",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream1}, nil),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, nil),
			match:    false,
			reason:   "number of defined streams is different",
		},
		{
			subTest:  "different number of streams",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream1}, nil),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, nil),
			match:    false,
			reason:   "number of defined streams is different",
		},
		{
			subTest:  "event stream specs differ",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream1, stream2}, nil),
			streamsB: fes,
			match:    false,
			reason:   "number of defined streams is different",
		},
		{
			subTest:  "event stream recovery specs differ",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream2}, nil),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream3}, nil),
			match:    false,
			reason:   "event stream specs differ",
		},
		{
			subTest:  "event stream annotations differ",
			streamsA: newFabricEventStream([]zalandov1.EventStream{stream2}, annotationsA),
			streamsB: newFabricEventStream([]zalandov1.EventStream{stream3}, annotationsB),
			match:    false,
			reason:   "event stream specs differ",
		},
	}

	for _, tt := range tests {
		streamsMatch, matchReason := cluster.compareStreams(tt.streamsA, tt.streamsB)
		if streamsMatch != tt.match {
			t.Errorf("%s %s: unexpected match result when comparing streams: got %s, epxected %s",
				testName, tt.subTest, matchReason, tt.reason)
		}
	}
}

func TestUpdateStreams(t *testing.T) {
	pg.Name = fmt.Sprintf("%s-3", pg.Name)
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

	// create the stream
	err = cluster.syncStream(appId)
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

	// compare stream returned from API with expected stream
	listOptions := metav1.ListOptions{
		LabelSelector: cluster.labelsSet(true).String(),
	}
	streams := patchPostgresqlStreams(t, cluster, &pg.Spec, listOptions)
	result := cluster.generateFabricEventStream(appId)
	if match, _ := cluster.compareStreams(&streams.Items[0], result); !match {
		t.Errorf("Malformed FabricEventStream after updating manifest, expected %#v, got %#v", streams.Items[0], result)
	}

	// disable recovery
	for idx, stream := range pg.Spec.Streams {
		if stream.ApplicationId == appId {
			stream.EnableRecovery = util.False()
			pg.Spec.Streams[idx] = stream
		}
	}

	streams = patchPostgresqlStreams(t, cluster, &pg.Spec, listOptions)
	result = cluster.generateFabricEventStream(appId)
	if match, _ := cluster.compareStreams(&streams.Items[0], result); !match {
		t.Errorf("Malformed FabricEventStream after disabling event recovery, expected %#v, got %#v", streams.Items[0], result)
	}

	mockClient := k8sutil.NewMockKubernetesClient()
	cluster.KubeClient.CustomResourceDefinitionsGetter = mockClient.CustomResourceDefinitionsGetter

	// remove streams from manifest
	pg.Spec.Streams = nil
	pgUpdated, err := cluster.KubeClient.Postgresqls(namespace).Update(
		context.TODO(), &pg, metav1.UpdateOptions{})
	assert.NoError(t, err)

	appIds := getDistinctApplicationIds(pgUpdated.Spec.Streams)
	cluster.cleanupRemovedStreams(appIds)

	streams, err = cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	if len(streams.Items) > 0 || err != nil {
		t.Errorf("stream resource has not been removed or unexpected error %v", err)
	}
}

func patchPostgresqlStreams(t *testing.T, cluster *Cluster, pgSpec *acidv1.PostgresSpec, listOptions metav1.ListOptions) (streams *zalandov1.FabricEventStreamList) {
	patchData, err := specPatch(pgSpec)
	assert.NoError(t, err)

	pgPatched, err := cluster.KubeClient.Postgresqls(namespace).Patch(
		context.TODO(), cluster.Name, types.MergePatchType, patchData, metav1.PatchOptions{}, "spec")
	assert.NoError(t, err)

	cluster.Postgresql.Spec = pgPatched.Spec
	err = cluster.syncStream(appId)
	assert.NoError(t, err)

	streams, err = cluster.KubeClient.FabricEventStreams(namespace).List(context.TODO(), listOptions)
	assert.NoError(t, err)

	return streams
}

func TestSlotNameWithinMaxLength(t *testing.T) {
	dbName := "testdb"
	appId := "test-app"
	expected := constants.EventStreamSourceSlotPrefix + "_testdb_test_app"
	result := getSlotName(dbName, appId)
	assert.Equal(t, expected, result)
}

func TestSlotNameExceedsMaxLength(t *testing.T) {
	dbName := "testdb"
	appId := "this-is-a-very-long-application-id-that-will-exceed-the-maximum-length"
	expected := constants.EventStreamSourceSlotPrefix + "_5a300d179c894b672b35bac212eab875d4c4145a"
	result := getSlotName(dbName, appId)
	assert.Equal(t, expected, result)
}

func TestSlotNameWithHyphens(t *testing.T) {
	dbName := "testdb"
	appId := "test-app-with-hyphens"
	expected := constants.EventStreamSourceSlotPrefix + "_testdb_test_app_with_hyphens"
	result := getSlotName(dbName, appId)
	assert.Equal(t, expected, result)
}
