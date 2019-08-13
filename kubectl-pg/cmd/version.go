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
	"fmt"
	"k8s.io/client-go/kubernetes"
	"log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/spf13/cobra"
	"strings"
)

const KubectlPgVersion = "0.1-beta"


// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version of kubectl-pg & postgres-operator",
	Long: `version of kubectl-pg and current running postgres-operator`,
	Run: func(cmd *cobra.Command, args []string) {
		    namespace, err := cmd.Flags().GetString("namespace")
		    if err != nil {
		    	log.Fatal(err)
			}
				version(namespace)
	},
}

func version(namespace string) {
	fmt.Printf("kubectl-pg: %s\n",KubectlPgVersion)

	config := getConfig()
	client,err := kubernetes.NewForConfig(config)
	if err!= nil {
		log.Fatal(err)
	}

	res,err:=client.AppsV1().Deployments(namespace).Get(OperatorName,metav1.GetOptions{})
	if err != nil {
		log.Fatalf("couldn't find the postgres operator in namespace: %v",namespace)
	}

	operatorImage := res.Spec.Template.Spec.Containers[0].Image
	imageDetails := strings.Split(operatorImage, ":")
	imageSplit := len(imageDetails)
	imageVersion := imageDetails[imageSplit-1]
	fmt.Printf("Postgres-Operator: %s\n",imageVersion)
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().StringP("namespace", "n", DefaultNamespace, "provide the namespace.")
}
