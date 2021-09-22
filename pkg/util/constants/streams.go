package constants

// PostgreSQL specific constants
const (
	EventStreamSourceCRDKind     = "FabricEventStream"
	EventStreamSourceCRDName     = "fabriceventstreams.zalando.org"
	EventStreamSourcePGType      = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix  = "fes"
	EventStreamSourceAuthType    = "DatabaseAuthenticationSecret"
	EventStreamFlowPgGenericType = "PostgresWalToGenericNakadiEvent"
	EventStreamSinkNakadiType    = "Nakadi"
)
