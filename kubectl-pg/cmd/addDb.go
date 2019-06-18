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
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// addDbCmd represents the addDb command
var addDbCmd = &cobra.Command{
	Use:   "add-db",
	Short: "Adds a db to the postgres cluster and it's owner",
	Long:  `Adds a db and it's owner to the cluster owner needs to be added with -o flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			dbName := args[0]
			dbOwner,_ := cmd.Flags().GetString("owner")
			clusterName,_ := cmd.Flags().GetString("clusterName")
			addDb(dbName,dbOwner, clusterName)
		} else {
			fmt.Println("database name can't be empty.")
		}

	},
}


// add db and it's owner to the cluster
func addDb(dbName string, dbOwner string, clusterName string) {
	config := getConfig()
	postgresConfig,err:=PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	namespace := getCurrentNamespace()
	postgresql,err := postgresConfig.Postgresqls(namespace).Get(clusterName,metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	postgresql.Spec.Databases[dbName] = dbOwner
	updatedPostgresql,err := postgresConfig.Postgresqls(namespace).Update(postgresql)
	if err != nil {
		panic(err)
	}
	if updatedPostgresql.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("postgresql %s is updated with new database: %s and as owner: %s.\n", updatedPostgresql.Name,dbName,dbOwner)
	} else {
		fmt.Printf("postgresql %s is unchanged.\n", updatedPostgresql.Name)
	}
}

func init() {
	addDbCmd.Flags().StringP("owner", "o", "", "provide owner of the database.")
	addDbCmd.Flags().StringP("clusterName", "c", "", "provide the cluster name.")
	rootCmd.AddCommand(addDbCmd)
}
