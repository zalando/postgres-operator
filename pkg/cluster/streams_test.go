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
	v1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1"
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
	fesUser     string = constants.EventStreamSourceSlotPrefix + constants.UserRoleNameSuffix
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
				dbName: dbName + constants.UserRoleNameSuffix,
			},
			Streams: []acidv1.Stream{
				{
					ApplicationId: appId,
					Database:      "foo",
					Tables: map[string]acidv1.StreamTable{
						"data.bar": acidv1.StreamTable{
							EventType:     "stream_type_a",
							IdColumn:      "b_id",
							PayloadColumn: "b_payload",
						},
					},
					Filter: map[string]string{
						"data.bar": "[?(@.source.txId > 500 && @.source.lsn > 123456)]",
					},
					BatchSize: uint32(100),
				},
			},
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	fes = &v1.FabricEventStream{
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
		Spec: v1.FabricEventStreamSpec{
			ApplicationId: appId,
			EventStreams: []v1.EventStream{
				{
					EventStreamFlow: v1.EventStreamFlow{
						PayloadColumn: "b_payload",
						Type:          constants.EventStreamFlowPgGenericType,
					},
					EventStreamSink: v1.EventStreamSink{
						EventType:    "stream_type_a",
						MaxBatchSize: uint32(100),
						Type:         constants.EventStreamSinkNakadiType,
					},
					EventStreamSource: v1.EventStreamSource{
						Filter: "[?(@.source.txId > 500 && @.source.lsn > 123456)]",
						Connection: v1.Connection{
							DBAuth: v1.DBAuth{
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
						EventStreamTable: v1.EventStreamTable{
							IDColumn: "b_id",
							Name:     "bar",
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

	_, err := cluster.createStatefulSet()
	assert.NoError(t, err)

	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream(appId)

	if !reflect.DeepEqual(result, fes) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", fes, result)
	}

	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
	assert.NoError(t, err)

	if !reflect.DeepEqual(streamCRD, fes) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", fes, streamCRD)
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
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	var pgSpec acidv1.PostgresSpec
	pgSpec.Streams = []acidv1.Stream{
		{
			ApplicationId: appId,
			Database:      dbName,
			Tables: map[string]acidv1.StreamTable{
				"data.bar": acidv1.StreamTable{
					EventType:     "stream_type_b",
					IdColumn:      "b_id",
					PayloadColumn: "b_payload",
				},
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
	err = cluster.createOrUpdateStreams()
	assert.NoError(t, err)

	streamCRD, err := cluster.KubeClient.FabricEventStreams(namespace).Get(context.TODO(), fesName, metav1.GetOptions{})
	assert.NoError(t, err)

	result := cluster.generateFabricEventStream(appId)
	if !reflect.DeepEqual(result, streamCRD) {
		t.Errorf("Malformed FabricEventStream, expected %#v, got %#v", streamCRD, result)
	}
}
