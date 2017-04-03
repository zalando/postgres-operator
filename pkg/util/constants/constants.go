package constants

const (
	//Constants
	TPRName                  = "postgresql"
	TPRVendor                = "acid.zalan.do"
	TPRDescription           = "Managed PostgreSQL clusters"
	TPRApiVersion            = "v1"
	DataVolumeName           = "pgdata"
	PasswordLength           = 64
	UserSecretTemplate       = "%s.%s.credentials.%s.%s" // Username, ClusterName, TPRName, TPRVendor
	ZalandoDnsNameAnnotation = "zalando.org/dnsname"
	ResourceName             = TPRName + "s"
)
