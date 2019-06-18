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
	"strconv"
	"time"
)

// listCmd represents kubectl pg list.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all the resources of kind postgresql",
	Long:  `Lists all the info specific to postgresql objects.`,
	Run: func(cmd *cobra.Command, args []string) {
		allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
		if allNamespaces {
			listAll()
			return
		} else if len(args) == 0 {
			list()
			return
		}
		fmt.Println("Invalid argument with list command.")
	},
	Example: "kubectl pg list\nkubectl pg list -A",
}

// list command to list postgresql objects.
func list() {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	namespace := getCurrentNamespace()
	listPostgresql, err := postgresConfig.Postgresqls(namespace).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	if len(listPostgresql.Items) == 0 {
		fmt.Println("No Resources found.")
		return
	}
	template := "%-32s%-16s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME")
	for _, pgObjs := range listPostgresql.Items {
		fmt.Printf(template, pgObjs.Name, pgObjs.Status.PostgresClusterStatus, strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PgVersion, time.Since(pgObjs.CreationTimestamp.Time).Truncate(6000000000), pgObjs.Spec.Size)
	}

}

// list command to list postgresql objects across namespaces.
func listAll() {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	listPostgresql, err := postgresConfig.Postgresqls("").List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	if len(listPostgresql.Items) == 0 {
		fmt.Println("No Resources found.")
		return
	}
	template := "%-32s%-16s%-12s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME", "NAMESPACE")
	for _, pgObjs := range listPostgresql.Items {
		fmt.Printf(template, pgObjs.Name, pgObjs.Status.PostgresClusterStatus, strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PgVersion, time.Since(pgObjs.CreationTimestamp.Time).Truncate(6000000000), pgObjs.Spec.Size, pgObjs.Namespace)
	}
}

func init() {
	listCmd.Flags().BoolP("all-namespaces", "A", false, "list pg resources across all namespaces.")
	rootCmd.AddCommand(listCmd)
}
