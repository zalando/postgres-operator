module github.com/zalando/postgres-operator

go 1.14

require (
	github.com/aws/aws-sdk-go v1.32.2
	github.com/lib/pq v1.7.0
	github.com/motomux/pretty v0.0.0-20161209205251-b2aad2c9a95d
	github.com/r3labs/diff v1.1.0
	github.com/sirupsen/logrus v1.6.0
	github.com/stretchr/testify v1.5.1
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/tools v0.0.0-20200729041821-df70183b1872 // indirect
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/api v0.18.6
	k8s.io/apiextensions-apiserver v0.18.6
	k8s.io/apimachinery v0.18.6
	k8s.io/client-go v0.18.6
	k8s.io/code-generator v0.18.6
)
