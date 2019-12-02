module github.com/zalando/postgres-operator

go 1.12

require (
	github.com/aws/aws-sdk-go v1.25.44
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/lib/pq v1.2.0
	github.com/motomux/pretty v0.0.0-20161209205251-b2aad2c9a95d
	github.com/sirupsen/logrus v1.4.2
	gopkg.in/yaml.v2 v2.2.7
	k8s.io/api v0.0.0-20191121015604-11707872ac1c
	k8s.io/apiextensions-apiserver v0.0.0-20191121021419-88daf26ec3b8
	k8s.io/apimachinery v0.0.0-20191123233150-4c4803ed55e3
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/code-generator v0.0.0-20191121015212-c4c8f8345c7e
)
