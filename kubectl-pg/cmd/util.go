package cmd

import (
	"flag"
	"k8s.io/client-go/tools/clientcmd"
	"path/filepath"
	"k8s.io/client-go/util/homedir"
	restclient "k8s.io/client-go/rest"
)

func getConfig()(*restclient.Config){
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}
	return config
}
