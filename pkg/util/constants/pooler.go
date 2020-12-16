package constants

// Connection pooler specific constants
const (
	ConnectionPoolerUserName             = "pooler"
	ConnectionPoolerSchemaName           = "pooler"
	ConnectionPoolerDefaultType          = "pgbouncer"
	ConnectionPoolerDefaultMode          = "transaction"
	ConnectionPoolerDefaultCpuRequest    = "500m"
	ConnectionPoolerDefaultCpuLimit      = "1"
	ConnectionPoolerDefaultMemoryRequest = "100Mi"
	ConnectionPoolerDefaultMemoryLimit   = "100Mi"

	ConnectionPoolerContainer            = 0
	ConnectionPoolerMaxDBConnections     = 60
	ConnectionPoolerMaxClientConnections = 10000
	ConnectionPoolerMinInstances         = 1
)
