/*
Copyright © 2019 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"github.com/spf13/cobra"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"log"
	"os"
)

// logsCmd represents the logs command
var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "This will fetch the logs of the specified postgres cluster & postgres operator",
	Long:  `Fetches the logs of the postgres cluster (i.e master( with -m flag) & replica with (-r 1 pod number) and without -m or -r connects to random replica`,
	Run: func(cmd *cobra.Command, args []string) {
		operatorLogs, _ := cmd.Flags().GetBool("operator")
		clusterName,_ := cmd.Flags().GetString("cluster")
		master,_:=cmd.Flags().GetBool("master")
		replica,_:= cmd.Flags().GetString("replica")

		if operatorLogs {
			logs(operatorLogs,clusterName,master,replica)
		} else {
			clusterLogs(clusterName,master,replica)
		}
	},
}

func logs(operatorLogs bool, clusterName string, master bool, replica string) {
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	allPods, err := client.CoreV1().Pods(getCurrentNamespace()).List(metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	operatorPodName := ""
	var lookFor string
	if operatorLogs {
		lookFor = "postgres-operator"
	}

	for _, pod := range allPods.Items {
		podName := pod.Labels
		if val, ok := podName["name"]; ok && val == lookFor {
			operatorPodName = pod.Name
			break
		}
	}

	execRequest := client.CoreV1().RESTClient().Get().Namespace("default").
		Name(operatorPodName).
		Resource("pods").
		SubResource("log").
		Param("follow", "--follow").
		Param("container", "postgres-operator")

	readCloser, err := execRequest.Stream()
	if err != nil {
		log.Fatal(err)
	}

	defer readCloser.Close()
	_, err = io.Copy(os.Stdout, readCloser)
	if err != nil {
		log.Fatal(err)
	}
}


func clusterLogs(clusterName string, master bool,replica string){
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	podName := getPodName(clusterName,master,replica)
	execRequest := client.CoreV1().RESTClient().Get().Namespace("default").
		Name(podName).
		Resource("pods").
		SubResource("log").
		Param("follow", "--follow").
		Param("container", "postgres")

	readCloser, err := execRequest.Stream()
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
	logsCmd.Flags().StringP("cluster","c","","logs for the provided cluster")
	logsCmd.Flags().BoolP("master","m",false,"specify -m for master")
	logsCmd.Flags().StringP("replica","r","","specify replica name")
}
