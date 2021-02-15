module github.com/zalando/postgres-operator/kubectl-pg

go 1.15

require (
	github.com/spf13/cobra v1.1.3
	github.com/spf13/viper v1.7.1
	github.com/zalando/postgres-operator v1.6.0
	k8s.io/api v0.20.2
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/klog v1.0.0 // indirect
)
