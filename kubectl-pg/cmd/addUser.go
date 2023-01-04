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
	"strings"

	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var allowedPrivileges = []string{"SUPERUSER", "REPLICATION", "INHERIT", "LOGIN", "NOLOGIN", "CREATEROLE", "CREATEDB", "BYPASSURL"}

// addUserCmd represents the addUser command
var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Adds a user to the postgres cluster with given privileges",
	Long:  `Adds a user to the postgres cluster. You can add privileges as well with -p flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		privileges, _ := cmd.Flags().GetString("privileges")

		if len(args) > 0 {
			user := args[0]
			var permissions []string
			var perms []string

			if privileges != "" {
				parsedRoles := strings.Replace(privileges, ",", " ", -1)
				parsedRoles = strings.ToUpper(parsedRoles)
				permissions = strings.Fields(parsedRoles)
				var invalidPerms []string

				for _, userPrivilege := range permissions {
					validPerm := false
					for _, privilege := range allowedPrivileges {
						if privilege == userPrivilege {
							perms = append(perms, userPrivilege)
							validPerm = true
						}
					}
					if !validPerm {
						invalidPerms = append(invalidPerms, userPrivilege)
					}
				}

				if len(invalidPerms) > 0 {
					fmt.Printf("Invalid privileges %s\n", invalidPerms)
					return
				}
			}
			addUser(user, clusterName, perms)
		}
	},
	Example: `
kubectl pg add-user user01 -p login,createdb -c cluster01
`,
}

// add user to the cluster with provided permissions
func addUser(user string, clusterName string, permissions []string) {
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

	setUsers := make(map[string]bool)
	for _, k := range permissions {
		setUsers[k] = true
	}

	if existingRoles, key := postgresql.Spec.Users[user]; key {
		for _, k := range existingRoles {
			setUsers[k] = true
		}
	}

	Privileges := []string{}
	for keys, values := range setUsers {
		if values {
			Privileges = append(Privileges, keys)
		}
	}

	patch := applyUserPatch(user, Privileges)
	updatedPostgresql, err := postgresConfig.Postgresqls(namespace).Patch(context.TODO(), postgresql.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		log.Fatal(err)
	}

	if updatedPostgresql.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("postgresql %s is updated with new user %s and with privileges %s.\n", updatedPostgresql.Name, user, permissions)
	} else {
		fmt.Printf("postgresql %s is unchanged.\n", updatedPostgresql.Name)
	}
}

func applyUserPatch(user string, value []string) []byte {
	ins := map[string]map[string]map[string][]string{"spec": {"users": {user: value}}}
	patchInstances, err := json.Marshal(ins)
	if err != nil {
		log.Fatal(err, "unable to parse number of instances json")
	}
	return patchInstances
}

func init() {
	addUserCmd.Flags().StringP("cluster", "c", "", "add user to the provided cluster.")
	addUserCmd.Flags().StringP("privileges", "p", "", "add privileges separated by commas without spaces")
	rootCmd.AddCommand(addUserCmd)
}
