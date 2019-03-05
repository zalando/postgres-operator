package util

import (
	"crypto/md5" // #nosec we need it to for PostgreSQL md5 passwords
	"encoding/hex"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/motomux/pretty"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zalando/postgres-operator/pkg/spec"
)

const (
	md5prefix = "md5"
)

var passwordChars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func init() {
	rand.Seed(time.Now().Unix())
}

// RandomPassword generates random alphanumeric password of a given length.
func RandomPassword(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = passwordChars[rand.Intn(len(passwordChars))]
	}

	return string(b)
}

// NameFromMeta converts a metadata object to the NamespacedName name representation.
func NameFromMeta(meta metav1.ObjectMeta) spec.NamespacedName {
	return spec.NamespacedName{
		Namespace: meta.Namespace,
		Name:      meta.Name,
	}
}

// PGUserPassword is used to generate md5 password hash for a given user. It does nothing for already hashed passwords.
func PGUserPassword(user spec.PgUser) string {
	if (len(user.Password) == md5.Size*2+len(md5prefix) && user.Password[:3] == md5prefix) || user.Password == "" {
		// Avoid processing already encrypted or empty passwords
		return user.Password
	}
	s := md5.Sum([]byte(user.Password + user.Name)) // #nosec, using md5 since PostgreSQL uses it for hashing passwords.
	return md5prefix + hex.EncodeToString(s[:])
}

// Diff returns diffs between 2 objects
func Diff(a, b interface{}) []string {
	return pretty.Diff(a, b)
}

// PrettyDiff shows the diff between 2 objects in an easy to understand format. It is mainly used for debugging output.
func PrettyDiff(a, b interface{}) string {
	return strings.Join(Diff(a, b), "\n")
}

// SubstractStringSlices finds elements in a that are not in b and return them as a result slice.
func SubstractStringSlices(a []string, b []string) (result []string, equal bool) {
	// Slices are assumed to contain unique elements only
OUTER:
	for _, vala := range a {
		for _, valb := range b {
			if vala == valb {
				continue OUTER
			}
		}
		result = append(result, vala)
	}
	return result, len(result) == 0
}

// FindNamedStringSubmatch returns a map of strings holding the text of the matches of the r regular expression
func FindNamedStringSubmatch(r *regexp.Regexp, s string) map[string]string {
	matches := r.FindStringSubmatch(s)
	grNames := r.SubexpNames()

	if matches == nil {
		return nil
	}

	groupMatches := 0
	res := make(map[string]string, len(grNames))
	for i, n := range grNames {
		if n == "" {
			continue
		}

		res[n] = matches[i]
		groupMatches++
	}

	if groupMatches == 0 {
		return nil
	}

	return res
}

// MapContains returns true if and only if haystack contains all the keys from the needle with matching corresponding values
func MapContains(haystack, needle map[string]string) bool {
	if len(haystack) < len(needle) {
		return false
	}

	for k, v := range needle {
		v2, ok := haystack[k]
		if !ok || v2 != v {
			return false
		}
	}

	return true
}

// Coalesce returns the first argument if it is not null, otherwise the second one.
func Coalesce(val, defaultVal string) string {
	if val == "" {
		return defaultVal
	}
	return val
}

// RequestIsSmallerThanLimit : ...
func RequestIsSmallerThanLimit(requestStr, limitStr string) (bool, error) {

	request, err := resource.ParseQuantity(requestStr)
	if err != nil {
		return false, fmt.Errorf("could not parse memory request %v : %v", requestStr, err)
	}

	limit, err2 := resource.ParseQuantity(limitStr)
	if err2 != nil {
		return false, fmt.Errorf("could not parse memory limit %v : %v", limitStr, err2)
	}

	return request.Cmp(limit) == -1, nil
}
