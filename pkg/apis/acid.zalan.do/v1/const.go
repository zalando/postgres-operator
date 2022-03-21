package v1

// 	ClusterStatusUnknown etc : status of a Postgres cluster known to the operator
const (
	ClusterStatusUnknown      = ""
	ClusterStatusCreating     = "Creating"
	ClusterStatusUpdating     = "Updating"
	ClusterStatusUpdateFailed = "UpdateFailed"
	ClusterStatusSyncFailed   = "SyncFailed"
	ClusterStatusAddFailed    = "CreateFailed"
	ClusterStatusRunning      = "Running"
	ClusterStatusInvalid      = "Invalid"
)

const (
	serviceNameMaxLength   = 63
	clusterNameMaxLength   = serviceNameMaxLength - len("-repl")
	serviceNameRegexString = `^[a-z]([-a-z0-9]*[a-z0-9])?$`
)
