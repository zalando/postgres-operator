package cluster

type PostgresRole string

const (
	Master  PostgresRole = "master"
	Replica PostgresRole = "replica"
)
