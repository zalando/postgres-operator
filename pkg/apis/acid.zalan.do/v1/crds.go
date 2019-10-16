package v1

import (
	"github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CRDResource* define names necesssary for the k8s CRD API
const (
	PostgresCRDResourceKind   = "postgresql"
	PostgresCRDResourcePlural = "postgresqls"
	PostgresCRDResouceName    = PostgresCRDResourcePlural + "." + acidzalando.GroupName
	PostgresCRDResourceShort  = "pg"

	OperatorConfigCRDResouceKind    = "OperatorConfiguration"
	OperatorConfigCRDResourcePlural = "operatorconfigurations"
	OperatorConfigCRDResourceName   = OperatorConfigCRDResourcePlural + "." + acidzalando.GroupName
	OperatorConfigCRDResourceShort  = "opconfig"
)

// PostgresCRDResourceColumns definition of AdditionalPrinterColumns for postgresql CRD
var PostgresCRDResourceColumns = []apiextv1beta1.CustomResourceColumnDefinition{
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Team",
		Type:        "string",
		Description: "Team responsible for Postgres cluster",
		JSONPath:    ".spec.teamId",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Version",
		Type:        "string",
		Description: "PostgreSQL version",
		JSONPath:    ".spec.postgresql.version",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Pods",
		Type:        "integer",
		Description: "Number of Pods per Postgres cluster",
		JSONPath:    ".spec.numberOfInstances",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Volume",
		Type:        "string",
		Description: "Size of the bound volume",
		JSONPath:    ".spec.volume.size",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "CPU-Request",
		Type:        "string",
		Description: "Requested CPU for Postgres containers",
		JSONPath:    ".spec.resources.requests.cpu",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Memory-Request",
		Type:        "string",
		Description: "Requested memory for Postgres containers",
		JSONPath:    ".spec.resources.requests.memory",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:     "Age",
		Type:     "date",
		JSONPath: ".metadata.creationTimestamp",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Status",
		Type:        "string",
		Description: "Current sync status of postgresql resource",
		JSONPath:    ".status.PostgresClusterStatus",
	},
}

// OperatorConfigCRDResourceColumns definition of AdditionalPrinterColumns for OperatorConfiguration CRD
var OperatorConfigCRDResourceColumns = []apiextv1beta1.CustomResourceColumnDefinition{
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Image",
		Type:        "string",
		Description: "Spilo image to be used for Pods",
		JSONPath:    ".configuration.docker_image",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Cluster-Label",
		Type:        "string",
		Description: "Label for K8s resources created by operator",
		JSONPath:    ".configuration.kubernetes.cluster_name_label",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Service-Account",
		Type:        "string",
		Description: "Name of service account to be used",
		JSONPath:    ".configuration.kubernetes.pod_service_account_name",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Min-Instances",
		Type:        "integer",
		Description: "Minimum number of instances per Postgres cluster",
		JSONPath:    ".configuration.min_instances",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:     "Age",
		Type:     "date",
		JSONPath: ".metadata.creationTimestamp",
	},
}

var min0 = 0.0
var min1 = 1.0
var minDisable = -1.0

