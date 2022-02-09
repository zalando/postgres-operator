package cluster

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/postgres-operator/mocks"
	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	fakeacidv1 "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/fake"
	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	"github.com/zalando/postgres-operator/pkg/util/patroni"
	"k8s.io/client-go/kubernetes/fake"
)

var patroniLogger = logrus.New().WithField("test", "patroni")
var acidClientSet = fakeacidv1.NewSimpleClientset()
var clientSet = fake.NewSimpleClientset()

func newMockPod(ip string) *v1.Pod {
	return &v1.Pod{
		Status: v1.PodStatus{
			PodIP: ip,
		},
	}
}

func newFakeK8sSyncClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	return k8sutil.KubernetesClient{
		PodsGetter:         clientSet.CoreV1(),
		PostgresqlsGetter:  acidClientSet.AcidV1(),
		StatefulSetsGetter: clientSet.AppsV1(),
	}, clientSet
}

func newFakeK8sSyncSecretsClient() (k8sutil.KubernetesClient, *fake.Clientset) {
	return k8sutil.KubernetesClient{
		SecretsGetter: clientSet.CoreV1(),
	}, clientSet
}

func TestSyncStatefulSetsAnnotations(t *testing.T) {
	testName := "test syncing statefulsets annotations"
	client, _ := newFakeK8sSyncClient()
	clusterName := "acid-test-cluster"
	namespace := "default"
	inheritedAnnotation := "environment"

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clusterName,
			Namespace:   namespace,
			Annotations: map[string]string{inheritedAnnotation: "test"},
		},
		Spec: acidv1.PostgresSpec{
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

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
					InheritedAnnotations:  []string{inheritedAnnotation},
					PodRoleLabel:          "spilo-role",
					ResourceCheckInterval: time.Duration(3),
					ResourceCheckTimeout:  time.Duration(10),
				},
			},
		}, client, pg, logger, eventRecorder)

	cluster.Name = clusterName
	cluster.Namespace = namespace

	// create a statefulset
	_, err := cluster.createStatefulSet()
	assert.NoError(t, err)

	// patch statefulset and add annotation
	patchData, err := metaAnnotationsPatch(map[string]string{"test-anno": "true"})
	assert.NoError(t, err)

	newSts, err := cluster.KubeClient.StatefulSets(namespace).Patch(
		context.TODO(),
		clusterName,
		types.MergePatchType,
		[]byte(patchData),
		metav1.PatchOptions{},
		"")
	assert.NoError(t, err)

	cluster.Statefulset = newSts

	// first compare running with desired statefulset - they should not match
	// because no inherited annotations or downscaler annotations are configured
	desiredSts, err := cluster.generateStatefulSet(&cluster.Postgresql.Spec)
	assert.NoError(t, err)

	cmp := cluster.compareStatefulSetWith(desiredSts)
	if cmp.match {
		t.Errorf("%s: match between current and desired statefulsets albeit differences: %#v", testName, cmp)
	}

	// now sync statefulset - the diff will trigger a replacement of the statefulset
	cluster.syncStatefulSet()

	// compare again after the SYNC - must be identical to the desired state
	cmp = cluster.compareStatefulSetWith(desiredSts)
	if !cmp.match {
		t.Errorf("%s: current and desired statefulsets are not matching %#v", testName, cmp)
	}

	// check if inherited annotation exists
	if _, exists := desiredSts.Annotations[inheritedAnnotation]; !exists {
		t.Errorf("%s: inherited annotation not found in desired statefulset: %#v", testName, desiredSts.Annotations)
	}
}

