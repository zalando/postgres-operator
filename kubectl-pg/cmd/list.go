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
)

// listCmd represents kubectl pg list.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List command to list all the resources specific to an postgresql object",
	Long: `List all the info specific to postgresql objects.`,
	Run: func(cmd *cobra.Command, args []string) {
		 list()
	},
}

// list command to list postgresql objects.
func list(){
    config := getConfig()
	template := "%-32s%-8s%-8s%-8s\n"
	fmt.Printf(template,"NAME","READY","STATUS", "AGE")
	postgresConfig,err:=PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	listPostgresslq,_ := postgresConfig.Postgresqls("").List(metav1.ListOptions{})
	fmt.Println(listPostgresslq)
	for _,pgObjs := range listPostgresslq.Items {
		fmt.Printf(template,pgObjs.Name,pgObjs.Status, pgObjs.Namespace, pgObjs.CreationTimestamp)
	}

}
func init() {
	listCmd.Flags().StringP("HII","p","NO","SAY HII")
	rootCmd.AddCommand(listCmd)
}
