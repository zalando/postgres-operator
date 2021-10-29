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

// createCmd kubectl pg create.
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Creates postgres object using manifest file",
	Long:  `Creates postgres custom resource objects from a manifest file.`,
	Run: func(cmd *cobra.Command, args []string) {
		fileName, _ := cmd.Flags().GetString("file")
		create(fileName)
	},
	Example: `
kubectl pg create -f cluster-manifest.yaml
`,
}

// Create postgresql resources.
func create(fileName string) {
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

	postgresSql := obj.(*v1.Postgresql)
	_, err = postgresConfig.Postgresqls(postgresSql.Namespace).Create(context.TODO(), postgresSql, metav1.CreateOptions{})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("postgresql %s created.\n", postgresSql.Name)
}

func init() {
	createCmd.Flags().StringP("file", "f", "", "manifest file with the cluster definition.")
	rootCmd.AddCommand(createCmd)
}
