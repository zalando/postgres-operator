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
	"strings"
)

// addUserCmd represents the addUser command
var addUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Add user cmd is to add user to the postgres cluster and it's roles",
	Long:  `Add user cmd is to add user to the postgres cluster you can add privileges as well with -f flag.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("clusterName")
		privileges, _ := cmd.Flags().GetString("privileges")
		if len(args) > 0 {
			user := args[0]
			permissions := []string{}
			if privileges != "" {
				parsedRoles := strings.Replace(privileges, ",", " ", -1)
				permissions = strings.Fields(parsedRoles)
			}
			addUser(user,clusterName, permissions)
		}
	},
}

// add user to the cluster with provided permissions
func addUser(user string, clusterName string, permissions []string) {
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
	if existingRoles ,key := postgresql.Spec.Users[user]; key{
		permissions = append(permissions,existingRoles...)
	}
	postgresql.Spec.Users[user] = permissions
	updatedPostgresql,err := postgresConfig.Postgresqls(namespace).Update(postgresql)
	if err != nil {
		panic(err)
	}
	if updatedPostgresql.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("postgresql %s is updated with new user %s and with privileges %s.\n", updatedPostgresql.Name,user,permissions)
	} else {
		fmt.Printf("postgresql %s is unchanged.\n", updatedPostgresql.Name)
	}
}

func init() {
	addUserCmd.Flags().StringP("clusterName", "c", "", "add user to the provided cluster.")
	addUserCmd.Flags().StringP("privileges", "p", "", "add privileges to the provided cluster.")
	rootCmd.AddCommand(addUserCmd)
}
