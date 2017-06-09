package types

import (
	"fmt"
	"strings"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/types"
)

// NamespacedName describes the namespace/name pairs used in Kubernetes names.
type NamespacedName types.NamespacedName

func (n NamespacedName) String() string {
	return types.NamespacedName(n).String()
}

// MarshalJSON defines marshaling rule for the namespaced name type.
func (n NamespacedName) MarshalJSON() ([]byte, error) {
	return []byte("\"" + n.String() + "\""), nil
}

// Decode converts a (possibly unqualified) string into the namespaced name object.
func (n *NamespacedName) Decode(value string) error {
	name := types.NewNamespacedNameFromString(value)

	if strings.Trim(value, string(types.Separator)) != "" && name == (types.NamespacedName{}) {
		name.Name = value
		name.Namespace = v1.NamespaceDefault
	} else if name.Namespace == "" {
		name.Namespace = v1.NamespaceDefault
	}

	if name.Name == "" {
		return fmt.Errorf("Incorrect namespaced name")
	}

	*n = NamespacedName(name)

	return nil
}
