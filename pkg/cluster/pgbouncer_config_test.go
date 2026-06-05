package cluster

import (
	"context"
	"strings"
	"testing"

	acidv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/zalando/postgres-operator/pkg/util"
	"github.com/zalando/postgres-operator/pkg/util/config"
	"github.com/zalando/postgres-operator/pkg/util/k8sutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newGenerateConfigCluster() *Cluster {
	maxDB := int32(60)
	instances := int32(2)
	cluster := New(
		Config{OpConfig: config.Config{
			ConnectionPooler: config.ConnectionPooler{
				User:              "pooler",
				Schema:            "pooler",
				Mode:              "transaction",
				MaxDBConnections:  &maxDB,
				NumberOfInstances: &instances,
				GenerateConfig:    true,
				AuthType:          "scram-sha-256",
				ConfigPath:        "/etc/pgbouncer/pgbouncer.ini",
				Args:              []string{"/etc/pgbouncer/pgbouncer.ini"},
			},
			Resources: config.Resources{
				EnableOwnerReferences: util.True(),
			},
		}},
		k8sutil.NewMockKubernetesClient(), acidv1.Postgresql{}, logger, eventRecorder)
	cluster.Name = "acid-test"
	cluster.Namespace = "default"
	cluster.Spec = acidv1.PostgresSpec{ConnectionPooler: &acidv1.ConnectionPooler{}}
	return cluster
}

func TestGeneratePgBouncerIni(t *testing.T) {
	cluster := newGenerateConfigCluster()

	ini, err := cluster.generatePgBouncerIni(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		"[databases]",
		"[pgbouncer]",
		"pool_mode = transaction",
		"auth_type = scram-sha-256",
		"auth_file = /etc/pgbouncer/userlist.txt",
		"auth_query = SELECT * FROM pooler.user_lookup($1)",
		"server_tls_sslmode = require",
		"default_pool_size = 15",
		"max_db_connections = 30",
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("rendered ini missing %q\n---\n%s", want, ini)
		}
	}

	if strings.Contains(ini, "client_tls_cert_file") {
		t.Errorf("did not expect client_tls_cert_file without spec.TLS\n%s", ini)
	}
}

func TestGeneratePgBouncerIniWithTLS(t *testing.T) {
	cluster := newGenerateConfigCluster()
	cluster.Spec.TLS = &acidv1.TLSDescription{SecretName: "pg-tls"}

	ini, err := cluster.generatePgBouncerIni(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"client_tls_sslmode = require",
		"client_tls_key_file = /tls/tls.key",
		"client_tls_cert_file = /tls/tls.crt",
	} {
		if !strings.Contains(ini, want) {
			t.Errorf("rendered ini missing %q\n---\n%s", want, ini)
		}
	}
}

