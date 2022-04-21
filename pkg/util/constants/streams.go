package constants

// PostgreSQL specific constants
const (
	EventStreamCRDApiVersion     = "zalando.org/v1"
	EventStreamCRDKind           = "FabricEventStream"
	EventStreamCRDName           = "fabriceventstreams.zalando.org"
	EventStreamSourcePGType      = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix  = "fes"
	EventStreamSourcePluginType  = "pgoutput"
	EventStreamSourceAuthType    = "DatabaseAuthenticationSecret"
	EventStreamFlowPgGenericType = "PostgresWalToGenericNakadiEvent"
	EventStreamSinkNakadiType    = "Nakadi"
)
