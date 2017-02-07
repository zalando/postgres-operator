package util

import (
	"math/rand"
	"time"
)

var passwordChars = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$^&*=")

func init() {
	rand.Seed(int64(time.Now().Unix()))
}

func RandomPasswordBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = passwordChars[rand.Intn(len(passwordChars))]
	}

	return b
}
