package util

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"regexp"

	"github.com/zalando/postgres-operator/v2/pkg/spec"
)

var pgUsers = []struct {
	in             spec.PgUser
	outmd5         string
	outscramsha256 string
}{{spec.PgUser{
	Name:     "test",
	Password: "password",
	Flags:    []string{},
	MemberOf: []string{}},
	"md587f77988ccb5aa917c93201ba314fcd4", "SCRAM-SHA-256$4096:c2FsdA==$lF4cRm/Jky763CN4HtxdHnjV4Q8AWTNlKvGmEFFU8IQ=:ub8OgRsftnk2ccDMOt7ffHXNcikRkQkq1lh4xaAqrSw="},
	{spec.PgUser{
		Name:     "test",
		Password: "md592f413f3974bdf3799bb6fecb5f9f2c6",
		Flags:    []string{},
		MemberOf: []string{}},
		"md592f413f3974bdf3799bb6fecb5f9f2c6", "md592f413f3974bdf3799bb6fecb5f9f2c6"},
	{spec.PgUser{
		Name:     "test",
		Password: "SCRAM-SHA-256$4096:S1ByZWhvYVV5VDlJNGZoVw==$ozLevu5k0pAQYRrSY+vZhetO6+/oB+qZvuutOdXR94U=:yADwhy0LGloXzh5RaVwLMFyUokwI17VkHVfKVuHu0Zs=",
		Flags:    []string{},
		MemberOf: []string{}},
		"SCRAM-SHA-256$4096:S1ByZWhvYVV5VDlJNGZoVw==$ozLevu5k0pAQYRrSY+vZhetO6+/oB+qZvuutOdXR94U=:yADwhy0LGloXzh5RaVwLMFyUokwI17VkHVfKVuHu0Zs=", "SCRAM-SHA-256$4096:S1ByZWhvYVV5VDlJNGZoVw==$ozLevu5k0pAQYRrSY+vZhetO6+/oB+qZvuutOdXR94U=:yADwhy0LGloXzh5RaVwLMFyUokwI17VkHVfKVuHu0Zs="}}

var prettyDiffTest = []struct {
	inA interface{}
	inB interface{}
	out string
}{
	{[]int{1, 2, 3, 4}, []int{1, 2, 3}, "[]int[4] != []int[3]"},
	{[]int{1, 2, 3, 4}, []int{1, 2, 3, 4}, ""},
}

var isEqualIgnoreOrderTest = []struct {
	inA      []string
	inB      []string
	outEqual bool
}{
	{[]string{"a", "b", "c"}, []string{"a", "b", "c"}, true},
	{[]string{"a", "b", "c"}, []string{"a", "c", "b"}, true},
	{[]string{"a", "b"}, []string{"a", "c", "b"}, false},
	{[]string{"a", "b", "c"}, []string{"a", "d", "c"}, false},
}

var substractTest = []struct {
	inA      []string
	inB      []string
	out      []string
	outEqual bool
}{
	{[]string{"a", "b", "c", "d"}, []string{"a", "b", "c", "d"}, []string{}, true},
	{[]string{"a", "b", "c", "d"}, []string{"a", "bb", "c", "d"}, []string{"b"}, false},
	{[]string{""}, []string{"b"}, []string{""}, false},
	{[]string{"a"}, []string{""}, []string{"a"}, false},
}

var removeStringTest = []struct {
	slice  []string
	item   string
	result []string
}{
	{[]string{"a", "b", "c"}, "b", []string{"a", "c"}},
	{[]string{"a"}, "b", []string{"a"}},
	{[]string{"a"}, "a", []string{}},
	{[]string{}, "a", []string{}},
}

var sliceContaintsTest = []struct {
	slice []string
	item  string
	out   bool
}{
	{[]string{"a", "b", "c"}, "a", true},
	{[]string{"a", "b", "c"}, "d", false},
	{[]string{}, "d", false},
}

