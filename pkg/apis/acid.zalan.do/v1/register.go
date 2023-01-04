package v1

import (
	acidzalando "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// APIVersion of the `postgresql` and `operator` CRDs
const (
	APIVersion = "v1"
)

var (
	// localSchemeBuilder and AddToScheme will stay in k8s.io/kubernetes.

	// SchemeBuilder : An instance of runtime.SchemeBuilder, global for this package
	SchemeBuilder      runtime.SchemeBuilder
	localSchemeBuilder = &SchemeBuilder
	//AddToScheme is localSchemeBuilder.AddToScheme
	AddToScheme = localSchemeBuilder.AddToScheme
	//SchemeGroupVersion has GroupName and APIVersion
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
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("PostgresTeam"), &PostgresTeam{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("PostgresTeamList"), &PostgresTeamList{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("OperatorConfiguration"),
		&OperatorConfiguration{})
	scheme.AddKnownTypeWithName(SchemeGroupVersion.WithKind("OperatorConfigurationList"),
		&OperatorConfigurationList{})
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
