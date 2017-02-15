package constants

import "time"

const (
	TPRName               = "postgresql"
	TPRVendor             = "acid.zalan.do"
	TPRDescription        = "Managed PostgreSQL clusters"
	TPRReadyWaitInterval  = 3 * time.Second
	TPRReadyWaitTimeout   = 30 * time.Second
	TPRApiVersion         = "v1"
	ResourceCheckInterval = 3 * time.Second
	ResourceCheckTimeout  = 10 * time.Minute

	ResourceName = TPRName + "s"
	ResyncPeriod = 5 * time.Minute

	EtcdHost    = "etcd-client.default.svc.cluster.local:2379" //TODO: move to the operator spec
	SpiloImage  = "registry.opensource.zalan.do/acid/spilo-9.6:1.2-p11"
	PamRoleName = "zalandos"

	PasswordLength = 64
)
