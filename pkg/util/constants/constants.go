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
	ZalandoDnsNameAnnotation  = "zalando.org/dnsname"
	KubeIAmAnnotation         = "iam.amazonaws.com/role"
	ResourceName              = TPRName + "s"
	PodRoleMaster             = "master"
	PodRoleReplica            = "replica"
	ApplicationNameLabel      = "application"
	ApplicationNameLabelValue = "spilo"
	SpiloRoleLabel            = "spilo-role"
	ClusterNameLabel          = "version"
)
