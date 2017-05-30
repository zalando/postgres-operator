package constants

import "time"

const (
	ListClustersURITemplate     = "/apis/" + TPRVendor + "/" + TPRApiVersion + "/namespaces/%s/" + ResourceName       // Namespace
	WatchClustersURITemplate    = "/apis/" + TPRVendor + "/" + TPRApiVersion + "/watch/namespaces/%s/" + ResourceName // Namespace
	K8sVersion                  = "v1"
	K8sAPIPath                  = "/api"
	StatefulsetDeletionInterval = 1 * time.Second
	StatefulsetDeletionTimeout  = 30 * time.Second
)