var postgresCRDResourceValidation = apiextv1beta1.JSONSchemaProps{
	Type:     "object",
	Required: []string{"kind", "apiVersion", "metadata", "spec"},
	Properties: map[string]apiextv1beta1.JSONSchemaProps{
		"kind": {
			Type: "string",
			Enum: []apiextv1beta1.JSON{
				{
					Raw: []byte(`"postgresql"`),
				},
			},
		},
		"apiVersion": {
			Type: "string",
			Enum: []apiextv1beta1.JSON{
				{
					Raw: []byte(`"acid.zalan.do/v1"`),
				},
			},
		},
		"metadata": {
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]apiextv1beta1.JSONSchemaProps{
				"annotations": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				"labels": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				"name": {
					Type:        "string",
					Description: "The cluster name specified here must begin with the team name",
				},
				"namespace": {
					Type: "string",
				},
			},
		},
		"spec": {
			Type:     "object",
			Required: []string{"numberOfInstances", "teamId", "postgresql"},
			Properties: map[string]apiextv1beta1.JSONSchemaProps{
				"allowedSourceRanges": {
					Type: "array",
					Items: &apiextv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type:    "string",
							Pattern: "^(\\d|[1-9]\\d|1\\d\\d|2[0-4]\\d|25[0-5])\\.(\\d|[1-9]\\d|1\\d\\d|2[0-4]\\d|25[0-5])\\.(\\d|[1-9]\\d|1\\d\\d|2[0-4]\\d|25[0-5])\\.(\\d|[1-9]\\d|1\\d\\d|2[0-4]\\d|25[0-5])\\/(\\d|[1-2]\\d|3[0-2])$",
						},
					},
				},
				"clone": {
					Type:     "object",
					Required: []string{"cluster"},
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"cluster": {
							Type: "string",
						},
						"s3_endpoint": {
							Type: "string",
						},
						"s3_access_key_id": {
							Type: "string",
						},
						"s3_secret_access_key": {
							Type: "string",
						},
						"s3_force_path_style": {
							Type: "string",
						},
						"s3_wal_path": {
							Type: "string",
						},
						"timestamp": {
							Type:        "string",
							Description: "Date-time format that specifies a timezone as an offset relative to UTC e.g. 1996-12-19T16:39:57-08:00",
							Pattern:     "^([0-9]+)-(0[1-9]|1[012])-(0[1-9]|[12][0-9]|3[01])[Tt]([01][0-9]|2[0-3]):([0-5][0-9]):([0-5][0-9]|60)(\\.[0-9]+)?(([Zz])|([+-]([01][0-9]|2[0-3]):[0-5][0-9]))$",
						},
						"uid": {
							Type:   "string",
							Format: "uuid",
						},
					},
				},
				"databases": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type:        "string",
							Description: "User names specified here as database owners must be declared in the users key of the spec key",
						},
					},
				},
				"dockerImage": {
					Type: "string",
				},
				"enableLogicalBackup": {
					Type: "boolean",
				},
				"enableMasterLoadBalancer": {
					Type: "boolean",
				},
				"enableReplicaLoadBalancer": {
					Type: "boolean",
				},
				"enableShmVolume": {
					Type: "boolean",
				},
				"logicalBackupSchedule": {
					Type:    "string",
					Pattern: "^(\\d+|\\*)(/\\d+)?(\\s+(\\d+|\\*)(/\\d+)?){4}$",
				},
				"maintenanceWindows": {
					Type: "array",
					Items: &apiextv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type:    "string",
							Pattern: "^\\ *((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))-((Mon|Tue|Wed|Thu|Fri|Sat|Sun):(2[0-3]|[01]?\\d):([0-5]?\\d)|(2[0-3]|[01]?\\d):([0-5]?\\d))\\ *$",
						},
					},
				},
				"numberOfInstances": {
					Type:    "integer",
					Minimum: &min0,
				},
				"patroni": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"initdb": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"pg_hba": {
							Type: "array",
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"slots": {
							Type: "array",
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "object",
									AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
										Schema: &apiextv1beta1.JSONSchemaProps{
											Type: "string",
										},
									},
								},
							},
						},
						"ttl": {
							Type: "integer",
						},
						"loop_wait": {
							Type: "integer",
						},
						"retry_timeout": {
							Type: "integer",
						},
						"maximum_lag_on_failover": {
							Type: "integer",
						},
					},
				},
				"pod_priority_class_name": {
					Type:        "string",
					Description: "Deprecated",
				},
				"podPriorityClassName": {
					Type: "string",
				},
				"postgresql": {
					Type:     "object",
					Required: []string{"version"},
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"version": {
							Type: "string",
							Enum: []apiextv1beta1.JSON{
								{
									Raw: []byte(`"9.3"`),
								},
								{
									Raw: []byte(`"9.4"`),
								},
								{
									Raw: []byte(`"9.5"`),
								},
								{
									Raw: []byte(`"9.6"`),
								},
								{
									Raw: []byte(`"10"`),
								},
								{
									Raw: []byte(`"11"`),
								},
								{
									Raw: []byte(`"12"`),
								},
							},
						},
						"parameters": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
					},
				},
				"replicaLoadBalancer": {
					Type:        "boolean",
					Description: "Deprecated",
				},
				"resources": {
					Type:     "object",
					Required: []string{"requests", "limits"},
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"limits": {
							Type:     "object",
							Required: []string{"cpu", "memory"},
							Properties: map[string]apiextv1beta1.JSONSchemaProps{
								"cpu": {
									Type:        "string",
									Description: "Decimal natural followed by m, or decimal natural followed by dot followed by up to three decimal digits (precision used by Kubernetes). Must be greater than 0",
									Pattern:     "^(\\d+m|\\d+\\.\\d{1,3})$",
								},
								"memory": {
									Type:        "string",
									Description: "Plain integer or fixed-point integer using one of these suffixes: E, P, T, G, M, k (with or without a tailing i). Must be greater than 0",
									Pattern:     "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
								},
							},
						},
						"requests": {
							Type:     "object",
							Required: []string{"cpu", "memory"},
							Properties: map[string]apiextv1beta1.JSONSchemaProps{
								"cpu": {
									Type:        "string",
									Description: "Decimal natural followed by m, or decimal natural followed by dot followed by up to three decimal digits (precision used by Kubernetes). Must be greater than 0",
									Pattern:     "^(\\d+m|\\d+\\.\\d{1,3})$",
								},
								"memory": {
									Type:        "string",
									Description: "Plain integer or fixed-point integer using one of these suffixes: E, P, T, G, M, k (with or without a tailing i). Must be greater than 0",
									Pattern:     "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
								},
							},
						},
					},
				},
				"spiloFSGroup": {
					Type: "integer",
				},
				"standby": {
					Type:     "object",
					Required: []string{"s3_wal_path"},
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"s3_wal_path": {
							Type: "string",
						},
					},
				},
				"teamId": {
					Type: "string",
				},
				"tolerations": {
					Type: "array",
					Items: &apiextv1beta1.JSONSchemaPropsOrArray{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type:     "object",
							Required: []string{"key", "operator", "effect"},
							Properties: map[string]apiextv1beta1.JSONSchemaProps{
								"key": {
									Type: "string",
								},
								"operator": {
									Type: "string",
									Enum: []apiextv1beta1.JSON{
										{
											Raw: []byte(`"Equal"`),
										},
										{
											Raw: []byte(`"Exists"`),
										},
									},
								},
								"value": {
									Type: "string",
								},
								"effect": {
									Type: "string",
									Enum: []apiextv1beta1.JSON{
										{
											Raw: []byte(`"NoExecute"`),
										},
										{
											Raw: []byte(`"NoSchedule"`),
										},
										{
											Raw: []byte(`"PreferNoSchedule"`),
										},
									},
								},
								"tolerationSeconds": {
									Type: "integer",
								},
							},
						},
					},
				},
				"useLoadBalancer": {
					Type:        "boolean",
					Description: "Deprecated",
				},
				"users": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type:        "array",
							Description: "Role flags specified here must not contradict each other",
							Nullable:    true,
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
									Enum: []apiextv1beta1.JSON{
										{
											Raw: []byte(`"bypassrls"`),
										},
										{
											Raw: []byte(`"BYPASSRLS"`),
										},
										{
											Raw: []byte(`"nobypassrls"`),
										},
										{
											Raw: []byte(`"NOBYPASSRLS"`),
										},
										{
											Raw: []byte(`"createdb"`),
										},
										{
											Raw: []byte(`"CREATEDB"`),
										},
										{
											Raw: []byte(`"nocreatedb"`),
										},
										{
											Raw: []byte(`"NOCREATEDB"`),
										},
										{
											Raw: []byte(`"createrole"`),
										},
										{
											Raw: []byte(`"CREATEROLE"`),
										},
										{
											Raw: []byte(`"nocreaterole"`),
										},
										{
											Raw: []byte(`"NOCREATEROLE"`),
										},
										{
											Raw: []byte(`"inherit"`),
										},
										{
											Raw: []byte(`"INHERIT"`),
										},
										{
											Raw: []byte(`"noinherit"`),
										},
										{
											Raw: []byte(`"NOINHERIT"`),
										},
										{
											Raw: []byte(`"login"`),
										},
										{
											Raw: []byte(`"LOGIN"`),
										},
										{
											Raw: []byte(`"nologin"`),
										},
										{
											Raw: []byte(`"NOLOGIN"`),
										},
										{
											Raw: []byte(`"replication"`),
										},
										{
											Raw: []byte(`"REPLICATION"`),
										},
										{
											Raw: []byte(`"noreplication"`),
										},
										{
											Raw: []byte(`"NOREPLICATION"`),
										},
										{
											Raw: []byte(`"superuser"`),
										},
										{
											Raw: []byte(`"SUPERUSER"`),
										},
										{
											Raw: []byte(`"nosuperuser"`),
										},
										{
											Raw: []byte(`"NOSUPERUSER"`),
										},
									},
								},
							},
						},
					},
				},
				"volume": {
					Type:     "object",
					Required: []string{"size"},
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"size": {
							Type:        "string",
							Description: "Value must not be zero",
							Pattern:     "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
						},
						"storageClass": {
							Type: "string",
						},
						"subPath": {
							Type: "string",
						},
					},
				},
			},
		},
	},
}

