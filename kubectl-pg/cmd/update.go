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
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	"io/ioutil"
	v1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
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
	Example: "kubectl pg update -f [File-NAME]",
}


// Update postgresql resources.
func updatePgResources(fileName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
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
	oldPostgresObj, err := postgresConfig.Postgresqls(newPostgresObj.Namespace).Get(newPostgresObj.Name, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	newPostgresObj.ResourceVersion = oldPostgresObj.ResourceVersion
	response, err := postgresConfig.Postgresqls(newPostgresObj.Namespace).Update(newPostgresObj)
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