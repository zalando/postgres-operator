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
	"encoding/json"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// addDbCmd represents the addDb command
var addDbCmd = &cobra.Command{
	Use:   "add-db",
	Short: "Adds a DB and its owner to a Postgres cluster. The owner role is created if it does not exist",
	Long:  `Adds a new DB to the Postgres cluster. Owner needs to be specified by the -o flag, cluster with -c flag.`,
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
	Example: `
kubectl pg add-db db01 -o owner01 -c cluster01
`,
}

// add db and it's owner to the cluster
func addDb(dbName string, dbOwner string, clusterName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	namespace := getCurrentNamespace()
	postgresql, err := postgresConfig.Postgresqls(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	var dbOwnerExists bool
	dbUsers := postgresql.Spec.Users
	for key := range dbUsers {
		if key == dbOwner {
			dbOwnerExists = true
		}
	}
	var patch []byte
	// validating reserved DB names
	if dbOwnerExists && dbName != "postgres" && dbName != "template0" && dbName != "template1" {
		patch = dbPatch(dbName, dbOwner)
	} else if !dbOwnerExists {
		log.Fatal("The provided db-owner doesn't exist")
	} else {
		log.Fatal("The provided db-name is reserved by postgres")
	}

	updatedPostgres, err := postgresConfig.Postgresqls(namespace).Patch(context.TODO(), postgresql.Name, types.MergePatchType, patch, metav1.PatchOptions{})
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
