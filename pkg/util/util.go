package util

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/motomux/pretty"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/zalando-incubator/postgres-operator/pkg/spec"
)

const (
	MD5Prefix = "md5"
)

var passwordChars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func init() {
	rand.Seed(int64(time.Now().Unix()))
}

func RandomPassword(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = passwordChars[rand.Intn(len(passwordChars))]
	}

	return string(b)
}

func NameFromMeta(meta v1.ObjectMeta) spec.NamespacedName {
	return spec.NamespacedName{
		Namespace: meta.Namespace,
		Name:      meta.Name,
	}
}

func PGUserPassword(user spec.PgUser) string {
	if (len(user.Password) == md5.Size && user.Password[:3] == MD5Prefix) || user.Password == "" {
		// Avoid processing already encrypted or empty passwords
		return user.Password
	}
	s := md5.Sum([]byte(user.Password + user.Name))
	return MD5Prefix + hex.EncodeToString(s[:])
}

func Pretty(x interface{}) (f fmt.Formatter) {
	return pretty.Formatter(x)
}

func PrettyDiff(a, b interface{}) (result string) {
	diff := pretty.Diff(a, b)
	return strings.Join(diff, "\n")
}

func SubstractStringSlices(a []string, b []string) (result []string, equal bool) {
	// Find elements in a that are not in b and return them as a result slice
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
