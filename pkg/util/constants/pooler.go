package constants

// Connection pool specific constants
const (
	ConnectionPoolUserName             = "pooler"
	ConnectionPoolSchemaName           = "pooler"
	ConnectionPoolDefaultType          = "pgbouncer"
	ConnectionPoolDefaultMode          = "transaction"
	ConnectionPoolDefaultCpuRequest    = "100m"
	ConnectionPoolDefaultCpuLimit      = "100m"
	ConnectionPoolDefaultMemoryRequest = "100Mi"
	ConnectionPoolDefaultMemoryLimit   = "100Mi"

	ConnPoolContainer = 0
)
