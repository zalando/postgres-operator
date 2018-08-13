package v1

const (
	serviceNameMaxLength   = 63
	clusterNameMaxLength   = serviceNameMaxLength - len("-repl")
	serviceNameRegexString = `^[a-z]([-a-z0-9]*[a-z0-9])?$`

	ClusterStatusUnknown      ClusterStateType = ""
	ClusterStatusCreating     ClusterStateType = "Creating"
	ClusterStatusUpdating     ClusterStateType = "Updating"
	ClusterStatusUpdateFailed ClusterStateType = "UpdateFailed"
	ClusterStatusSyncFailed   ClusterStateType = "SyncFailed"
	ClusterStatusAddFailed    ClusterStateType = "CreateFailed"
	ClusterStatusRunning      ClusterStateType = "Running"
	ClusterStatusInvalid      ClusterStateType = "Invalid"

)