var operatorConfigCRDResourceValidation = apiextv1beta1.JSONSchemaProps{
	Type:     "object",
	Required: []string{"kind", "apiVersion", "metadata", "configuration"},
	Properties: map[string]apiextv1beta1.JSONSchemaProps{
		"kind": {
			Type: "string",
			Enum: []apiextv1beta1.JSON{
				{
					Raw: []byte(`"OperatorConfiguration"`),
				},
			},
		},
		"apiVersion": {
			Type: "string",
			Enum: []apiextv1beta1.JSON{
				{
					Raw: []byte(`"acid.zalan.do/v1"`),
				},
			},
		},
		"metadata": {
			Type:     "object",
			Required: []string{"name"},
			Properties: map[string]apiextv1beta1.JSONSchemaProps{
				"annotations": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				"labels": {
					Type: "object",
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				"name": {
					Type: "string",
				},
				"namespace": {
					Type: "string",
				},
			},
		},
		"configuration": {
			Type: "object",
			Properties: map[string]apiextv1beta1.JSONSchemaProps{
				"etcd_host": {
					Type:     "string",
					Nullable: true,
				},
				"docker_image": {
					Type: "string",
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
					AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
						Schema: &apiextv1beta1.JSONSchemaProps{
							Type: "string",
						},
					},
				},
				"workers": {
					Type:    "integer",
					Minimum: &min1,
				},
				"users": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"replication_username": {
							Type: "string",
						},
						"super_username": {
							Type: "string",
						},
					},
				},
				"kubernetes": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"cluster_domain": {
							Type: "string",
						},
						"cluster_labels": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"cluster_name_label": {
							Type: "string",
						},
						"enable_pod_antiaffinity": {
							Type: "boolean",
						},
						"enable_pod_disruption_budget": {
							Type: "boolean",
						},
						"infrastructure_roles_secret_name": {
							Type: "string",
						},
						"inherited_labels": {
							Type: "array",
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"node_readiness_label": {
							Type:     "array",
							Nullable: true,
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"oauth_token_secret_name": {
							Type: "string",
						},
						"pdb_name_format": {
							Type: "string",
						},
						"pod_antiaffinity_topology_key": {
							Type: "string",
						},
						"pod_environment_configmap": {
							Type: "string",
						},
						"pod_management_policy": {
							Type: "string",
						},
						"pod_role_label": {
							Type: "string",
						},
						"pod_service_account_name": {
							Type: "string",
						},
						"pod_terminate_grace_period": {
							Type: "string",
						},
						"secret_name_template": {
							Type: "string",
						},
						"spilo_fsgroup": {
							Type: "integer",
						},
						"spilo_privileged": {
							Type: "boolean",
						},
						"toleration": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"watched_namespace": {
							Type:     "string",
							Nullable: true,
						},
					},
				},
				"postgres_pod_resources": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"default_cpu_limit": {
							Type:    "string",
							Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
						},
						"default_cpu_request": {
							Type:    "string",
							Pattern: "^(\\d+m|\\d+(\\.\\d{1,3})?)$",
						},
						"default_memory_limit": {
							Type:    "string",
							Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
						},
						"default_memory_request": {
							Type:    "string",
							Pattern: "^(\\d+(e\\d+)?|\\d+(\\.\\d+)?(e\\d+)?[EPTGMK]i?)$",
						},
					},
				},
				"timeouts": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
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
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"db_hosted_zone": {
							Type: "string",
						},
						"enable_master_load_balancer": {
							Type: "boolean",
						},
						"enable_replica_load_balancer": {
							Type: "boolean",
						},
						"custom_service_annotations": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"master_dns_name_format": {
							Type: "string",
						},
						"replica_dns_name_format": {
							Type: "string",
						},
					},
				},
				"aws_or_gcp": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"additional_secret_mount": {
							Type: "string",
						},
						"additional_secret_mount_path": {
							Type: "string",
						},
						"aws_region": {
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
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"logical_backup_schedule": {
							Type:    "string",
							Pattern: "^(\\d+|\\*)(/\\d+)?(\\s+(\\d+|\\*)(/\\d+)?){4}$",
						},
						"logical_backup_docker_image": {
							Type: "string",
						},
						"logical_backup_s3_bucket": {
							Type: "string",
						},
					},
				},
				"debug": {
					Type: "object",
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
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
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
						"enable_admin_role_for_users": {
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
							Type:     "array",
							Nullable: true,
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"protected_role_names": {
							Type:     "array",
							Nullable: true,
							Items: &apiextv1beta1.JSONSchemaPropsOrArray{
								Schema: &apiextv1beta1.JSONSchemaProps{
									Type: "string",
								},
							},
						},
						"team_admin_role": {
							Type: "string",
						},
						"team_api_role_configuration": {
							Type: "object",
							AdditionalProperties: &apiextv1beta1.JSONSchemaPropsOrBool{
								Schema: &apiextv1beta1.JSONSchemaProps{
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
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
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
					Properties: map[string]apiextv1beta1.JSONSchemaProps{
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
			},
		},
	},
}

func buildCRD(name, kind, plural, short string, columns []apiextv1beta1.CustomResourceColumnDefinition, validationSchema apiextv1beta1.JSONSchemaProps) *apiextv1beta1.CustomResourceDefinition {
	return &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group:   SchemeGroupVersion.Group,
			Version: SchemeGroupVersion.Version,
			Names: apiextv1beta1.CustomResourceDefinitionNames{
				Plural:     plural,
				ShortNames: []string{short},
				Kind:       kind,
			},
			Scope: apiextv1beta1.NamespaceScoped,
			Subresources: &apiextv1beta1.CustomResourceSubresources{
				Status: &apiextv1beta1.CustomResourceSubresourceStatus{},
			},
			AdditionalPrinterColumns: columns,
			Validation: &apiextv1beta1.CustomResourceValidation{
				OpenAPIV3Schema: &validationSchema,
			},
		},
	}
}

// PostgresCRD returns CustomResourceDefinition built from PostgresCRDResource
func PostgresCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(PostgresCRDResouceName,
		PostgresCRDResourceKind,
		PostgresCRDResourcePlural,
		PostgresCRDResourceShort,
		PostgresCRDResourceColumns,
		postgresCRDResourceValidation)
}

// ConfigurationCRD returns CustomResourceDefinition built from OperatorConfigCRDResource
func ConfigurationCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(OperatorConfigCRDResourceName,
		OperatorConfigCRDResouceKind,
		OperatorConfigCRDResourcePlural,
		OperatorConfigCRDResourceShort,
		OperatorConfigCRDResourceColumns,
		operatorConfigCRDResourceValidation)
}
