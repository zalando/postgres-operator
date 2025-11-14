package constants

// PostgreSQL specific constants
const (
	EventStreamCRDApiVersion       = "zalando.org/v1"
	EventStreamCRDKind             = "FabricEventStream"
	EventStreamCRDName             = "fabriceventstreams.zalando.org"
	EventStreamSourcePGType        = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix    = "fes"
	EventStreamSourcePluginType    = "pgoutput"
	EventStreamSourceAuthType      = "DatabaseAuthenticationSecret"
	EventStreamFlowPgGenericType   = "PostgresWalToGenericNakadiEvent"
	EventStreamSinkNakadiType      = "Nakadi"
	EventStreamRecoveryDLQType     = "DeadLetter"
	EventStreamRecoveryIgnoreType  = "Ignore"
	EventStreamRecoveryNoneType    = "None"
	EventStreamRecoverySuffix      = "dead-letter-queue"
	EventStreamCpuAnnotationKey    = "fes.zalando.org/FES_CPU"
	EventStreamMemoryAnnotationKey = "fes.zalando.org/FES_MEMORY"
)