func TestCheckAndSetGlobalPostgreSQLConfiguration(t *testing.T) {
	testName := "test config comparison"
	client, _ := newFakeK8sSyncClient()
	clusterName := "acid-test-cluster"
	namespace := "default"

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			Patroni: acidv1.Patroni{
				TTL: 20,
			},
			PostgresqlParam: acidv1.PostgresqlParam{
				Parameters: map[string]string{
					"log_min_duration_statement": "200",
					"max_connections":            "50",
				},
			},
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

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
					PodRoleLabel:          "spilo-role",
					ResourceCheckInterval: time.Duration(3),
					ResourceCheckTimeout:  time.Duration(10),
				},
			},
		}, client, pg, logger, eventRecorder)

	// mocking a config after setConfig is called
	configJson := `{"postgresql": {"parameters": {"log_min_duration_statement": 200, "max_connections": 50}}}, "ttl": 20}`
	r := ioutil.NopCloser(bytes.NewReader([]byte(configJson)))

	response := http.Response{
		StatusCode: 200,
		Body:       r,
	}

	mockClient := mocks.NewMockHTTPClient(ctrl)
	mockClient.EXPECT().Do(gomock.Any()).Return(&response, nil).AnyTimes()

	p := patroni.New(patroniLogger, mockClient)
	cluster.patroni = p
	mockPod := newMockPod("192.168.100.1")

	// simulate existing config that differs from cluster.Spec
	tests := []struct {
		subtest       string
		patroni       acidv1.Patroni
		pgParams      map[string]string
		restartMaster bool
	}{
		{
			subtest: "Patroni and Postgresql.Parameters differ - restart replica first",
			patroni: acidv1.Patroni{
				TTL: 30, // desired 20
			},
			pgParams: map[string]string{
				"log_min_duration_statement": "500", // desired 200
				"max_connections":            "100", // desired 50
			},
			restartMaster: false,
		},
		{
			subtest: "multiple Postgresql.Parameters differ - restart replica first",
			patroni: acidv1.Patroni{
				TTL: 20,
			},
			pgParams: map[string]string{
				"log_min_duration_statement": "500", // desired 200
				"max_connections":            "100", // desired 50
			},
			restartMaster: false,
		},
		{
			subtest: "desired max_connections bigger - restart replica first",
			patroni: acidv1.Patroni{
				TTL: 20,
			},
			pgParams: map[string]string{
				"log_min_duration_statement": "200",
				"max_connections":            "30", // desired 50
			},
			restartMaster: false,
		},
		{
			subtest: "desired max_connections smaller - restart master first",
			patroni: acidv1.Patroni{
				TTL: 20,
			},
			pgParams: map[string]string{
				"log_min_duration_statement": "200",
				"max_connections":            "100", // desired 50
			},
			restartMaster: true,
		},
	}

	for _, tt := range tests {
		requireMasterRestart, err := cluster.checkAndSetGlobalPostgreSQLConfiguration(mockPod, tt.patroni, tt.pgParams)
		assert.NoError(t, err)
		if requireMasterRestart != tt.restartMaster {
			t.Errorf("%s - %s: unexpect master restart strategy, got %v, expected %v", testName, tt.subtest, requireMasterRestart, tt.restartMaster)
		}
	}
}

func TestUpdateSecret(t *testing.T) {
	testName := "test syncing secrets"
	client, _ := newFakeK8sSyncSecretsClient()

	clusterName := "acid-test-cluster"
	namespace := "default"
	username := "foo"
	secretTemplate := config.StringTemplate("{username}.{cluster}.credentials")
	rotationUsers := make(spec.PgUserMap)
	retentionUsers := make([]string, 0)
	yesterday := time.Now().AddDate(0, 0, -1)

	// new cluster with pvc storage resize mode and configured labels
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Auth: config.Auth{
					SecretNameTemplate:            secretTemplate,
					EnablePasswordRotation:        true,
					PasswordRotationInterval:      1,
					PasswordRotationUserRetention: 3,
				},
				Resources: config.Resources{
					ClusterLabels:    map[string]string{"application": "spilo"},
					ClusterNameLabel: "cluster-name",
				},
			},
		}, client, acidv1.Postgresql{}, logger, eventRecorder)

	cluster.Name = clusterName
	cluster.Namespace = namespace
	cluster.pgUsers = map[string]spec.PgUser{}
	cluster.Spec.Users = map[string]acidv1.UserFlags{username: {}}
	cluster.initRobotUsers()

	// create a secret for user foo
	cluster.syncSecrets()

	secret, err := cluster.KubeClient.Secrets(namespace).Get(context.TODO(), secretTemplate.Format("username", username, "cluster", clusterName), metav1.GetOptions{})
	assert.NoError(t, err)
	generatedSecret := cluster.Secrets[secret.UID]

	// now update the secret setting next rotation date (yesterday + interval)
	cluster.updateSecret(username, generatedSecret, &rotationUsers, &retentionUsers, yesterday)
	updatedSecret, err := cluster.KubeClient.Secrets(namespace).Get(context.TODO(), secretTemplate.Format("username", username, "cluster", clusterName), metav1.GetOptions{})
	assert.NoError(t, err)

	nextRotation := string(updatedSecret.Data["nextRotation"])
	_, nextRotationDate := cluster.getNextRotationDate(yesterday)
	if nextRotation != nextRotationDate {
		t.Errorf("%s: updated secret does not contain correct rotation date: expected %s, got %s", testName, nextRotationDate, nextRotation)
	}

	// update secret again but use current time to trigger rotation
	cluster.updateSecret(username, generatedSecret, &rotationUsers, &retentionUsers, time.Now())
	updatedSecret, err = cluster.KubeClient.Secrets(namespace).Get(context.TODO(), secretTemplate.Format("username", username, "cluster", clusterName), metav1.GetOptions{})
	assert.NoError(t, err)

	if len(rotationUsers) != 1 && len(retentionUsers) != 1 {
		t.Errorf("%s: unexpected number of users to rotate - expected only foo, found %d", testName, len(rotationUsers))
	}

	secretUsername := string(updatedSecret.Data["username"])
	rotatedUsername := username + time.Now().Format("060102")
	if secretUsername != rotatedUsername {
		t.Errorf("%s: updated secret does not contain correct username: expected %s, got %s", testName, rotatedUsername, secretUsername)
	}
}
