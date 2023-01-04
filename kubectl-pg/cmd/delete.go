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
	"fmt"
	"io/ioutil"
	"log"

	"github.com/spf13/cobra"
	v1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Example: `
#Deleting the postgres cluster using manifest file
kubectl pg delete -f cluster-manifest.yaml

#Deleting the postgres cluster using cluster name in current namespace.
kubectl pg delete cluster01

#Deleting the postgres cluster using cluster name in provided namespace
kubectl pg delete cluster01 -n namespace01
`,
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
	_, err = postgresConfig.Postgresqls(postgresSql.Namespace).Get(context.TODO(), postgresSql.Name, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Postgresql %s not found with the provided namespace %s : %s \n", postgresSql.Name, postgresSql.Namespace, err)
		return
	}
	fmt.Printf("Are you sure you want to remove this PostgreSQL cluster? If so, please type (%s/%s) and hit Enter\n", postgresSql.Namespace, postgresSql.Name)

	confirmAction(postgresSql.Name, postgresSql.Namespace)
	err = postgresConfig.Postgresqls(postgresSql.Namespace).Delete(context.TODO(), postgresSql.Name, metav1.DeleteOptions{})
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

	_, err = postgresConfig.Postgresqls(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		fmt.Printf("Postgresql %s not found with the provided namespace %s : %s \n", clusterName, namespace, err)
		return
	}
	fmt.Printf("Are you sure you want to remove this PostgreSQL cluster? If so, please type (%s/%s) and hit Enter\n", namespace, clusterName)

	confirmAction(clusterName, namespace)
	err = postgresConfig.Postgresqls(namespace).Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
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
