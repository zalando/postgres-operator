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
	"strings"
)

var allowedPrivileges  = []string{"SUPERUSER", "REPLICATION", "INHERIT", "LOGIN", "NOLOGIN", "CREATEROLE", "CREATEDB", "BYPASSURL"}
// addUserCmd represents the addUser command
var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Adds a user to the postgres cluster with given privileges",
	Long:  `Adds a user to the postgres cluster you can add privileges as well with -p flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		privileges, _ := cmd.Flags().GetString("privileges")

		if len(args) > 0 {
			user := args[0]
			var permissions []string
			var perms []string

			if privileges != "" {
				parsedRoles := strings.Replace(privileges, ",", " ", -1)
				parsedRoles  = strings.ToUpper(parsedRoles)
				fmt.Println(parsedRoles)
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
	Example: "kubectl pg add-user [USER] -p [PRIVILEGES] -c [CLUSTER-NAME]",
}

// add user to the cluster with provided permissions
func addUser(user string, clusterName string, permissions []string) {
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

	setUsers := make(map[string]bool)
	for _, k := range permissions {
		setUsers[k] = true
	}

	if existingRoles, key := postgresql.Spec.Users[user]; key {
		for _, k := range existingRoles {
			setUsers[k] = true
		}
	}

	var Privileges []string
	for keys, values := range setUsers {
		if values {
			Privileges = append(Privileges, keys)
		}
	}

	patch := applyUserPatch(user, Privileges)
	updatedPostgresql, err := postgresConfig.Postgresqls(namespace).Patch(postgresql.Name,types.MergePatchType, patch,"")
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
