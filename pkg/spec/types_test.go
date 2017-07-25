package spec

import (
	"bytes"
	"testing"
)

var nnTests = []struct {
	s               string
	expected        NamespacedName
	expectedMarshal []byte
}{
	{`acid/cluster`, NamespacedName{Namespace: "acid", Name: "cluster"}, []byte(`"acid/cluster"`)},
	{`/name`, NamespacedName{Namespace: "default", Name: "name"}, []byte(`"default/name"`)},
	{`test`, NamespacedName{Namespace: "default", Name: "test"}, []byte(`"default/test"`)},
}

var nnErr = []string{"test/", "/", "", "//"}

func TestNamespacedNameDecode(t *testing.T) {
	for _, tt := range nnTests {
		var actual NamespacedName
		err := actual.Decode(tt.s)
		if err != nil {
			t.Errorf("Decode error: %v", err)
		}
		if actual != tt.expected {
			t.Errorf("Expected: %v, got %#v", tt.expected, actual)
		}
	}
}

func TestNamespacedNameMarshal(t *testing.T) {
	for _, tt := range nnTests {
		var actual NamespacedName

		m, err := actual.MarshalJSON()
		if err != nil {
			t.Errorf("Marshal error: %v", err)
		}
		if bytes.Equal(m, tt.expectedMarshal) {
			t.Errorf("Expected marshal: %v, got %#v", tt.expected, actual)
		}
	}
}

func TestNamespacedNameError(t *testing.T) {
	for _, tt := range nnErr {
		var actual NamespacedName
		err := actual.Decode(tt)
		if err == nil {
			t.Errorf("Error expected for %q, got: %#v", tt, actual)
		}
	}
}
