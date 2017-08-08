package constants

// PostgreSQL specific constants
const (
	DataVolumeName    = "pgdata"
	PodRoleMaster     = "master"
	PodRoleReplica    = "replica"
	PostgresDataMount = "/home/postgres/pgdata"
	PostgresDataPath  = PostgresDataMount + "/pgroot"
)
