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
	//v1 "github.com/VineethReddy02/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"strconv"
	"time"
	"github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	)

// listCmd represents kubectl pg list.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all the resources of kind postgresql",
	Long:  `Lists all the info specific to postgresql objects.`,
	Run: func(cmd *cobra.Command, args []string) {
		allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
		namespace,_ := cmd.Flags().GetString("namespace")
		if allNamespaces {
			list(allNamespaces,"")
		} else {
			list(allNamespaces,namespace)
		}

	},
	Example: "kubectl pg list\nkubectl pg list -A",
}

// list command to list postgres.
func list(allNamespaces bool, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	var listPostgres *v1.PostgresqlList
	listPostgres, err = postgresConfig.Postgresqls(namespace).List(metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}
	if len(listPostgres.Items) == 0 {
		if namespace != "" {
			fmt.Printf("No Postgresql clusters found in namespace: %v\n", namespace)
		} else {
			fmt.Println("No Postgresql clusters found in all namespaces")
		}
		return
	}
	if allNamespaces {
		listAll(listPostgres)
	} else {
		listWithNamespace(listPostgres)
	}
}


func listAll(listPostgres *v1.PostgresqlList) {
	template := "%-32s%-16s%-12s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME", "NAMESPACE")
	for _, pgObjs := range listPostgres.Items {
		fmt.Printf(template, pgObjs.Name, pgObjs.Status.PostgresClusterStatus, strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PgVersion, time.Since(pgObjs.CreationTimestamp.Time).Truncate(6000000000), pgObjs.Spec.Size, pgObjs.Namespace)
	}
}

func listWithNamespace(listPostgres *v1.PostgresqlList) {
	template := "%-32s%-16s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME")
	for _, pgObjs := range listPostgres.Items {
		fmt.Printf(template, pgObjs.Name, pgObjs.Status.PostgresClusterStatus, strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PgVersion, time.Since(pgObjs.CreationTimestamp.Time).Truncate(6000000000), pgObjs.Spec.Size)
	}
}


func init() {
	listCmd.Flags().BoolP("all-namespaces", "A", false, "list pg resources across all namespaces.")
	listCmd.Flags().StringP("namespace","n",getCurrentNamespace(),"provide the namespace")
	rootCmd.AddCommand(listCmd)
}
