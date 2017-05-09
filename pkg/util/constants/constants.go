package constants

const (
	//Constants
	TPRName                   = "postgresql"
	TPRVendor                 = "acid.zalan.do"
	TPRDescription            = "Managed PostgreSQL clusters"
	TPRApiVersion             = "v1"
	DataVolumeName            = "pgdata"
	PasswordLength            = 64
	UserSecretTemplate        = "%s.%s.credentials.%s.%s" // Username, ClusterName, TPRName, TPRVendor
	ZalandoDnsNameAnnotation  = "external-dns.alpha.kubernetes.io/hostname"
	ElbTimeoutAnnotationName  = "service.beta.kubernetes.io/aws-load-balancer-connection-idle-timeout"
	ElbTimeoutAnnotationValue = "3600"
	KubeIAmAnnotation         = "iam.amazonaws.com/role"
	ResourceName              = TPRName + "s"
	PodRoleMaster             = "master"
	PodRoleReplica            = "replica"
	SuperuserKeyName          = "superuser"
	ReplicationUserKeyName    = "replication"
	RoleFlagSuperuser         = "SUPERUSER"
	RoleFlagInherit           = "INHERIT"
	RoleFlagLogin             = "LOGIN"
	RoleFlagNoLogin           = "NOLOGIN"
	RoleFlagCreateRole        = "CREATEROLE"
	RoleFlagCreateDB          = "CREATEDB"
)
