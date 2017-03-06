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

	PodLabelWaitTimeout    = 10 * time.Minute
	PodDeletionWaitTimeout = 10 * time.Minute

	ResourceName    = TPRName + "s"
	ResyncPeriodTPR = 5 * time.Minute
	ResyncPeriodPod = 5 * time.Minute

	SuperuserName       = "postgres"
	ReplicationUsername = "replication"

	//TODO: move to the operator spec
	EtcdHost           = "etcd-client.default.svc.cluster.local:2379"
	SpiloImage         = "registry.opensource.zalan.do/acid/spilo-9.6:1.2-p12"
	PamRoleName        = "zalandos"
	PamConfiguration   = "https://info.example.com/oauth2/tokeninfo?access_token= uid realm=/employees"
	PasswordLength     = 64
	TeamsAPIUrl        = "https://teams.example.com/api/"
	UserSecretTemplate = "%s.%s.credentials.%s.%s"

	OAuthTokenSecretName = "postgresql-operator"
	ServiceAccountName   = "operator"
)
