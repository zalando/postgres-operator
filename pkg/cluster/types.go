package cluster

// PostgresRole describes role of the node
type PostgresRole string

const (
	// Master role
	Master PostgresRole = "master"

	// Replica role
	Replica PostgresRole = "replica"
)
