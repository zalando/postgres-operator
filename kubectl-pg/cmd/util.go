package cmd

import (
	"flag"
	"fmt"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getConfig() *restclient.Config {
	var kubeconfig *string
	var config *restclient.Config
	envKube := os.Getenv("KUBECONFIG")
	if  envKube != ""{
		kubeconfig = &envKube
	} else {
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		} else {
			kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
		}
	}
		flag.Parse()
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			log.Fatal(err)
		}
		return config
}

func getCurrentNamespace() string {
	namespace, err := exec.Command("kubectl", "config", "view", "--minify", "--output", "jsonpath={..namespace}").CombinedOutput()
	if err != nil {
		log.Fatal(err)
	}
	currentNamespace := string(namespace)
	if currentNamespace == "" {
		currentNamespace = "default"
	}
	return currentNamespace
}

func confirmAction(clusterName string, namespace string) {
	for {
		confirmClusterDetails := ""
		_, err := fmt.Scan(&confirmClusterDetails)
		if err != nil {
			log.Fatalf("couldn't get confirmation from the user %v",err)
		}
		clusterDetails := strings.Split(confirmClusterDetails, "/")
		if clusterDetails[0] != namespace || clusterDetails[1] != clusterName {
			fmt.Printf("cluster name or namespace doesn't match. Please re-enter %s/%s\nHint: Press (ctrl+c) to exit\n", namespace, clusterName)
		} else {
			return
		}
	}
}

func getPodName(clusterName string, master bool, replica string) string {
	config := getConfig()
	client,er := kubernetes.NewForConfig(config)
	if er != nil {
		log.Fatal(er)
	}
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	postgresCluster, err := postgresConfig.Postgresqls(getCurrentNamespace()).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	numOfInstances := postgresCluster.Spec.NumberOfInstances
	var podName string
	var podRole string
	replica = clusterName+"-"+replica
	for ins:=0;ins < int(numOfInstances);ins++ {
		pod,err := client.CoreV1().Pods(getCurrentNamespace()).Get(clusterName+"-"+strconv.Itoa(ins),metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}
		podRole = pod.Labels["spilo-role"]
		if podRole == "master" && master {
			podName = pod.Name
			fmt.Printf("connected to %s with name %s\n",podRole, podName)
			break
		} else if podRole == "replica" &&  !master  && (pod.Name == replica || replica == "") {
			podName = pod.Name
			fmt.Printf("connected to %s with pod name as %s\n",podRole, podName)
			break
		}
	}
	if podName == "" {
		log.Fatal("Provided replica doesn't exist")
	}
	return podName
}
