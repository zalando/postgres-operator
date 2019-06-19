package cmd

import (
	"flag"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os/exec"
	"path/filepath"
)

func getConfig() *restclient.Config {
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

func getCurrentNamespace() string {
	namespace, _ := exec.Command("kubectl", "config", "view", "--minify", "--output", "jsonpath={..namespace}").CombinedOutput()
	currentNamespace := string(namespace)
	if currentNamespace == "" {
		currentNamespace = "default"
	}
	return currentNamespace
}
