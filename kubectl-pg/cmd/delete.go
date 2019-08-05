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
	"log"
)

// deleteCmd represents kubectl pg delete.
var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Deletes postgresql object by cluster-name/manifest file",
	Long: `Deletes the postgres objects identified by a manifest file or cluster-name.
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

func deleteByFile(file string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	ymlFile, err := ioutil.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(ymlFile), nil, &v1.Postgresql{})
	if err != nil {
		log.Fatal(err)
	}
	postgresSql := obj.(*v1.Postgresql)
	_, err = postgresConfig.Postgresqls(postgresSql.Namespace).Get(postgresSql.Name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Postgresql %s not found with the provided namespace %s : %s \n", postgresSql.Name, postgresSql.Namespace, err)
		return
	}
	fmt.Printf("Are you sure you want to remove this PostgreSQL cluster? If so, please type (%s/%s) and hit Enter\n", postgresSql.Namespace, postgresSql.Name)
	confirmAction(postgresSql.Name, postgresSql.Namespace)
	err = postgresConfig.Postgresqls(postgresSql.Namespace).Delete(postgresSql.Name, &metav1.DeleteOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Postgresql %s deleted from %s.\n", postgresSql.Name, postgresSql.Namespace)
}

func deleteByName(clusterName string, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	_, err = postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Postgresql %s not found with the provided namespace %s : %s \n", clusterName, namespace, err)
		return
	}
	fmt.Printf("Are you sure you want to remove this PostgreSQL cluster? If so, please type (%s/%s) and hit Enter\n", namespace, clusterName)
	confirmAction(clusterName, namespace)
	err = postgresConfig.Postgresqls(namespace).Delete(clusterName, &metav1.DeleteOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Postgresql %s deleted from %s.\n", clusterName, namespace)
}

func init() {
	namespace := getCurrentNamespace()
	deleteCmd.Flags().StringP("namespace", "n", namespace, "namespace of the cluster to be deleted.")
	deleteCmd.Flags().StringP("file", "f", "", "manifest file with the cluster definition.")
	rootCmd.AddCommand(deleteCmd)
}