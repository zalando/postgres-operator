package constants

// PostgreSQL specific constants
const (
	EventStreamSourcePGType        = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix    = "fes_"
	EventStreamSourceAuthType      = "DatabaseAuthenticationSecret"
	EventStreamFlowPgNakadiType    = "PostgresWalToNakadiDataEvent"
	EventStreamFlowPgGenericType   = "PostgresWalToGenericNakadiEvent"
	EventStreamFlowPgApiType       = "PostgresWalToApiCallHomeEvent"
	EventStreamFlowDataTypeColumn  = "data_type"
	EventStreamFlowDataOpColumn    = "data_op"
	EventStreamFlowMetadataColumn  = "metadata"
	EventStreamFlowDataColumn      = "data"
	EventStreamFlowPayloadColumn   = "payload"
	EventStreamSinkNakadiType      = "Nakadi"
	EventStreamSinkSqsStandardType = "SqsStandard"
	EventStreamSinkSqsFifoType     = "SqsFifo"
)
