// Copyright © 2019 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"strconv"
)

// scaleCmd represents the scale command
var scaleCmd = &cobra.Command{
	Use:   "scale",
	Short: "Add/remove pods to a Postgres cluster",
	Long: `Scales the postgres objects using cluster-name.
Scaling to 0 leads to down time.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("clusterName")
		namespace, _ := cmd.Flags().GetString("namespace")
		if len(args) > 0 {
			numberofInstances, _ := strconv.Atoi(args[0])
			scale(int32(numberofInstances), clusterName, namespace)
		} else {
			fmt.Println("Please enter number of instances to scale.")
		}

	},
	Example: "kubectl pg scale [NUMBER-OF-INSTANCES] -c [CLUSTER-NAME] -n [NAMESPACE]",
}

func scale(numberOfInstances int32, clusterName string, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	postgresql, err := postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if numberOfInstances == 0 {
		fmt.Printf("Scaling to zero leads to down time. please type %s/%s and hit Enter\n", namespace, clusterName)
		confirmAction(clusterName, namespace)
	}
	postgresql.Spec.NumberOfInstances = numberOfInstances
	UpdatedPostgres, err := postgresConfig.Postgresqls(namespace).Update(postgresql)
	if err != nil {
		log.Fatal(err)
	}
	if UpdatedPostgres.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("scaled postgresql %s/%s to %d instances\n", UpdatedPostgres.Namespace, UpdatedPostgres.Name, UpdatedPostgres.Spec.NumberOfInstances)
		return
	}
	fmt.Printf("postgresql %s is unchanged.\n", postgresql.Name)
}

func init() {
	namespace := getCurrentNamespace()
	scaleCmd.Flags().StringP("namespace", "n", namespace, "provide the namespace.")
	scaleCmd.Flags().StringP("clusterName", "c", "", "provide the cluster name.")
	rootCmd.AddCommand(scaleCmd)
}
