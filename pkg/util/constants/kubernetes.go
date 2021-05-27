package constants

import "time"

// General kubernetes-related constants
const (
	PostgresContainerName = "postgres"
	K8sAPIPath            = "/apis"

	QueueResyncPeriodPod  = 5 * time.Minute
	QueueResyncPeriodTPR  = 5 * time.Minute
	QueueResyncPeriodNode = 5 * time.Minute
)
