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
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"log"
)

// addDbCmd represents the addDb command
var addDbCmd = &cobra.Command{
	Use:   "add-db",
	Short: "Adds a DB and its owner to a Postgres cluster. The owner role is created if it does not exist",
	Long:  `Adds a DB and its owner to the cluster owner needs to be added with -o flag, cluster with -c flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			dbName := args[0]
			dbOwner, _ := cmd.Flags().GetString("owner")
			clusterName, _ := cmd.Flags().GetString("cluster")
			addDb(dbName, dbOwner, clusterName)
		} else {
			fmt.Println("database name can't be empty. Use kubectl pg add-db [-h | --help] for more info")
		}

	},
	Example: "kubectl pg add-db [DB-NAME] -o [OWNER-NAME] -c [CLUSTER-NAME]",
}

// add db and it's owner to the cluster
func addDb(dbName string, dbOwner string, clusterName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	namespace := getCurrentNamespace()
	postgresql, err := postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	var patch []byte
	// validating reserved DB names
	if dbName != "postgres" && dbName != "template0" && dbName != "template1" {
		patch = dbPatch(dbName,dbOwner)
	} else {
		log.Fatal("The provided db-name is reserved by postgres")
	}

	updatedPostgres, err := postgresConfig.Postgresqls(namespace).Patch(postgresql.Name,types.MergePatchType, patch,"")
	if err != nil {
		log.Fatal(err)
	}

	if updatedPostgres.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("Created new database %s with owner %s in PostgreSQL cluster %s.\n", dbName, dbOwner, updatedPostgres.Name)
	} else {
		fmt.Printf("postgresql %s is unchanged.\n", updatedPostgres.Name)
	}
}

func dbPatch(dbname string, owner string) []byte {
	ins := map[string]map[string]map[string]string{"spec": {"databases": {dbname: owner}}}
	patchInstances, err := json.Marshal(ins)
	if err != nil {
		log.Fatal(err, "unable to parse patch for add-db")
	}
	return patchInstances
}

func init() {
	addDbCmd.Flags().StringP("owner", "o", "", "provide owner of the database.")
	addDbCmd.Flags().StringP("cluster", "c", "", "provide a postgres cluster name.")
	rootCmd.AddCommand(addDbCmd)
}
