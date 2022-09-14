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
		subtest        string
		patroni        acidv1.Patroni
		pgParams       map[string]string
		restartPrimary bool
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
			restartPrimary: false,
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
			restartPrimary: false,
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
			restartPrimary: false,
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
			restartPrimary: true,
		},
	}

	for _, tt := range tests {
		configPatched, requirePrimaryRestart, err := cluster.checkAndSetGlobalPostgreSQLConfiguration(mockPod, tt.patroni, cluster.Spec.Patroni, tt.pgParams, cluster.Spec.Parameters)
		assert.NoError(t, err)
		if configPatched != true {
			t.Errorf("%s - %s: expected config update did not happen", testName, tt.subtest)
		}
		if requirePrimaryRestart != tt.restartPrimary {
			t.Errorf("%s - %s: wrong master restart strategy, got restart %v, expected restart %v", testName, tt.subtest, requirePrimaryRestart, tt.restartPrimary)
		}
	}
}

func TestUpdateSecret(t *testing.T) {
	testName := "test syncing secrets"
	client, _ := newFakeK8sSyncSecretsClient()

	clusterName := "acid-test-cluster"
	namespace := "default"
	dbname := "app"
	dbowner := "appowner"
	secretTemplate := config.StringTemplate("{username}.{cluster}.credentials")
	rotationUsers := make(spec.PgUserMap)
	retentionUsers := make([]string, 0)

	// define manifest users and enable rotation for dbowner
	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: acidv1.PostgresSpec{
			Databases:                      map[string]string{dbname: dbowner},
			Users:                          map[string]acidv1.UserFlags{"foo": {}, dbowner: {}},
			UsersWithInPlaceSecretRotation: []string{dbowner},
			Streams: []acidv1.Stream{
				{
					ApplicationId: appId,
					Database:      dbname,
					Tables: map[string]acidv1.StreamTable{
						"data.foo": acidv1.StreamTable{
							EventType: "stream-type-b",
						},
					},
				},
			},
			Volume: acidv1.Volume{
				Size: "1Gi",
			},
		},
	}

	// new cluster with enabled password rotation
	var cluster = New(
		Config{
			OpConfig: config.Config{
				Auth: config.Auth{
					SuperUsername:                 "postgres",
					ReplicationUsername:           "standby",
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
		}, client, pg, logger, eventRecorder)

	cluster.Name = clusterName
	cluster.Namespace = namespace
	cluster.pgUsers = map[string]spec.PgUser{}

	// init all users
	cluster.initUsers()
	// create secrets
	cluster.syncSecrets()
	// initialize rotation with current time
	cluster.syncSecrets()

	dayAfterTomorrow := time.Now().AddDate(0, 0, 2)

	allUsers := make(map[string]spec.PgUser)
	for userName, pgUser := range cluster.pgUsers {
		allUsers[userName] = pgUser
	}
	for _, systemUser := range cluster.systemUsers {
		allUsers[systemUser.Name] = systemUser
	}

	for username, pgUser := range allUsers {
		// first, get the secret
		secretName := cluster.credentialSecretName(username)
		secret, err := cluster.KubeClient.Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
		assert.NoError(t, err)
		secretPassword := string(secret.Data["password"])

		// now update the secret setting a next rotation date (tomorrow + interval)
		cluster.updateSecret(username, secret, &rotationUsers, &retentionUsers, dayAfterTomorrow)
		updatedSecret, err := cluster.KubeClient.Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
		assert.NoError(t, err)

		// check that passwords are different
		rotatedPassword := string(updatedSecret.Data["password"])
		if secretPassword == rotatedPassword {
			// passwords for system users should not have been rotated
			if pgUser.Origin != spec.RoleOriginManifest {
				continue
			}
			t.Errorf("%s: password unchanged in updated secret for %s", testName, username)
		}

		// check that next rotation date is tomorrow + interval, not date in secret + interval
		nextRotation := string(updatedSecret.Data["nextRotation"])
		_, nextRotationDate := cluster.getNextRotationDate(dayAfterTomorrow)
		if nextRotation != nextRotationDate {
			t.Errorf("%s: updated secret of %s does not contain correct rotation date: expected %s, got %s", testName, username, nextRotationDate, nextRotation)
		}

		// compare username, when it's dbowner they should be equal because of UsersWithInPlaceSecretRotation
		secretUsername := string(updatedSecret.Data["username"])
		if pgUser.IsDbOwner {
			if secretUsername != username {
				t.Errorf("%s: username differs in updated secret: expected %s, got %s", testName, username, secretUsername)
			}
		} else {
			rotatedUsername := username + dayAfterTomorrow.Format("060102")
			if secretUsername != rotatedUsername {
				t.Errorf("%s: updated secret does not contain correct username: expected %s, got %s", testName, rotatedUsername, secretUsername)
			}

			if len(rotationUsers) != 1 && len(retentionUsers) != 1 {
				t.Errorf("%s: unexpected number of users to rotate - expected only %s, found %d", testName, username, len(rotationUsers))
			}
		}
	}
}