func TestConnectionPoolerConfigChecksumStability(t *testing.T) {
	cluster := newGenerateConfigCluster()

	sum1, err := cluster.connectionPoolerConfigChecksum(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum2, err := cluster.connectionPoolerConfigChecksum(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum1 != sum2 {
		t.Errorf("checksum not stable: %q != %q", sum1, sum2)
	}

	cluster.OpConfig.ConnectionPooler.AuthType = "md5"
	sum3, err := cluster.connectionPoolerConfigChecksum(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum1 == sum3 {
		t.Errorf("checksum should change when config changes")
	}
}

func TestGenerateConnectionPoolerConfigMap(t *testing.T) {
	cluster := newGenerateConfigCluster()

	cm, err := cluster.generateConnectionPoolerConfigMap(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cm.Name != cluster.connectionPoolerName(Master)+"-config" {
		t.Errorf("unexpected config map name %q", cm.Name)
	}
	if _, ok := cm.Data["pgbouncer.ini"]; !ok {
		t.Errorf("config map missing pgbouncer.ini key, got %#v", cm.Data)
	}
	if len(cm.OwnerReferences) == 0 {
		t.Errorf("config map should have owner references")
	}
}

func findVolumeMount(mounts []v1.VolumeMount, path string) *v1.VolumeMount {
	for i := range mounts {
		if mounts[i].MountPath == path {
			return &mounts[i]
		}
	}
	return nil
}

func TestPoolerPodTemplateGeneratedConfigOn(t *testing.T) {
	cluster := newGenerateConfigCluster()

	tmpl, err := cluster.generateConnectionPoolerPodTemplate(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container := tmpl.Spec.Containers[0]

	mount := findVolumeMount(container.VolumeMounts, "/etc/pgbouncer/pgbouncer.ini")
	if mount == nil {
		t.Fatalf("expected a volume mount at /etc/pgbouncer/pgbouncer.ini")
	}
	if mount.SubPath != "pgbouncer.ini" {
		t.Errorf("expected subPath pgbouncer.ini, got %q", mount.SubPath)
	}
	if len(container.Args) != 1 || container.Args[0] != "/etc/pgbouncer/pgbouncer.ini" {
		t.Errorf("expected args [/etc/pgbouncer/pgbouncer.ini], got %#v", container.Args)
	}
	if _, ok := tmpl.Annotations[poolerConfigChecksumAnnotation]; !ok {
		t.Errorf("expected checksum annotation on pod template")
	}
}

func TestPoolerPodTemplateGeneratedConfigOff(t *testing.T) {
	cluster := newGenerateConfigCluster()
	cluster.OpConfig.ConnectionPooler.GenerateConfig = false

	tmpl, err := cluster.generateConnectionPoolerPodTemplate(Master)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	container := tmpl.Spec.Containers[0]

	if findVolumeMount(container.VolumeMounts, "/etc/pgbouncer/pgbouncer.ini") != nil {
		t.Errorf("did not expect config mount when GenerateConfig is off")
	}
	if len(container.Args) != 0 {
		t.Errorf("did not expect args when GenerateConfig is off, got %#v", container.Args)
	}
	if _, ok := tmpl.Annotations[poolerConfigChecksumAnnotation]; ok {
		t.Errorf("did not expect checksum annotation when GenerateConfig is off")
	}
}

func TestSyncConnectionPoolerConfigMap(t *testing.T) {
	client, _ := newFakeK8sPoolerTestClient()
	maxDB := int32(60)
	instances := int32(2)
	pg := acidv1.Postgresql{
		ObjectMeta: metav1.ObjectMeta{Name: "acid-test", Namespace: "default"},
		Spec: acidv1.PostgresSpec{
			EnableConnectionPooler: boolToPointer(true),
			ConnectionPooler:       &acidv1.ConnectionPooler{},
		},
	}
	cluster := New(
		Config{OpConfig: config.Config{
			ConnectionPooler: config.ConnectionPooler{
				User: "pooler", Schema: "pooler", Mode: "transaction",
				MaxDBConnections: &maxDB, NumberOfInstances: &instances,
				GenerateConfig: true, AuthType: "scram-sha-256",
				ConfigPath: "/etc/pgbouncer/pgbouncer.ini",
				Args:       []string{"/etc/pgbouncer/pgbouncer.ini"},
			},
		}},
		client, pg, logger, eventRecorder)
	cluster.Name = "acid-test"
	cluster.Namespace = "default"
	cluster.Spec = pg.Spec
	cluster.ConnectionPooler = map[PostgresRole]*ConnectionPoolerObjects{
		Master: {Name: cluster.connectionPoolerName(Master), Namespace: "default", Role: Master},
	}

	if err := cluster.syncConnectionPoolerConfigMap(Master); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	name := cluster.connectionPoolerName(Master) + "-config"
	cm, err := client.ConfigMaps("default").Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("config map not created: %v", err)
	}
	if _, ok := cm.Data["pgbouncer.ini"]; !ok {
		t.Errorf("config map missing pgbouncer.ini")
	}

	if err := cluster.syncConnectionPoolerConfigMap(Master); err != nil {
		t.Fatalf("unexpected error on resync: %v", err)
	}
}
