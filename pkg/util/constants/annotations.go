package constants

// Names and values in Kubernetes annotation for services, statefulsets and volumes
const (
	ZalandoDNSNameAnnotation           = "external-dns.alpha.kubernetes.io/hostname"
	KubeIAmAnnotation                  = "iam.amazonaws.com/role"
	VolumeStorateProvisionerAnnotation = "pv.kubernetes.io/provisioned-by"
	PostgresqlControllerAnnotationKey  = "acid.zalan.do/controller"
)
