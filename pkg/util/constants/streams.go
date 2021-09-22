package constants

// PostgreSQL specific constants
const (
	EventStreamSourcePGType      = "PostgresLogicalReplication"
	EventStreamSourceSlotPrefix  = "fes"
	EventStreamSourceAuthType    = "DatabaseAuthenticationSecret"
	EventStreamFlowPgGenericType = "PostgresWalToGenericNakadiEvent"
	EventStreamSinkNakadiType    = "Nakadi"
)
