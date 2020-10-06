package pooler_interface

//functions of cluster package used in connection_pooler package
type pooler interface {
	(c *Cluster) credentialSecretName(username string) string
	(c *Cluster) serviceAddress(role PostgresRole) string
	(c *Cluster) servicePort(role PostgresRole) string
	generateResourceRequirements(resources acidv1.Resources, defaultResources acidv1.Resources) (*v1.ResourceRequirements, error)
	(c *Cluster) generatePodAnnotations(spec *acidv1.PostgresSpec) map[string]string
	(c *Cluster) ownerReferences() []metav1.OwnerReference
	(c *Cluster) credentialSecretName(username string)
	(c *Cluster) deleteSecrets() error
	specPatch(spec interface{}) ([]byte, error)
	metaAnnotationsPatch(annotations map[string]string) ([]byte, error)
	generateResourceRequirements(resources acidv1.Resources, defaultResources acidv1.Resources) (*v1.ResourceRequirements, error)
	syncResources(a, b *v1.ResourceRequirements) bool
	(c *Cluster) AnnotationsToPropagate(annotations map[string]string) map[string]string
}
