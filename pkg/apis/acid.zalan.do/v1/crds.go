package v1

import (
	_ "embed"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// CRDResource* define names necesssary for the k8s CRD API
const (
	PostgresCRDResourceKind       = "postgresql"
	OperatorConfigCRDResourceKind = "OperatorConfiguration"
)

//go:embed postgresql.crd.yaml
var postgresqlCRDYAML []byte

// PostgresCRD returns CustomResourceDefinition built from PostgresCRDResource
func PostgresCRD(crdCategories []string) (*apiextv1.CustomResourceDefinition, error) {
	var crd apiextv1.CustomResourceDefinition
	err := yaml.Unmarshal(postgresqlCRDYAML, &crd)
	if err != nil {
		return nil, err
	}

	crd.Spec.Names.Categories = crdCategories

	return &crd, nil
}

//go:embed operatorconfiguration.crd.yaml
var operatorConfigurationCRDYAML []byte

// OperatorConfigurationCRD returns CustomResourceDefinition built from OperatorConfigurationCRDResource
func OperatorConfigurationCRD(crdCategories []string) (*apiextv1.CustomResourceDefinition, error) {
	var crd apiextv1.CustomResourceDefinition
	err := yaml.Unmarshal(operatorConfigurationCRDYAML, &crd)
	if err != nil {
		return nil, err
	}

	crd.Spec.Names.Categories = crdCategories

	return &crd, nil
}
