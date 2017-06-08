package constants

import "time"

// General kubernetes-related constants
const (
	ListClustersURITemplate     = "/apis/" + TPRVendor + "/" + TPRApiVersion + "/namespaces/%s/" + ResourceName       // Namespace
	WatchClustersURITemplate    = "/apis/" + TPRVendor + "/" + TPRApiVersion + "/watch/namespaces/%s/" + ResourceName // Namespace
	K8sVersion                  = "v1"
	K8sAPIPath                  = "/api"
	StatefulsetDeletionInterval = 1 * time.Second
	StatefulsetDeletionTimeout  = 30 * time.Second

	QueueResyncPeriodPod = 5 * time.Minute
	QueueResyncPeriodTPR = 5 * time.Minute
)
