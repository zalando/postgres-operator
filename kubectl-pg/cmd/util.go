/*
Copyright © 2019 Vineeth Pothulapati <vineethpothulapati@outlook.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package cmd

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	OperatorName     = "postgres-operator"
	DefaultNamespace = "default"
)

func getConfig() *restclient.Config {
	var kubeconfig *string
	var config *restclient.Config
	envKube := os.Getenv("KUBECONFIG")
	if envKube != "" {
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
		currentNamespace = DefaultNamespace
	}
	return currentNamespace
}

func confirmAction(clusterName string, namespace string) {
	for {
		confirmClusterDetails := ""
		_, err := fmt.Scan(&confirmClusterDetails)
		if err != nil {
			log.Fatalf("couldn't get confirmation from the user %v", err)
		}
		clusterDetails := strings.Split(confirmClusterDetails, "/")
		if clusterDetails[0] != namespace || clusterDetails[1] != clusterName {
			fmt.Printf("cluster name or namespace does not match. Please re-enter %s/%s\nHint: Press (ctrl+c) to exit\n", namespace, clusterName)
		} else {
			return
		}
	}
}

func getPodName(clusterName string, master bool, replicaNumber string) string {
	config := getConfig()
	client, er := kubernetes.NewForConfig(config)
	if er != nil {
		log.Fatal(er)
	}

	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	postgresCluster, err := postgresConfig.Postgresqls(getCurrentNamespace()).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	numOfInstances := postgresCluster.Spec.NumberOfInstances
	var podName string
	var podRole string
	replica := clusterName + "-" + replicaNumber

	for ins := 0; ins < int(numOfInstances); ins++ {
		pod, err := client.CoreV1().Pods(getCurrentNamespace()).Get(clusterName+"-"+strconv.Itoa(ins), metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}

		podRole = pod.Labels["spilo-role"]
		if podRole == "master" && master {
			podName = pod.Name
			fmt.Printf("connected to %s with pod name as %s\n", podRole, podName)
			break
		} else if podRole == "replica" && !master && (pod.Name == replica || replicaNumber == "") {
			podName = pod.Name
			fmt.Printf("connected to %s with pod name as %s\n", podRole, podName)
			break
		}
	}
	if podName == "" {
		log.Fatal("Provided replica doesn't exist")
	}
	return podName
}

func getPostgresOperator(k8sClient *kubernetes.Clientset) *v1.Deployment {
	var operator *v1.Deployment
	operator, err := k8sClient.AppsV1().Deployments(getCurrentNamespace()).Get(OperatorName, metav1.GetOptions{})
	if err == nil {
		return operator
	}

	allDeployments := k8sClient.AppsV1().Deployments("")
	listDeployments, err := allDeployments.List(metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for _, deployment := range listDeployments.Items {
		if deployment.Name == OperatorName {
			operator = deployment.DeepCopy()
			break
		} else {
			for key, value := range deployment.Labels {
				if key == "app.kubernetes.io/name" && value == OperatorName {
					operator = deployment.DeepCopy()
					break
				}
			}
		}
	}
	return operator
}
