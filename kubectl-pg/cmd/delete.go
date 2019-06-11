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
	"io/ioutil"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
)

// deleteCmd represents kubectl pg delete.
var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete command to delete k8s postgresql objects by object-name/manifest file",
	Long: `Delete command deletes the postgres objects specific to a manifest file or an object provided object-name.`,
	Run: func(cmd *cobra.Command, args []string) {

		name,_ :=cmd.Flags().GetString("name")
		file,_ :=cmd.Flags().GetString("file")
		if name != "" {
			deleteByName(name)
		} else {
			deleteByFile(file)
		}
	},
}

func init() {
	deleteCmd.Flags().StringP("name","n","","Delete postgresql resource by it's name.")
	deleteCmd.Flags().StringP("file","f","","using file.")
	rootCmd.AddCommand(deleteCmd)
}

// Delete postgresql by manifest file.
func deleteByFile(file string) {
	config := getConfig()
	postgresConfig,err := PostgresqlLister.NewForConfig(config)
	ymlFile,err := ioutil.ReadFile(file)
	if err != nil {
		panic(err)
	}
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj,_,err := decode([]byte(ymlFile),nil, &v1.Postgresql{})
	if err!=nil {
		panic(err)
	}
	postgresSql := obj.(*v1.Postgresql)
	_ , err = postgresConfig.Postgresqls(postgresSql.Namespace).Get(postgresSql.Name, metav1.GetOptions{})
	if err!=nil {
		panic(err)
	}
	deleteStatus := postgresConfig.Postgresqls(postgresSql.Namespace).Delete(postgresSql.Name,&metav1.DeleteOptions{})
	if deleteStatus == nil {
		fmt.Printf("postgresql %s deleted.\n", postgresSql.Name)
	}
}

// Delete postgresql by name.
func deleteByName(name string) {
	config := getConfig()
	postgresConfig,err := PostgresqlLister.NewForConfig(config)
	if err!=nil{
		panic(err)
	}
	postgresSql,err:= postgresConfig.Postgresqls("").List(metav1.ListOptions{})
	if err!=nil {
		panic(err)
	}
	deleted:=false
	for _,pgObjs := range postgresSql.Items {
		if(name == pgObjs.Name) {
			postgresConfig.Postgresqls(pgObjs.Namespace).Delete(pgObjs.Name, &metav1.DeleteOptions{})
			deleted=true
			break
		}
	}
	if deleted {
		fmt.Printf("postgresql %s deleted.\n", name)
	} else {
		fmt.Printf("postgresql %s not found.\n", name)
	}
}