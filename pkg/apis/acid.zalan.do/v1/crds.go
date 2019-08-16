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

// PostgresCRDResourceColumns definition of AdditionalPrinterColumns for postgresql CRD
var PostgresCRDResourceColumns = []apiextv1beta1.CustomResourceColumnDefinition{
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Team",
		Type:        "string",
		Description: "Team responsible for Postgres cluster",
		JSONPath:    ".spec.teamId",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Status",
		Type:        "string",
		Description: "Current status of Postgres cluster",
		JSONPath:    ".status.PostgresClusterStatus",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Instances",
		Type:        "integer",
		Description: "Number of instances per Postgres cluster",
		JSONPath:    ".spec.numberOfInstances",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Volume",
		Type:        "string",
		Description: "Size of the bound volume",
		JSONPath:    ".spec.volume.size",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:     "Age",
		Type:     "date",
		JSONPath: ".metadata.creationTimestamp",
	},
}

// OperatorConfigCRDResourceColumns definition of AdditionalPrinterColumns for OperatorConfiguration CRD
var OperatorConfigCRDResourceColumns = []apiextv1beta1.CustomResourceColumnDefinition{
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Image",
		Type:        "string",
		Description: "Spilo image to be used for Pods",
		JSONPath:    ".configuration.docker_image",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Cluster-Label",
		Type:        "string",
		Description: "Label for K8s resources created by operator",
		JSONPath:    ".configuration.kubernetes.cluster_name_label",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Service-Account",
		Type:        "string",
		Description: "Name of service account to be used",
		JSONPath:    ".configuration.kubernetes.pod_service_account_name",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:        "Min-Instances",
		Type:        "integer",
		Description: "Minimum number of instances per Postgres cluster",
		JSONPath:    ".configuration.min_instances",
	},
	apiextv1beta1.CustomResourceColumnDefinition{
		Name:     "Age",
		Type:     "date",
		JSONPath: ".metadata.creationTimestamp",
	},
}

func buildCRD(name, kind, plural, short string, columns []apiextv1beta1.CustomResourceColumnDefinition) *apiextv1beta1.CustomResourceDefinition {
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
			AdditionalPrinterColumns: columns,
		},
	}
}

// PostgresCRD returns CustomResourceDefinition built from PostgresCRDResource
func PostgresCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(PostgresCRDResouceName,
		PostgresCRDResourceKind,
		PostgresCRDResourcePlural,
		PostgresCRDResourceShort,
		PostgresCRDResourceColumns)
}

// ConfigurationCRD returns CustomResourceDefinition built from OperatorConfigCRDResource
func ConfigurationCRD() *apiextv1beta1.CustomResourceDefinition {
	return buildCRD(OperatorConfigCRDResourceName,
		OperatorConfigCRDResouceKind,
		OperatorConfigCRDResourcePlural,
		OperatorConfigCRDResourceShort,
		OperatorConfigCRDResourceColumns)
}
