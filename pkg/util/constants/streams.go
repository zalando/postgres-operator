package constants

// PostgreSQL specific constants
const (
	FESsuffix                     = "-event-streams"
	EventStreamSourcePGType       = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix   = "fes_"
	EventStreamSourceAuthType     = "DatabaseAuthenticationSecret"
	EventStreamFlowPgNakadiType   = "PostgresWalToNakadiDataEvent"
	EventStreamFlowPgApiType      = "PostgresWalToApiCallHomeEvent"
	EventStreamFlowDataTypeColumn = "data_type"
	EventStreamFlowDataOpColumn   = "data_op"
	EventStreamFlowMetadataColumn = "metadata"
	EventStreamFlowDataColumn     = "data"
	EventStreamSinkNakadiType     = "Nakadi"
	EventStreamSinkSqsType        = "Sqs"
)
