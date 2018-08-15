package v1

const (
	serviceNameMaxLength   = 63
	clusterNameMaxLength   = serviceNameMaxLength - len("-repl")
	serviceNameRegexString = `^[a-z]([-a-z0-9]*[a-z0-9])?$`

	ClusterStatusUnknown      PostgresStatus = ""
	ClusterStatusCreating     PostgresStatus = "Creating"
	ClusterStatusUpdating     PostgresStatus = "Updating"
	ClusterStatusUpdateFailed PostgresStatus = "UpdateFailed"
	ClusterStatusSyncFailed   PostgresStatus = "SyncFailed"
	ClusterStatusAddFailed    PostgresStatus = "CreateFailed"
	ClusterStatusRunning      PostgresStatus = "Running"
	ClusterStatusInvalid      PostgresStatus = "Invalid"
)
