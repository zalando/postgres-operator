package util

import (
	"reflect"
	"testing"

	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

var pgUsers = []struct {
	in  spec.PgUser
	out string
}{{spec.PgUser{
	Name:     "test",
	Password: "password",
	Flags:    []string{},
	MemberOf: []string{}},
	"md587f77988ccb5aa917c93201ba314fcd4"},
	{spec.PgUser{
		Name:     "test",
		Password: "md592f413f3974bdf3799bb6fecb5f9f2c6",
		Flags:    []string{},
		MemberOf: []string{}},
		"md592f413f3974bdf3799bb6fecb5f9f2c6"}}

var prettyDiffTest = []struct {
	inA interface{}
	inB interface{}
	out string
}{
	{[]int{1, 2, 3, 4}, []int{1, 2, 3}, "[]int[4] != []int[3]"},
	{[]int{1, 2, 3, 4}, []int{1, 2, 3, 4}, ""},
}

var substractTest = []struct {
	inA      []string
	inB      []string
	out      []string
	outEqual bool
}{
	{[]string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d"}, []string{}, true},
	{[]string{"a", "b", "c", "d"}, []string{"a", "bb", "c", "d"}, []string{"b"}, false},
}

func TestRandomPassword(t *testing.T) {
	const pwdLength = 10
	pwd := RandomPassword(pwdLength)
	if a := len(pwd); a != pwdLength {
		t.Errorf("Password length expected: %d, got: %d", pwdLength, a)
	}
}

func TestNameFromMeta(t *testing.T) {
	meta := v1.ObjectMeta{
		Name:      "testcluster",
		Namespace: "default",
	}

	expected := spec.NamespacedName{
		Name:      "testcluster",
		Namespace: "default",
	}

	actual := NameFromMeta(meta)
	if actual != expected {
		t.Errorf("NameFromMeta expected: %#v, got: %#v", expected, actual)
	}
}

func TestPGUserPassword(t *testing.T) {
	for _, tt := range pgUsers {
		pwd := PGUserPassword(tt.in)
		if pwd != tt.out {
			t.Errorf("PgUserPassword expected: %s, got: %s", tt.out, pwd)
		}
	}
}

func TestPrettyDiff(t *testing.T) {
	for _, tt := range prettyDiffTest {
		if actual := PrettyDiff(tt.inA, tt.inB); actual != tt.out {
			t.Errorf("PrettyDiff expected: %s, got: %s", tt.out, actual)
		}
	}
}

func TestSubstractSlices(t *testing.T) {
	for _, tt := range substractTest {
		actualRes, actualEqual := SubstractStringSlices(tt.inA, tt.inB)
		if actualEqual != tt.outEqual {
			t.Errorf("SubstractStringSlices expected equal: %t, got: %t", tt.outEqual, actualEqual)
		}

		if len(actualRes) == 0 && len(tt.out) == 0 {
			continue
		} else if !reflect.DeepEqual(actualRes, tt.out) {
			t.Errorf("SubstractStringSlices expected res: %v, got: %v", tt.out, actualRes)
		}
	}
}
