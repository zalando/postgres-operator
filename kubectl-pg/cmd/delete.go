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
	"github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	"io/ioutil"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

// deleteCmd represents kubectl pg delete.
var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes postgresql object by object-name/manifest file",
	Long: `Deletes the postgres objects specific to a manifest file or object-name.
Deleting the manifest is sufficient to delete the cluster.`,
	Run: func(cmd *cobra.Command, args []string) {
		namespace, _ := cmd.Flags().GetString("namespace")
		file, _ := cmd.Flags().GetString("file")
		if file != "" {
			deleteByFile(file)
		} else if namespace != "" {
			if len(args) != 0 {
				clusterName := args[0]
				deleteByName(clusterName, namespace)
			} else {
				fmt.Println("cluster name can't be empty")
			}
		} else {
			fmt.Println("use the flag either -n or -f to delete a resource.")
		}
	},
	Example: "kubectl pg delete -f [FILE-NAME]\nkubectl pg delete [CLUSTER-NAME] -n [NAMESPACE]",
}

func init() {
	namespace := getCurrentNamespace()
	deleteCmd.Flags().StringP("namespace", "n", namespace, "Delete postgresql resource by it's name.")
	deleteCmd.Flags().StringP("file", "f", "", "using file.")
	rootCmd.AddCommand(deleteCmd)
}

// Delete postgresql by manifest file.
func deleteByFile(file string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	ymlFile, err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(ymlFile), nil, &v1.Postgresql{})
	if err != nil {
		panic(err)
	}
	postgresSql := obj.(*v1.Postgresql)
	confirmDeletion(postgresConfig, postgresSql.Name, postgresSql.Namespace)
}

// Delete postgresql by name and namespace.
func deleteByName(clusterName string, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	confirmDeletion(postgresConfig, clusterName, namespace)
}

//Confirm delete & delete postgresql cluster.
func confirmDeletion(postgresConfig *PostgresqlLister.AcidV1Client, clusterName string, namespace string) {
	_, err := postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("-> postgresql %s not found with the provided namespace %s : %s \n", clusterName, namespace, err)
		return
	}
enterClusterName:
	fmt.Println("-> Are you sure you want to remove this PostgreSQL cluster? If so, please type the (NAMESPACE/CLUSTER-NAME) and hit Enter")
	confirmClusterName := ""
	_, _ = fmt.Scan(&confirmClusterName)
	clusterDetails := strings.Split(confirmClusterName, "/")
	confirmedNamespace := clusterDetails[0]
	confirmedClusterName := clusterDetails[1]
	if clusterName == confirmedClusterName {
		err = postgresConfig.Postgresqls(confirmedNamespace).Delete(confirmedClusterName, &metav1.DeleteOptions{})
		if err == nil {
			fmt.Printf("-> postgresql %s deleted from %s.\n", confirmedClusterName, confirmedNamespace)
		} else {
			fmt.Println(err)
			fmt.Println("-> cluster name or namespace doesn't match. Please re-enter the NAMESPACE/CLUSTER-NAME.")
			goto enterClusterName
		}
	} else {
		fmt.Println("-> cluster name or namespace doesn't match. Please re-enter the NAMESPACE/CLUSTER-NAME.")
		goto enterClusterName
	}
}
