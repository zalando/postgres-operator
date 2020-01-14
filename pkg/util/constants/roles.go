package constants

// Roles specific constants
const (
	PasswordLength            = 64
	SuperuserKeyName          = "superuser"
	ConnectionPoolUserKeyName = "pooler"
	ReplicationUserKeyName    = "replication"
	RoleFlagSuperuser         = "SUPERUSER"
	RoleFlagInherit           = "INHERIT"
	RoleFlagLogin             = "LOGIN"
	RoleFlagNoLogin           = "NOLOGIN"
	RoleFlagCreateRole        = "CREATEROLE"
	RoleFlagCreateDB          = "CREATEDB"
	RoleFlagReplication       = "REPLICATION"
	RoleFlagByPassRLS         = "BYPASSRLS"
)