var mapContaintsTest = []struct {
	inA map[string]string
	inB map[string]string
	out bool
}{
	{map[string]string{"1": "a", "2": "b", "3": "c", "4": "c"}, map[string]string{"1": "a", "2": "b", "3": "c"}, true},
	{map[string]string{"1": "a", "2": "b", "3": "c", "4": "c"}, map[string]string{"1": "a", "2": "b", "3": "d"}, false},
	{map[string]string{}, map[string]string{}, true},
	{map[string]string{"3": "c", "4": "c"}, map[string]string{"1": "a", "2": "b", "3": "c"}, false},
	{map[string]string{"3": "c", "4": "c"}, map[string]string{}, true},
}

var substringMatch = []struct {
	inRegex *regexp.Regexp
	inStr   string
	out     map[string]string
}{
	{regexp.MustCompile(`aaaa (?P<num>\d+) bbbb`), "aaaa 123 bbbb", map[string]string{"num": "123"}},
	{regexp.MustCompile(`aaaa (?P<num>\d+) bbbb`), "a aa 123 bbbb", nil},
	{regexp.MustCompile(`aaaa \d+ bbbb`), "aaaa 123 bbbb", nil},
	{regexp.MustCompile(`aaaa (\d+) bbbb`), "aaaa 123 bbbb", nil},
}

var requestIsSmallerQuantityTests = []struct {
	request string
	limit   string
	out     bool
}{
	{"1G", "2G", true},
	{"1G", "1Gi", true}, // G is 1000^3 bytes, Gi is 1024^3 bytes
	{"1024Mi", "1G", false},
	{"1e9", "1G", false}, // 1e9 bytes == 1G
}

func TestRandomPassword(t *testing.T) {
	const pwdLength = 10
	pwd := RandomPassword(pwdLength)
	if a := len(pwd); a != pwdLength {
		t.Errorf("password length expected: %d, got: %d", pwdLength, a)
	}
}

func TestNameFromMeta(t *testing.T) {
	meta := metav1.ObjectMeta{
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
		e := NewEncryptor("md5")
		pwd := e.PGUserPassword(tt.in)
		if pwd != tt.outmd5 {
			t.Errorf("PgUserPassword expected: %q, got: %q", tt.outmd5, pwd)
		}
		e = NewEncryptor("scram-sha-256")
		e.random = func(n int) string { return "salt" }
		pwd = e.PGUserPassword(tt.in)
		if pwd != tt.outscramsha256 {
			t.Errorf("PgUserPassword expected: %q, got: %q", tt.outscramsha256, pwd)
		}
	}
}

func TestPGUserPasswordUpToDate(t *testing.T) {
	user := spec.PgUser{Name: "someuser", Password: "password"}
	md5Hash := NewEncryptor("md5").PGUserPassword(user)
	// real generation path: random salt in the verifier
	scramHash := NewEncryptor("scram-sha-256").PGUserPassword(user)

	tests := []struct {
		name       string
		user       spec.PgUser
		stored     string
		encryption string
		want       bool
	}{
		{"scram verifier matches its plaintext", user, scramHash, "scram-sha-256", true},
		{"a differently salted verifier of the same password matches", user, NewEncryptor("scram-sha-256").PGUserPassword(user), "scram-sha-256", true},
		{"scram verifier of another password does not match", spec.PgUser{Name: "someuser", Password: "different"}, scramHash, "scram-sha-256", false},
		{"md5 hash matches its plaintext", user, md5Hash, "md5", true},
		{"md5 hash of another password does not match", spec.PgUser{Name: "someuser", Password: "different"}, md5Hash, "md5", false},
		{"stored md5 is outdated when scram is configured", user, md5Hash, "scram-sha-256", false},
		{"stored scram is outdated when md5 is configured", user, scramHash, "md5", false},
		{"pre-hashed desired password compares verbatim", spec.PgUser{Name: "someuser", Password: md5Hash}, md5Hash, "md5", true},
		{"pre-hashed desired password differs from stored", spec.PgUser{Name: "someuser", Password: md5Hash}, scramHash, "md5", false},
		{"empty desired password matches empty stored", spec.PgUser{Name: "someuser"}, "", "scram-sha-256", true},
		{"empty desired password differs from stored hash", spec.PgUser{Name: "someuser"}, scramHash, "scram-sha-256", false},
		{"malformed stored hash is outdated", user, "not-a-hash", "scram-sha-256", false},
		{"truncated scram verifier is outdated", user, "SCRAM-SHA-256$4096:c2FsdA==", "scram-sha-256", false},
		{"scram verifier with bad base64 is outdated", user, "SCRAM-SHA-256$4096:!!$aaaa:bbbb", "scram-sha-256", false},
	}
	for _, tt := range tests {
		if got := PGUserPasswordUpToDate(tt.user, tt.stored, tt.encryption); got != tt.want {
			t.Errorf("%s: PGUserPasswordUpToDate expected %v, got %v", tt.name, tt.want, got)
		}
	}
}

