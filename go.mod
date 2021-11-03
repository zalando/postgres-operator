module github.com/zalando/postgres-operator

go 1.16

require (
	github.com/aws/aws-sdk-go v1.41.16
	github.com/golang/mock v1.6.0
	github.com/lib/pq v1.10.3
	github.com/motomux/pretty v0.0.0-20161209205251-b2aad2c9a95d
	github.com/r3labs/diff v1.1.0
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/crypto v0.0.0-20210921155107-089bfa567519
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.3
	k8s.io/apiextensions-apiserver v0.22.3
	k8s.io/apimachinery v0.22.3
	k8s.io/client-go v0.22.3
	k8s.io/code-generator v0.22.3
)
