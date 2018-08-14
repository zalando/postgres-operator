package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/zalando-incubator/postgres-operator/pkg/apis/acid.zalan.do"
)

const (
	PostgresCRDResourceKind   = "postgresql"
	PostgresCRDResourcePlural = "postgresqls"
	PostgresCRDResouceName    = PostgresCRDResourcePlural + "." + acidzalando.GroupName
	PostgresCRDResourceShort  = "pg"

	OperatorConfigCRDResouceKind    = "OperatorConfiguration"
	OperatorConfigCRDResourcePlural = "operatorconfigurations"
	OperatorConfigCRDResourceName   = OperatorConfigCRDResourcePlural + "." + acidzalando.GroupName
	OperatorConfigCRDResourceShort  = "opconfig"

	APIVersion = "v1"
)

var (
	// localSchemeBuilder and AddToScheme will stay in k8s.io/kubernetes.
	SchemeBuilder      runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder
	AddToScheme        = localSchemeBuilder.AddToScheme
	SchemeGroupVersion = schema.GroupVersion{Group: acidzalando.GroupName, Version: APIVersion}
)

func init() {
	// We only register manually written functions here. The registration of the
	// generated functions takes place in the generated files. The separation
	// makes the code compile even when the generated files are missing.
	localSchemeBuilder.Register(addKnownTypes)
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	// AddKnownType assumes derives the type kind from the type name, which is always uppercase.
	// For our CRDs we use lowercase names historically, therefore we have to supply the name separately.
	// TODO: User uppercase CRDResourceKind of our types in the next major API version
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("postgresql"), &Postgresql{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("postgresqlList"), &PostgresqlList{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("OperatorConfiguration"),
		&OperatorConfiguration{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("OperatorConfigurationList"),
		&OperatorConfigurationList{})
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
