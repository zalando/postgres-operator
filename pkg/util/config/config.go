package config

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

type TPR struct {
	ReadyWaitInterval time.Duration `name:"ready_wait_interval" default:"4s"`
	ReadyWaitTimeout  time.Duration `name:"ready_wait_timeout" default:"30s"`
	ResyncPeriod      time.Duration `name:"resync_period" default:"5m"`
}

type Resources struct {
	ResyncPeriodPod        time.Duration     `name:"resync_period_pod" default:"5m"`
	ResourceCheckInterval  time.Duration     `name:"resource_check_interval" default:"3s"`
	ResourceCheckTimeout   time.Duration     `name:"resource_check_timeout" default:"10m"`
	PodLabelWaitTimeout    time.Duration     `name:"pod_label_wait_timeout" default:"10m"`
	PodDeletionWaitTimeout time.Duration     `name:"pod_deletion_wait_timeout" default:"10m"`
	ClusterLabels          map[string]string `name:"cluster_labels" default:"application:spilo"`
	ClusterNameLabel       string            `name:"cluster_name_label" default:"cluster-name"`
	PodRoleLabel           string            `name:"pod_role_label" default:"spilo-role"`
	DefaultCpuRequest      string            `name:"default_cpu_request" default:"100m"`
	DefaultMemoryRequest   string            `name:"default_memory_request" default:"100Mi"`
	DefaultCpuLimit        string            `name:"default_cpu_limit" default:"3"`
	DefaultMemoryLimit     string            `name:"default_memory_limit" default:"1Gi"`
}

type Auth struct {
	PamRoleName                   string              `name:"pam_rol_name" default:"zalandos"`
	PamConfiguration              string              `name:"pam_configuration" default:"https://info.example.com/oauth2/tokeninfo?access_token= uid realm=/employees"`
	TeamsAPIUrl                   string              `name:"teams_api_url" default:"https://teams.example.com/api/"`
	OAuthTokenSecretName          spec.NamespacedName `name:"oauth_token_secret_name" default:"postgresql-operator"`
	InfrastructureRolesSecretName spec.NamespacedName `name:"infrastructure_roles_secret_name"`
	SuperUsername                 string              `name:"super_username" default:"postgres"`
	ReplicationUsername           string              `name:"replication_username" default:"replication"`
}

type Config struct {
	TPR
	Resources
	Auth
	Namespace          string `name:"namespace"`
	EtcdHost           string `name:"etcd_host" default:"etcd-client.default.svc.cluster.local:2379"`
	DockerImage        string `name:"docker_image" default:"registry.opensource.zalan.do/acid/spiloprivate-9.6:1.2-p4"`
	ServiceAccountName string `name:"service_account_name" default:"operator"`
	DbHostedZone       string `name:"db_hosted_zone" default:"db.example.com"`
	EtcdScope          string `name:"etcd_scope" default:"service"`
	WALES3Bucket       string `name:"wal_s3_bucket"`
	KubeIAMRole        string `name:"kube_iam_role"`
	DebugLogging       bool   `name:"debug_logging" default:"false"`
	EnableDBAccess     bool   `name:"enable_database_access" default:"true"`
	EnableTeamsAPI     bool   `name:"enable_teams_api" default:"true"`
	DNSNameFormat      string `name:"dns_name_format" default:"%s.%s.%s"`
	Workers            uint32 `name:"workers" default:"4"`
}

func (c Config) MustMarshal() string {
	b, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		panic(err)
	}

	return string(b)
}

func NewFromMap(m map[string]string) *Config {
	cfg := Config{}
	fields, _ := structFields(&cfg)

	for _, structField := range fields {
		key := strings.ToLower(structField.Name)
		value, ok := m[key]
		if !ok && structField.Default != "" {
			value = structField.Default
		}

		err := processField(value, structField.Field)
		if err != nil {
			panic(err)
		}
	}

	return &cfg
}

func Copy(c *Config) Config {
	cfg := *c

	cfg.ClusterLabels = make(map[string]string, len(c.ClusterLabels))
	for k, v := range c.ClusterLabels {
		cfg.ClusterLabels[k] = v
	}

	return cfg
}
