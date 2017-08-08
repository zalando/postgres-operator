package cluster

type postgresRole string

const (
	master  postgresRole = "master"
	replica postgresRole = "replica"
)
