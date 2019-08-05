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
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"log"
	v1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
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
	Example: "kubectl pg create -f [FILE-NAME]",
}

// Create postgresql resources.
func create(fileName string) {
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
	postgresSql := obj.(*v1.Postgresql)
	_, err = postgresConfig.Postgresqls(postgresSql.Namespace).Create(postgresSql)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("postgresql %s created.\n", postgresSql.Name)
}

func init() {
	createCmd.Flags().StringP("file", "f", "", "manifest file with the cluster definition.")
	rootCmd.AddCommand(createCmd)
}
