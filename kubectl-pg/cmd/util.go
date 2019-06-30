package cmd

import (
	"flag"
	"fmt"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os/exec"
	"path/filepath"
	"strings"
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

func confirmAction(clusterName string, namespace string) {
	for {
		confirmClusterDetails := ""
		_, _ = fmt.Scan(&confirmClusterDetails)
		clusterDetails := strings.Split(confirmClusterDetails, "/")
		if clusterDetails[0] != namespace || clusterDetails[1] != clusterName {
			fmt.Printf("cluster name or namespace doesn't match. Please re-enter %s/%s\nHint: Press (ctrl+c) to exit\n", namespace,clusterName)
		} else {
			return
		}
	}
}
