package cluster

type postgresRole string

const (
	Master  postgresRole = "master"
	Replica postgresRole = "replica"
)
