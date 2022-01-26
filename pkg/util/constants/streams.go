package constants

// PostgreSQL specific constants
const (
	EventStreamSourceCRDKind     = "FabricEventStream"
	EventStreamSourceCRDName     = "fabriceventstreams.zalando.org"
	EventStreamSourcePGType      = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix  = "fes"
	EventStreamSourcePluginType  = "pgoutput"
	EventStreamSourceAuthType    = "DatabaseAuthenticationSecret"
	EventStreamFlowPgGenericType = "PostgresWalToGenericNakadiEvent"
	EventStreamSinkNakadiType    = "Nakadi"
)
