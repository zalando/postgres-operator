package constants

// Roles specific constants
const (
	PasswordLength         = 64
	UserSecretTemplate     = "%s.%s.credentials." + TPRKind + "." + TPRGroup // Username, ClusterName
	SuperuserKeyName       = "superuser"
	ReplicationUserKeyName = "replication"
	RoleFlagSuperuser      = "SUPERUSER"
	RoleFlagInherit        = "INHERIT"
	RoleFlagLogin          = "LOGIN"
	RoleFlagNoLogin        = "NOLOGIN"
	RoleFlagCreateRole     = "CREATEROLE"
	RoleFlagCreateDB       = "CREATEDB"
	RoleFlagReplication    = "REPLICATION"
	RoleFlagByPassRLS      = "BYPASSRLS"
)
