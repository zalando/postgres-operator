package v1

// 	ClusterStatusUnknown etc : status of a Postgres cluster known to the operator
const (
	ClusterStatusUnknown      PostgresClusterStatus = ""
	ClusterStatusCreating     PostgresClusterStatus = "Creating"
	ClusterStatusUpdating     PostgresClusterStatus = "Updating"
	ClusterStatusUpdateFailed PostgresClusterStatus = "UpdateFailed"
	ClusterStatusSyncFailed   PostgresClusterStatus = "SyncFailed"
	ClusterStatusAddFailed    PostgresClusterStatus = "CreateFailed"
	ClusterStatusRunning      PostgresClusterStatus = "Running"
	ClusterStatusInvalid      PostgresClusterStatus = "Invalid"
)

const (
	serviceNameMaxLength   = 63
	clusterNameMaxLength   = serviceNameMaxLength - len("-repl")
	serviceNameRegexString = `^[a-z]([-a-z0-9]*[a-z0-9])?$`
)
