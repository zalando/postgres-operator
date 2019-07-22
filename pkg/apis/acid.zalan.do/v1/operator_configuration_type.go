package v1

import (
	"github.com/zalando/postgres-operator/pkg/util/config"

	"time"

	"github.com/zalando/postgres-operator/pkg/spec"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:onlyVerbs=get
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OperatorConfiguration defines the specification for the OperatorConfiguration.
type OperatorConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Configuration OperatorConfigurationData `json:"configuration"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OperatorConfigurationList is used in the k8s API calls
type OperatorConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []OperatorConfiguration `json:"items"`
}

// PostgresUsersConfiguration defines the system users of Postgres.
type PostgresUsersConfiguration struct {
	SuperUsername       string `json:"super_username,omitempty"`
	ReplicationUsername string `json:"replication_username,omitempty"`
}

// KubernetesMetaConfiguration defines k8s conf required for all Postgres clusters and the operator itself
type KubernetesMetaConfiguration struct {
	PodServiceAccountName string `json:"pod_service_account_name,omitempty"`
	// TODO: change it to the proper json
	PodServiceAccountDefinition            string                `json:"pod_service_account_definition,omitempty"`
	PodServiceAccountRoleBindingDefinition string                `json:"pod_service_account_role_binding_definition,omitempty"`
	PodTerminateGracePeriod                Duration              `json:"pod_terminate_grace_period,omitempty"`
	SpiloPrivileged                        bool                  `json:"spilo_privileged,omitemty"`
	SpiloFSGroup                           *int64                `json:"spilo_fsgroup,omitempty"`
	WatchedNamespace                       string                `json:"watched_namespace,omitempty"`
	PDBNameFormat                          config.StringTemplate `json:"pdb_name_format,omitempty"`
	EnablePodDisruptionBudget              *bool                 `json:"enable_pod_disruption_budget,omitempty"`
	SecretNameTemplate                     config.StringTemplate `json:"secret_name_template,omitempty"`
	ClusterDomain                          string                `json:"cluster_domain"`
	OAuthTokenSecretName                   spec.NamespacedName   `json:"oauth_token_secret_name,omitempty"`
	InfrastructureRolesSecretName          spec.NamespacedName   `json:"infrastructure_roles_secret_name,omitempty"`
	PodRoleLabel                           string                `json:"pod_role_label,omitempty"`
	ClusterLabels                          map[string]string     `json:"cluster_labels,omitempty"`
	InheritedLabels                        []string              `json:"inherited_labels,omitempty"`
	ClusterNameLabel                       string                `json:"cluster_name_label,omitempty"`
	NodeReadinessLabel                     map[string]string     `json:"node_readiness_label,omitempty"`
	// TODO: use a proper toleration structure?
	PodToleration map[string]string `json:"toleration,omitempty"`
	// TODO: use namespacedname
	PodEnvironmentConfigMap    string        `json:"pod_environment_configmap,omitempty"`
	PodPriorityClassName       string        `json:"pod_priority_class_name,omitempty"`
	MasterPodMoveTimeout       time.Duration `json:"master_pod_move_timeout,omitempty"`
	EnablePodAntiAffinity      bool          `json:"enable_pod_antiaffinity,omitempty"`
	PodAntiAffinityTopologyKey string        `json:"pod_antiaffinity_topology_key,omitempty"`
	PodManagementPolicy        string        `json:"pod_management_policy,omitempty"`
}

// PostgresPodResourcesDefaults defines the spec of default resources
type PostgresPodResourcesDefaults struct {
	DefaultCPURequest    string `json:"default_cpu_request,omitempty"`
	DefaultMemoryRequest string `json:"default_memory_request,omitempty"`
	DefaultCPULimit      string `json:"default_cpu_limit,omitempty"`
	DefaultMemoryLimit   string `json:"default_memory_limit,omitempty"`
}

// OperatorTimeouts defines the timeout of ResourceCheck, PodWait, ReadyWait
type OperatorTimeouts struct {
	ResourceCheckInterval  Duration `json:"resource_check_interval,omitempty"`
	ResourceCheckTimeout   Duration `json:"resource_check_timeout,omitempty"`
	PodLabelWaitTimeout    Duration `json:"pod_label_wait_timeout,omitempty"`
	PodDeletionWaitTimeout Duration `json:"pod_deletion_wait_timeout,omitempty"`
	ReadyWaitInterval      Duration `json:"ready_wait_interval,omitempty"`
	ReadyWaitTimeout       Duration `json:"ready_wait_timeout,omitempty"`
}

// LoadBalancerConfiguration defines the LB configuration
type LoadBalancerConfiguration struct {
	DbHostedZone              string                `json:"db_hosted_zone,omitempty"`
	EnableMasterLoadBalancer  bool                  `json:"enable_master_load_balancer,omitempty"`
	EnableReplicaLoadBalancer bool                  `json:"enable_replica_load_balancer,omitempty"`
	CustomServiceAnnotations  map[string]string     `json:"custom_service_annotations,omitempty"`
	MasterDNSNameFormat       config.StringTemplate `json:"master_dns_name_format,omitempty"`
	ReplicaDNSNameFormat      config.StringTemplate `json:"replica_dns_name_format,omitempty"`
}

// AWSGCPConfiguration defines the configuration for AWS
// TODO complete Google Cloud Platform (GCP) configuration
type AWSGCPConfiguration struct {
	WALES3Bucket              string `json:"wal_s3_bucket,omitempty"`
	AWSRegion                 string `json:"aws_region,omitempty"`
	LogS3Bucket               string `json:"log_s3_bucket,omitempty"`
	KubeIAMRole               string `json:"kube_iam_role,omitempty"`
	AdditionalSecretMount     string `json:"additional_secret_mount,omitempty"`
	AdditionalSecretMountPath string `json:"additional_secret_mount_path" default:"/meta/credentials"`
}

// OperatorDebugConfiguration defines options for the debug mode
type OperatorDebugConfiguration struct {
	DebugLogging   bool `json:"debug_logging,omitempty"`
	EnableDBAccess bool `json:"enable_database_access,omitempty"`
}

// TeamsAPIConfiguration defines the configration of TeamsAPI
type TeamsAPIConfiguration struct {
	EnableTeamsAPI           bool              `json:"enable_teams_api,omitempty"`
	TeamsAPIUrl              string            `json:"teams_api_url,omitempty"`
	TeamAPIRoleConfiguration map[string]string `json:"team_api_role_configuration,omitempty"`
	EnableTeamSuperuser      bool              `json:"enable_team_superuser,omitempty"`
	EnableAdminRoleForUsers  bool              `json:"enable_admin_role_for_users,omitempty"`
	TeamAdminRole            string            `json:"team_admin_role,omitempty"`
	PamRoleName              string            `json:"pam_role_name,omitempty"`
	PamConfiguration         string            `json:"pam_configuration,omitempty"`
	ProtectedRoles           []string          `json:"protected_role_names,omitempty"`
	PostgresSuperuserTeams   []string          `json:"postgres_superuser_teams,omitempty"`
}

// LoggingRESTAPIConfiguration defines Logging API conf
type LoggingRESTAPIConfiguration struct {
	APIPort               int `json:"api_port,omitempty"`
	RingLogLines          int `json:"ring_log_lines,omitempty"`
	ClusterHistoryEntries int `json:"cluster_history_entries,omitempty"`
}

// ScalyrConfiguration defines the configuration for ScalyrAPI
type ScalyrConfiguration struct {
	ScalyrAPIKey        string `json:"scalyr_api_key,omitempty"`
	ScalyrImage         string `json:"scalyr_image,omitempty"`
	ScalyrServerURL     string `json:"scalyr_server_url,omitempty"`
	ScalyrCPURequest    string `json:"scalyr_cpu_request,omitempty"`
	ScalyrMemoryRequest string `json:"scalyr_memory_request,omitempty"`
	ScalyrCPULimit      string `json:"scalyr_cpu_limit,omitempty"`
	ScalyrMemoryLimit   string `json:"scalyr_memory_limit,omitempty"`
}

// OperatorConfigurationData defines the operation config
type OperatorConfigurationData struct {
	EtcdHost                   string                             `json:"etcd_host,omitempty"`
	DockerImage                string                             `json:"docker_image,omitempty"`
	Workers                    uint32                             `json:"workers,omitempty"`
	MinInstances               int32                              `json:"min_instances,omitempty"`
	MaxInstances               int32                              `json:"max_instances,omitempty"`
	ResyncPeriod               Duration                           `json:"resync_period,omitempty"`
	RepairPeriod               Duration                           `json:"repair_period,omitempty"`
	SetMemoryRequestToLimit    bool                               `json:"set_memory_request_to_limit,omitempty"`
	ShmVolume                  *bool                              `json:"enable_shm_volume,omitempty"`
	Sidecars                   map[string]string                  `json:"sidecar_docker_images,omitempty"`
	PostgresUsersConfiguration PostgresUsersConfiguration         `json:"users"`
	Kubernetes                 KubernetesMetaConfiguration        `json:"kubernetes"`
	PostgresPodResources       PostgresPodResourcesDefaults       `json:"postgres_pod_resources"`
	Timeouts                   OperatorTimeouts                   `json:"timeouts"`
	LoadBalancer               LoadBalancerConfiguration          `json:"load_balancer"`
	AWSGCP                     AWSGCPConfiguration                `json:"aws_or_gcp"`
	OperatorDebug              OperatorDebugConfiguration         `json:"debug"`
	TeamsAPI                   TeamsAPIConfiguration              `json:"teams_api"`
	LoggingRESTAPI             LoggingRESTAPIConfiguration        `json:"logging_rest_api"`
	Scalyr                     ScalyrConfiguration                `json:"scalyr"`
	LogicalBackup              OperatorLogicalBackupConfiguration `json:"logical_backup"`
}

// OperatorConfigurationUsers defines configration for super user
type OperatorConfigurationUsers struct {
	SuperUserName            string            `json:"superuser_name,omitempty"`
	Replication              string            `json:"replication_user_name,omitempty"`
	ProtectedRoles           []string          `json:"protected_roles,omitempty"`
	TeamAPIRoleConfiguration map[string]string `json:"team_api_role_configuration,omitempty"`
}

//Duration shortens this frequently used name
type Duration time.Duration

type OperatorLogicalBackupConfiguration struct {
	Schedule    string `json:"logical_backup_schedule,omitempty"`
	DockerImage string `json:"logical_backup_docker_image,omitempty"`
	S3Bucket    string `json:"logical_backup_s3_bucket,omitempty"`
}
