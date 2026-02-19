package v1

import (
	_ "embed"
	"fmt"

	acidzalando "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do"
	"github.com/zalando/postgres-operator/pkg/util"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// CRDResource* define names necesssary for the k8s CRD API
const (
	PostgresCRDResourceKind = "postgresql"

	OperatorConfigCRDResouceKind    = "OperatorConfiguration"
	OperatorConfigCRDResourcePlural = "operatorconfigurations"
	OperatorConfigCRDResourceList   = OperatorConfigCRDResouceKind + "List"
	OperatorConfigCRDResourceName   = OperatorConfigCRDResourcePlural + "." + acidzalando.GroupName
	OperatorConfigCRDResourceShort  = "opconfig"
)

// OperatorConfigCRDResourceColumns definition of AdditionalPrinterColumns for OperatorConfiguration CRD
var OperatorConfigCRDResourceColumns = []apiextv1.CustomResourceColumnDefinition{
	{
		Name:        "Image",
		Type:        "string",
		Description: "Spilo image to be used for Pods",
		JSONPath:    ".configuration.docker_image",
	},
	{
		Name:        "Cluster-Label",
		Type:        "string",
		Description: "Label for K8s resources created by operator",
		JSONPath:    ".configuration.kubernetes.cluster_name_label",
	},
	{
		Name:        "Service-Account",
		Type:        "string",
		Description: "Name of service account to be used",
		JSONPath:    ".configuration.kubernetes.pod_service_account_name",
	},
	{
		Name:        "Min-Instances",
		Type:        "integer",
		Description: "Minimum number of instances per Postgres cluster",
		JSONPath:    ".configuration.min_instances",
	},
	{
		Name:     "Age",
		Type:     "date",
		JSONPath: ".metadata.creationTimestamp",
	},
}

var min1 = 1.0
var minLength1 int64 = 1
var minDisable = -1.0

// OperatorConfigCRDResourceValidation to check applied manifest parameters
var OperatorConfigCRDResourceValidation = apiextv1.CustomResourceValidation{
	OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
		Type:     "object",
		Required: []string{"kind", "apiVersion", "configuration"},
		Properties: map[string]apiextv1.JSONSchemaProps{
			"kind": {
				Type: "string",
				Enum: []apiextv1.JSON{
					{
						Raw: []byte(`"OperatorConfiguration"`),
					},
				},
			},
			"apiVersion": {
				Type: "string",
				Enum: []apiextv1.JSON{
					{
						Raw: []byte(`"acid.zalan.do/v1"`),
					},
				},
			},
			"configuration": {
				Type: "object",
				Properties: map[string]apiextv1.JSONSchemaProps{
					"crd_categories": {
						Type:     "array",
						Nullable: true,
						Items: &apiextv1.JSONSchemaPropsOrArray{
							Schema: &apiextv1.JSONSchemaProps{
								Type: "string",
							},
						},
					},
					"docker_image": {
						Type: "string",
					},
					"enable_crd_registration": {
						Type: "boolean",
					},
					"enable_crd_validation": {
						Type:        "boolean",
						Description: "deprecated",
					},
					"enable_lazy_spilo_upgrade": {
						Type: "boolean",
					},
					"enable_shm_volume": {
						Type: "boolean",
					},
					"enable_spilo_wal_path_compat": {
						Type:        "boolean",
						Description: "deprecated",
					},
					"enable_team_id_clustername_prefix": {
						Type: "boolean",
					},
					"etcd_host": {
						Type: "string",
					},
					"ignore_instance_limits_annotation_key": {
						Type: "string",
					},
					"ignore_resources_limits_annotation_key": {
						Type: "string",
					},
					"kubernetes_use_configmaps": {
						Type: "boolean",
					},
					"maintenance_windows": {
						Type: "array",
						Items: &apiextv1.JSONSchemaPropsOrArray{
							Schema: &apiextv1.JSONSchemaProps{
								Type:    "string",
								Pattern: "^\\ *((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))-((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))\\ *$",
							},
						},
					},
					"max_instances": {
						Type:        "integer",
						Description: "-1 = disabled",
						Minimum:     &minDisable,
					},
					"min_instances": {
						Type:        "integer",
						Description: "-1 = disabled",
						Minimum:     &minDisable,
					},
					"resync_period": {
						Type: "string",
					},
					"repair_period": {
						Type: "string",
					},
					"set_memory_request_to_limit": {
						Type: "boolean",
					},
					"sidecar_docker_images": {
						Type: "object",
						AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
							Schema: &apiextv1.JSONSchemaProps{
								Type: "string",
							},
						},
					},
					"sidecars": {
						Type:     "array",
						Nullable: true,
						Items: &apiextv1.JSONSchemaPropsOrArray{
							Schema: &apiextv1.JSONSchemaProps{
								Type:                   "object",
								XPreserveUnknownFields: util.True(),
							},
						},
					},
					"workers": {
						Type:    "integer",
						Minimum: &min1,
					},
					"users": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"additional_owner_roles": {
								Type:     "array",
								Nullable: true,
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"enable_password_rotation": {
								Type: "boolean",
							},
							"password_rotation_interval": {
								Type: "integer",
							},
							"password_rotation_user_retention": {
								Type: "integer",
							},
							"replication_username": {
								Type: "string",
							},
							"super_username": {
								Type: "string",
							},
						},
					},
					"major_version_upgrade": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"major_version_upgrade_mode": {
								Type: "string",
							},
							"major_version_upgrade_team_allow_list": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"minimal_major_version": {
								Type: "string",
							},
							"target_major_version": {
								Type: "string",
							},
						},
					},
					"kubernetes": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"additional_pod_capabilities": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"cluster_domain": {
								Type: "string",
							},
							"cluster_labels": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"cluster_name_label": {
								Type: "string",
							},
							"custom_pod_annotations": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"delete_annotation_date_key": {
								Type: "string",
							},
							"delete_annotation_name_key": {
								Type: "string",
							},
							"downscaler_annotations": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"enable_cross_namespace_secret": {
								Type: "boolean",
							},
							"enable_finalizers": {
								Type: "boolean",
							},
							"enable_init_containers": {
								Type: "boolean",
							},
							"enable_owner_references": {
								Type: "boolean",
							},
							"enable_persistent_volume_claim_deletion": {
								Type: "boolean",
							},
							"enable_pod_antiaffinity": {
								Type: "boolean",
							},
							"enable_pod_disruption_budget": {
								Type: "boolean",
							},
							"enable_readiness_probe": {
								Type: "boolean",
							},
							"enable_secrets_deletion": {
								Type: "boolean",
							},
							"enable_sidecars": {
								Type: "boolean",
							},
							"ignored_annotations": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"infrastructure_roles_secret_name": {
								Type: "string",
							},
							"infrastructure_roles_secrets": {
								Type:     "array",
								Nullable: true,
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type:     "object",
										Required: []string{"secretname", "userkey", "passwordkey"},
										Properties: map[string]apiextv1.JSONSchemaProps{
											"secretname": {
												Type: "string",
											},
											"userkey": {
												Type: "string",
											},
											"passwordkey": {
												Type: "string",
											},
											"rolekey": {
												Type: "string",
											},
											"defaultuservalue": {
												Type: "string",
											},
											"defaultrolevalue": {
												Type: "string",
											},
											"details": {
												Type: "string",
											},
											"template": {
												Type: "boolean",
											},
										},
									},
								},
							},
							"inherited_annotations": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"inherited_labels": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"master_pod_move_timeout": {
								Type: "string",
							},
							"node_readiness_label": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"node_readiness_label_merge": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"AND"`),
									},
									{
										Raw: []byte(`"OR"`),
									},
								},
							},
							"oauth_token_secret_name": {
								Type: "string",
							},
							"pdb_name_format": {
								Type: "string",
							},
							"pdb_master_label_selector": {
								Type: "boolean",
							},
							"persistent_volume_claim_retention_policy": {
								Type: "object",
								Properties: map[string]apiextv1.JSONSchemaProps{
									"when_deleted": {
										Type: "string",
										Enum: []apiextv1.JSON{
											{
												Raw: []byte(`"delete"`),
											},
											{
												Raw: []byte(`"retain"`),
											},
										},
									},
									"when_scaled": {
										Type: "string",
										Enum: []apiextv1.JSON{
											{
												Raw: []byte(`"delete"`),
											},
											{
												Raw: []byte(`"retain"`),
											},
										},
									},
								},
							},
							"pod_antiaffinity_preferred_during_scheduling": {
								Type: "boolean",
							},
							"pod_antiaffinity_topology_key": {
								Type: "string",
							},
							"pod_environment_configmap": {
								Type: "string",
							},
							"pod_environment_secret": {
								Type: "string",
							},
							"pod_management_policy": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"ordered_ready"`),
									},
									{
										Raw: []byte(`"parallel"`),
									},
								},
							},
							"pod_priority_class_name": {
								Type: "string",
							},
							"pod_role_label": {
								Type: "string",
							},
							"pod_service_account_definition": {
								Type: "string",
							},
							"pod_service_account_name": {
								Type: "string",
							},
							"pod_service_account_role_binding_definition": {
								Type: "string",
							},
							"pod_terminate_grace_period": {
								Type: "string",
							},
							"secret_name_template": {
								Type: "string",
							},
							"share_pgsocket_with_sidecars": {
								Type: "boolean",
							},
							"spilo_runasuser": {
								Type: "integer",
							},
							"spilo_runasgroup": {
								Type: "integer",
							},
							"spilo_fsgroup": {
								Type: "integer",
							},
							"spilo_privileged": {
								Type: "boolean",
							},
							"spilo_allow_privilege_escalation": {
								Type: "boolean",
							},
							"storage_resize_mode": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"ebs"`),
									},
									{
										Raw: []byte(`"mixed"`),
									},
									{
										Raw: []byte(`"pvc"`),
									},
									{
										Raw: []byte(`"off"`),
									},
								},
							},
							"toleration": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"watched_namespace": {
								Type: "string",
							},
						},
					},
					"patroni": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"enable_patroni_failsafe_mode": {
								Type: "boolean",
							},
						},
					},
					"postgres_pod_resources": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"default_cpu_limit": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$|^$",
							},
							"default_cpu_request": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$|^$",
							},
							"default_memory_limit": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$|^$",
							},
							"default_memory_request": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$|^$",
							},
							"max_cpu_request": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$|^$",
							},
							"max_memory_request": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$|^$",
							},
							"min_cpu_limit": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$|^$",
							},
							"min_memory_limit": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$|^$",
							},
						},
					},
					"timeouts": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"patroni_api_check_interval": {
								Type: "string",
							},
							"patroni_api_check_timeout": {
								Type: "string",
							},
							"pod_label_wait_timeout": {
								Type: "string",
							},
							"pod_deletion_wait_timeout": {
								Type: "string",
							},
							"ready_wait_interval": {
								Type: "string",
							},
							"ready_wait_timeout": {
								Type: "string",
							},
							"resource_check_interval": {
								Type: "string",
							},
							"resource_check_timeout": {
								Type: "string",
							},
						},
					},
					"load_balancer": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"custom_service_annotations": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"db_hosted_zone": {
								Type: "string",
							},
							"enable_master_load_balancer": {
								Type: "boolean",
							},
							"enable_master_pooler_load_balancer": {
								Type: "boolean",
							},
							"enable_replica_load_balancer": {
								Type: "boolean",
							},
							"enable_replica_pooler_load_balancer": {
								Type: "boolean",
							},
							"external_traffic_policy": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"Cluster"`),
									},
									{
										Raw: []byte(`"Local"`),
									},
								},
							},
							"master_dns_name_format": {
								Type: "string",
							},
							"master_legacy_dns_name_format": {
								Type: "string",
							},
							"replica_dns_name_format": {
								Type: "string",
							},
							"replica_legacy_dns_name_format": {
								Type: "string",
							},
						},
					},
					"aws_or_gcp": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"additional_secret_mount": {
								Type: "string",
							},
							"additional_secret_mount_path": {
								Type: "string",
							},
							"aws_region": {
								Type: "string",
							},
							"enable_ebs_gp3_migration": {
								Type: "boolean",
							},
							"enable_ebs_gp3_migration_max_size": {
								Type: "integer",
							},
							"gcp_credentials": {
								Type: "string",
							},
							"kube_iam_role": {
								Type: "string",
							},
							"log_s3_bucket": {
								Type: "string",
							},
							"wal_s3_bucket": {
								Type: "string",
							},
						},
					},
					"logical_backup": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"logical_backup_azure_storage_account_name": {
								Type: "string",
							},
							"logical_backup_azure_storage_container": {
								Type: "string",
							},
							"logical_backup_azure_storage_account_key": {
								Type: "string",
							},
							"logical_backup_cpu_limit": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"logical_backup_cpu_request": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"logical_backup_docker_image": {
								Type: "string",
							},
							"logical_backup_google_application_credentials": {
								Type: "string",
							},
							"logical_backup_job_prefix": {
								Type: "string",
							},
							"logical_backup_memory_limit": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"logical_backup_memory_request": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"logical_backup_provider": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"az"`),
									},
									{
										Raw: []byte(`"gcs"`),
									},
									{
										Raw: []byte(`"s3"`),
									},
								},
							},
							"logical_backup_s3_access_key_id": {
								Type: "string",
							},
							"logical_backup_s3_bucket": {
								Type: "string",
							},
							"logical_backup_s3_bucket_prefix": {
								Type: "string",
							},
							"logical_backup_s3_endpoint": {
								Type: "string",
							},
							"logical_backup_s3_region": {
								Type: "string",
							},
							"logical_backup_s3_secret_access_key": {
								Type: "string",
							},
							"logical_backup_s3_sse": {
								Type: "string",
							},
							"logical_backup_s3_retention_time": {
								Type: "string",
							},
							"logical_backup_schedule": {
								Type:    "string",
								Pattern: "^(\\d+|\\*)(/\\d+)?(\\s+(\\d+|\\*)(/\\d+)?){4}$",
							},
							"logical_backup_cronjob_environment_secret": {
								Type: "string",
							},
						},
					},
					"debug": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"debug_logging": {
								Type: "boolean",
							},
							"enable_database_access": {
								Type: "boolean",
							},
						},
					},
					"teams_api": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"enable_admin_role_for_users": {
								Type: "boolean",
							},
							"enable_postgres_team_crd": {
								Type: "boolean",
							},
							"enable_postgres_team_crd_superusers": {
								Type: "boolean",
							},
							"enable_team_member_deprecation": {
								Type: "boolean",
							},
							"enable_team_superuser": {
								Type: "boolean",
							},
							"enable_teams_api": {
								Type: "boolean",
							},
							"pam_configuration": {
								Type: "string",
							},
							"pam_role_name": {
								Type: "string",
							},
							"postgres_superuser_teams": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"protected_role_names": {
								Type: "array",
								Items: &apiextv1.JSONSchemaPropsOrArray{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"role_deletion_suffix": {
								Type: "string",
							},
							"team_admin_role": {
								Type: "string",
							},
							"team_api_role_configuration": {
								Type: "object",
								AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
									Schema: &apiextv1.JSONSchemaProps{
										Type: "string",
									},
								},
							},
							"teams_api_url": {
								Type: "string",
							},
						},
					},
					"logging_rest_api": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"api_port": {
								Type: "integer",
							},
							"cluster_history_entries": {
								Type: "integer",
							},
							"ring_log_lines": {
								Type: "integer",
							},
						},
					},
					"scalyr": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"scalyr_api_key": {
								Type: "string",
							},
							"scalyr_cpu_limit": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"scalyr_cpu_request": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"scalyr_image": {
								Type: "string",
							},
							"scalyr_memory_limit": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"scalyr_memory_request": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"scalyr_server_url": {
								Type: "string",
							},
						},
					},
					"connection_pooler": {
						Type: "object",
						Properties: map[string]apiextv1.JSONSchemaProps{
							"connection_pooler_default_cpu_limit": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"connection_pooler_default_cpu_request": {
								Type:    "string",
								Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
							},
							"connection_pooler_default_memory_limit": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"connection_pooler_default_memory_request": {
								Type:    "string",
								Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
							},
							"connection_pooler_image": {
								Type: "string",
							},
							"connection_pooler_max_db_connections": {
								Type: "integer",
							},
							"connection_pooler_mode": {
								Type: "string",
								Enum: []apiextv1.JSON{
									{
										Raw: []byte(`"session"`),
									},
									{
										Raw: []byte(`"transaction"`),
									},
								},
							},
							"connection_pooler_number_of_instances": {
								Type:    "integer",
								Minimum: &min1,
							},
							"connection_pooler_schema": {
								Type: "string",
							},
							"connection_pooler_user": {
								Type: "string",
							},
						},
					},
				},
			},
			"status": {
				Type: "object",
				AdditionalProperties: &apiextv1.JSONSchemaPropsOrBool{
					Schema: &apiextv1.JSONSchemaProps{
						Type: "string",
					},
				},
			},
		},
	},
}

func buildCRD(name, kind, plural, list, short string,
	categories []string,
	columns []apiextv1.CustomResourceColumnDefinition,
	validation apiextv1.CustomResourceValidation) *apiextv1.CustomResourceDefinition {
	return &apiextv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fmt.Sprintf("%s/%s", apiextv1.GroupName, apiextv1.SchemeGroupVersion.Version),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: SchemeGroupVersion.Group,
			Names: apiextv1.CustomResourceDefinitionNames{
				Kind:       kind,
				ListKind:   list,
				Plural:     plural,
				Singular:   kind,
				ShortNames: []string{short},
				Categories: categories,
			},
			Scope: apiextv1.NamespaceScoped,
			Versions: []apiextv1.CustomResourceDefinitionVersion{
				{
					Name:    SchemeGroupVersion.Version,
					Served:  true,
					Storage: true,
					Subresources: &apiextv1.CustomResourceSubresources{
						Status: &apiextv1.CustomResourceSubresourceStatus{},
					},
					AdditionalPrinterColumns: columns,
					Schema:                   &validation,
				},
			},
		},
	}
}

//go:embed postgresql.crd.yaml
var postgresqlCRDYAML []byte

// PostgresCRD returns CustomResourceDefinition built from PostgresCRDResource
func PostgresCRD(crdCategories []string) (*apiextv1.CustomResourceDefinition, error) {
	var crd apiextv1.CustomResourceDefinition
	err := yaml.Unmarshal(postgresqlCRDYAML, &crd)
	if err != nil {
		return nil, err
	}

	crd.Spec.Names.Categories = crdCategories

	return &crd, nil
}

// ConfigurationCRD returns CustomResourceDefinition built from OperatorConfigCRDResource
func ConfigurationCRD(crdCategories []string) *apiextv1.CustomResourceDefinition {
	return buildCRD(OperatorConfigCRDResourceName,
		OperatorConfigCRDResouceKind,
		OperatorConfigCRDResourcePlural,
		OperatorConfigCRDResourceList,
		OperatorConfigCRDResourceShort,
		crdCategories,
		OperatorConfigCRDResourceColumns,
		OperatorConfigCRDResourceValidation)
}
