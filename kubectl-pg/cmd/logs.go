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
	"context"
	"io"
	"log"
	"os"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// logsCmd represents the logs command
var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "This will fetch the logs of the specified postgres cluster & postgres operator",
	Long:  `Fetches the logs of the postgres cluster (i.e master( with -m flag) & replica with (-r 1 pod number) and without -m or -r connects to random replica`,
	Run: func(cmd *cobra.Command, args []string) {
		opLogs, _ := cmd.Flags().GetBool("operator")
		clusterName, _ := cmd.Flags().GetString("cluster")
		master, _ := cmd.Flags().GetBool("master")
		replica, _ := cmd.Flags().GetString("replica")

		if opLogs {
			operatorLogs()
		} else {
			clusterLogs(clusterName, master, replica)
		}
	},
	Example: `
#Fetch the logs of the postgres operator
kubectl pg logs -o

#Fetch the logs of the master for provided cluster
kubectl pg logs -c cluster01 -m

#Fetch the logs of the random replica for provided cluster
kubectl pg logs -c cluster01

#Fetch the logs of the provided replica number of the cluster
kubectl pg logs -c cluster01 -r 3
`,
}

func operatorLogs() {
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	operator := getPostgresOperator(client)
	allPods, err := client.CoreV1().Pods(operator.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	var operatorPodName string
	for _, pod := range allPods.Items {
		for key, value := range pod.Labels {
			if (key == "name" && value == OperatorName) || (key == "app.kubernetes.io/name" && value == OperatorName) {
				operatorPodName = pod.Name
				break
			}
		}
	}

	execRequest := client.CoreV1().RESTClient().Get().Namespace(operator.Namespace).
		Name(operatorPodName).
		Resource("pods").
		SubResource("log").
		Param("follow", "--follow").
		Param("container", OperatorName)

	readCloser, err := execRequest.Stream(context.TODO())
	if err != nil {
		log.Fatal(err)
	}

	defer readCloser.Close()
	_, err = io.Copy(os.Stdout, readCloser)
	if err != nil {
		log.Fatal(err)
	}
}

func clusterLogs(clusterName string, master bool, replica string) {
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	podName := getPodName(clusterName, master, replica)
	execRequest := client.CoreV1().RESTClient().Get().Namespace(getCurrentNamespace()).
		Name(podName).
		Resource("pods").
		SubResource("log").
		Param("follow", "--follow").
		Param("container", "postgres")

	readCloser, err := execRequest.Stream(context.TODO())
	if err != nil {
		log.Fatal(err)
	}

	defer readCloser.Close()
	_, err = io.Copy(os.Stdout, readCloser)
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().BoolP("operator", "o", false, "logs of operator")
	logsCmd.Flags().StringP("cluster", "c", "", "logs for the provided cluster")
	logsCmd.Flags().BoolP("master", "m", false, "Patroni logs of master")
	logsCmd.Flags().StringP("replica", "r", "", "Patroni logs of replica. Specify replica number.")
}
