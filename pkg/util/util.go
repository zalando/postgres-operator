package util

import (
	"crypto/md5"
	"encoding/hex"
	"math/rand"
	"strings"
	"time"

	"github.com/motomux/pretty"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
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
	s := md5.Sum([]byte(user.Password + user.Name))
	return md5prefix + hex.EncodeToString(s[:])
}

// PrettyDiff shows the diff between 2 objects in an easy to understand format. It is mainly used for debugging output.
func PrettyDiff(a, b interface{}) (result string) {
	diff := pretty.Diff(a, b)
	return strings.Join(diff, "\n")
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
