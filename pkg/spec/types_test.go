package spec

import (
	"bytes"
	"testing"
)

const (
	mockOperatorNamespace = "acid"
)

var nnTests = []struct {
	s               string
	expected        NamespacedName
	expectedMarshal []byte
}{
	{`acid/cluster`, NamespacedName{Namespace: mockOperatorNamespace, Name: "cluster"}, []byte(`"acid/cluster"`)},
	{`/name`, NamespacedName{Namespace: mockOperatorNamespace, Name: "name"}, []byte(`"acid/name"`)},
	{`test`, NamespacedName{Namespace: mockOperatorNamespace, Name: "test"}, []byte(`"acid/test"`)},
}

var nnErr = []string{"test/", "/", "", "//"}

func TestNamespacedNameDecode(t *testing.T) {

	for _, tt := range nnTests {
		var actual NamespacedName
		err := actual.DecodeWorker(tt.s, mockOperatorNamespace)
		if err != nil {
			t.Errorf("decode error: %v", err)
		}
		if actual != tt.expected {
			t.Errorf("expected: %v, got %#v", tt.expected, actual)
		}
	}

}

func TestNamespacedNameMarshal(t *testing.T) {
	for _, tt := range nnTests {
		var actual NamespacedName

		m, err := actual.MarshalJSON()
		if err != nil {
			t.Errorf("marshal error: %v", err)
		}
		if bytes.Equal(m, tt.expectedMarshal) {
			t.Errorf("expected marshal: %v, got %#v", tt.expected, actual)
		}
	}
}

func TestNamespacedNameError(t *testing.T) {
	for _, tt := range nnErr {
		var actual NamespacedName
		err := actual.DecodeWorker(tt, mockOperatorNamespace)
		if err == nil {
			t.Errorf("error expected for %q, got: %#v", tt, actual)
		}
	}
}
