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
	"log"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	v1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TrimCreateTimestamp = 6000000000
)

// listCmd represents kubectl pg list.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all the resources of kind postgresql",
	Long:  `Lists all the info specific to postgresql objects.`,
	Run: func(cmd *cobra.Command, args []string) {
		allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
		namespace, _ := cmd.Flags().GetString("namespace")
		if allNamespaces {
			list(allNamespaces, "")
		} else {
			list(allNamespaces, namespace)
		}

	},
	Example: `
#Lists postgres cluster in current namespace
kubectl pg list

#Lists postgres clusters in all namespaces
kubectl pg list -A
`,
}

// list command to list postgres.
func list(allNamespaces bool, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	listPostgres, err := postgresConfig.Postgresqls(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	if len(listPostgres.Items) == 0 {
		if namespace != "" {
			fmt.Printf("No Postgresql clusters found in namespace: %v\n", namespace)
		} else {
			fmt.Println("No Postgresql clusters found in all namespaces")
		}
		return
	}

	if allNamespaces {
		listAll(listPostgres)
	} else {
		listWithNamespace(listPostgres)
	}
}

func listAll(listPostgres *v1.PostgresqlList) {
	template := "%-32s%-16s%-12s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME", "NAMESPACE")
	for _, pgObjs := range listPostgres.Items {
		fmt.Printf(template, pgObjs.Name,
			pgObjs.Status.PostgresClusterStatus,
			strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PostgresqlParam.PgVersion,
			time.Since(pgObjs.CreationTimestamp.Time).Truncate(TrimCreateTimestamp),
			pgObjs.Spec.Size, pgObjs.Namespace)
	}
}

func listWithNamespace(listPostgres *v1.PostgresqlList) {
	template := "%-32s%-16s%-12s%-12s%-12s%-12s\n"
	fmt.Printf(template, "NAME", "STATUS", "INSTANCES", "VERSION", "AGE", "VOLUME")
	for _, pgObjs := range listPostgres.Items {
		fmt.Printf(template, pgObjs.Name,
			pgObjs.Status.PostgresClusterStatus,
			strconv.Itoa(int(pgObjs.Spec.NumberOfInstances)),
			pgObjs.Spec.PostgresqlParam.PgVersion,
			time.Since(pgObjs.CreationTimestamp.Time).Truncate(TrimCreateTimestamp),
			pgObjs.Spec.Size)
	}
}

func init() {
	listCmd.Flags().BoolP("all-namespaces", "A", false, "list pg resources across all namespaces.")
	listCmd.Flags().StringP("namespace", "n", getCurrentNamespace(), "provide the namespace")
	rootCmd.AddCommand(listCmd)
}
