package util

import (
	"fmt"
	"math/rand"
	"time"

	"k8s.io/client-go/pkg/api/v1"
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

func FullObjectNameFromMeta(meta v1.ObjectMeta) string {
	return FullObjectName(meta.Namespace, meta.Name)
}

//TODO: Remove in favour of FullObjectNameFromMeta
func FullObjectName(ns, name string) string {
	if ns == "" {
		ns = "default"
	}

	return fmt.Sprintf("%s / %s", ns, name)
}
