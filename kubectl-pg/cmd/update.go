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

// updateCmd represents kubectl pg update
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Updates postgresql object using manifest file",
	Long:  `Updates the state of cluster using manifest file to reflect the changes on the cluster.`,
	Run: func(cmd *cobra.Command, args []string) {
		fileName, _ := cmd.Flags().GetString("file")
		updatePgResources(fileName)
	},
	Example: `
#usage
kubectl pg update -f [File-NAME]

#update the postgres cluster with updated manifest file
kubectl pg update -f cluster-manifest.yaml
`,
}

// Update postgresql resources.
func updatePgResources(fileName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	ymlFile, err := ioutil.ReadFile(fileName)
	if err != nil {
		log.Fatal(err)
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode([]byte(ymlFile), nil, &v1.Postgresql{})
	if err != nil {
		log.Fatal(err)
	}

	newPostgresObj := obj.(*v1.Postgresql)
	oldPostgresObj, err := postgresConfig.Postgresqls(newPostgresObj.Namespace).Get(context.TODO(), newPostgresObj.Name, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	newPostgresObj.ResourceVersion = oldPostgresObj.ResourceVersion
	response, err := postgresConfig.Postgresqls(newPostgresObj.Namespace).Update(context.TODO(), newPostgresObj, metav1.UpdateOptions{})
	if err != nil {
		log.Fatal(err)
	}

	if newPostgresObj.ResourceVersion != response.ResourceVersion {
		fmt.Printf("postgresql %s updated.\n", response.Name)
	} else {
		fmt.Printf("postgresql %s is unchanged.\n", response.Name)
	}
}

func init() {
	updateCmd.Flags().StringP("file", "f", "", "manifest file with the cluster definition.")
	rootCmd.AddCommand(updateCmd)
}