func TestPrettyDiff(t *testing.T) {
	for _, tt := range prettyDiffTest {
		if actual := PrettyDiff(tt.inA, tt.inB); actual != tt.out {
			t.Errorf("PrettyDiff expected: %q, got: %q", tt.out, actual)
		}
	}
}

func TestIsEqualIgnoreOrder(t *testing.T) {
	for _, tt := range isEqualIgnoreOrderTest {
		actualEqual := IsEqualIgnoreOrder(tt.inA, tt.inB)
		if actualEqual != tt.outEqual {
			t.Errorf("IsEqualIgnoreOrder expected: %t, got: %t", tt.outEqual, actualEqual)
		}
	}
}

func TestStringSliceReplaceElement(t *testing.T) {
	testSlice := []string{"a", "b", "c"}
	testSlice = StringSliceReplaceElement(testSlice, "b", "d")
	if !SliceContains(testSlice, "d") {
		t.Errorf("testSlide item not replaced: %v", testSlice)
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

func TestFindNamedStringSubmatch(t *testing.T) {
	for _, tt := range substringMatch {
		actualRes := FindNamedStringSubmatch(tt.inRegex, tt.inStr)
		if !reflect.DeepEqual(actualRes, tt.out) {
			t.Errorf("FindNamedStringSubmatch expected: %#v, got: %#v", tt.out, actualRes)
		}
	}
}

func TestRemoveString(t *testing.T) {
	for _, tt := range removeStringTest {
		res := RemoveString(tt.slice, tt.item)
		if !IsEqualIgnoreOrder(res, tt.result) {
			t.Errorf("RemoveString expected: %#v, got: %#v", tt.result, res)
		}
	}
}

func TestSliceContains(t *testing.T) {
	for _, tt := range sliceContaintsTest {
		res := SliceContains(tt.slice, tt.item)
		if res != tt.out {
			t.Errorf("SliceContains expected: %#v, got: %#v", tt.out, res)
		}
	}
}

func TestMapContains(t *testing.T) {
	for _, tt := range mapContaintsTest {
		res := MapContains(tt.inA, tt.inB)
		if res != tt.out {
			t.Errorf("MapContains expected: %#v, got: %#v", tt.out, res)
		}
	}
}

func TestIsSmallerQuantity(t *testing.T) {
	for _, tt := range requestIsSmallerQuantityTests {
		res, err := IsSmallerQuantity(tt.request, tt.limit)
		if err != nil {
			t.Errorf("IsSmallerQuantity returned unexpected error: %#v", err)
		}
		if res != tt.out {
			t.Errorf("IsSmallerQuantity expected: %#v, got: %#v", tt.out, res)
		}
	}
}

/*
func TestNiceDiff(t *testing.T) {
	o := "a\nb\nc\n"
	n := "b\nd\n"
	d := nicediff.Diff(o, n, true)
	t.Log(d)
	// t.Errorf("Lets see output")
}
*/
