package constants

import "time"

// PostgreSQL specific constants
const (
	DataVolumeName    = "pgdata"
	PostgresDataMount = "/home/postgres/pgdata"
	PostgresDataPath  = PostgresDataMount + "/pgroot"

	PatroniPGParametersParameterName = "parameters"

	PostgresConnectRetryTimeout = 2 * time.Minute
	PostgresConnectTimeout      = 15 * time.Second

	ShmVolumeName = "dshm"
	ShmVolumePath = "/dev/shm"
)
