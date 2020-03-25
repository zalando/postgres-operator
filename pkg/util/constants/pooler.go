package constants

// Connection pool specific constants
const (
	ConnectionPoolUserName             = "pooler"
	ConnectionPoolSchemaName           = "pooler"
	ConnectionPoolDefaultType          = "pgbouncer"
	ConnectionPoolDefaultMode          = "transaction"
	ConnectionPoolDefaultCpuRequest    = "500m"
	ConnectionPoolDefaultCpuLimit      = "1"
	ConnectionPoolDefaultMemoryRequest = "100Mi"
	ConnectionPoolDefaultMemoryLimit   = "100Mi"

	ConnPoolContainer            = 0
	ConnPoolMaxDBConnections     = 60
	ConnPoolMaxClientConnections = 10000
	ConnPoolMinInstances         = 2
)
