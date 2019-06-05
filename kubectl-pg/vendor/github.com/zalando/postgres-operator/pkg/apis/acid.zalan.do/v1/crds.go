package v1

import (
	"github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CRDResource* define names necesssary for the k8s CRD API
const (
	PostgresCRDResourceKind   = "postgresql"
	PostgresCRDResourcePlural = "postgresqls"
	PostgresCRDResouceName    = PostgresCRDResourcePlural + "." + acidzalando.GroupName
	PostgresCRDResourceShort  = "pg"

	OperatorConfigCRDResouceKind    = "OperatorConfiguration"
	OperatorConfigCRDResourcePlural = "operatorconfigurations"
	OperatorConfigCRDResourceName   = OperatorConfigCRDResourcePlural + "." + acidzalando.GroupName
	OperatorConfigCRDResourceShort  = "opconfig"
)

func buildCRD(name, kind, plural, short string) *apiextv1beta1.CustomResourceDefinition {
	return &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group:   SchemeGroupVersion.Group,
			Version: SchemeGroupVersion.Version,
			Names: apiextv1beta1.CustomResourceDefinitionNames{
				Plural:     plural,
				ShortNames: []string{short},
				Kind:       kind,
			},
			Scope: apiextv1beta1.NamespaceScoped,
			Subresources: &apiextv1beta1.CustomResourceSubresources{
				Status: &apiextv1beta1.CustomResourceSubresourceStatus{},
			},
		},
	}
}

// PostgresCRD returns CustomResourceDefinition built from PostgresCRDResource
func PostgresCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(PostgresCRDResouceName,
		PostgresCRDResourceKind,
		PostgresCRDResourcePlural,
		PostgresCRDResourceShort)
}

// ConfigurationCRD returns CustomResourceDefinition built from OperatorConfigCRDResource
func ConfigurationCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(OperatorConfigCRDResourceName,
		OperatorConfigCRDResouceKind,
		OperatorConfigCRDResourcePlural,
		OperatorConfigCRDResourceShort)
}
