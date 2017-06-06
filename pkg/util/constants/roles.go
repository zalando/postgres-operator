package constants

const (
	PasswordLength         = 64
	UserSecretTemplate     = "%s.%s.credentials." + TPRName + "." + TPRVendor // Username, ClusterName
	SuperuserKeyName       = "superuser"
	ReplicationUserKeyName = "replication"
	RoleFlagSuperuser      = "SUPERUSER"
	RoleFlagInherit        = "INHERIT"
	RoleFlagLogin          = "LOGIN"
	RoleFlagNoLogin        = "NOLOGIN"
	RoleFlagCreateRole     = "CREATEROLE"
	RoleFlagCreateDB       = "CREATEDB"
)
