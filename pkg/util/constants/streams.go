package constants

// PostgreSQL specific constants
const (
	EventStreamSourcePGType       = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix   = "fes"
	EventStreamSourceAuthType     = "DatabaseAuthenticationSecret"
	EventStreamFlowPgNakadiType   = "PostgresWalToNakadiDataEvent"
	EventStreamFlowPgGenericType  = "PostgresWalToGenericNakadiEvent"
	EventStreamFlowDataTypeColumn = "data_type"
	EventStreamFlowDataOpColumn   = "data_op"
	EventStreamFlowMetadataColumn = "metadata"
	EventStreamFlowDataColumn     = "data"
	EventStreamFlowPayloadColumn  = "payload"
	EventStreamSinkNakadiType     = "Nakadi"
)
