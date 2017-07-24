package constants

// Different properties of the PostgreSQL Third Party Resources
const (
	TPRKind        = "postgresql"
	TPRGroup       = "acid.zalan.do"
	TPRDescription = "Managed PostgreSQL clusters"
	TPRApiVersion  = "v1"
	TPRName        = TPRKind + "." + TPRGroup
	ResourceName   = TPRKind + "s"
)
