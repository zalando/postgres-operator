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
	postgresConstants "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	apiextbeta1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)


// checkCmd represent kubectl pg check.
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Checks the postgresql CRD presence in k8s cluster",
	Long: `Checks for postgresql CRD across the namespaces in k8s cluster.`,
	Run: func(cmd *cobra.Command, args []string) {
		check()
	},
}

// check validates postgresql CRD registered or not.
func check() {
    config:=getConfig()
	apiExtClient, err := apiextbeta1.NewForConfig(config)
	if err!=nil {
		panic(err)
	}

	crdInfo,_:=apiExtClient.CustomResourceDefinitions().Get(postgresConstants.PostgresCRDResouceName,metav1.GetOptions{})
	if crdInfo.Name == postgresConstants.PostgresCRDResouceName {
		fmt.Printf("%s CRD registered.\n",crdInfo.Name)
	} else {
		fmt.Printf("%s CRD not registered.\n",postgresConstants.PostgresCRDResouceName)
	}
}


func init() {
	rootCmd.AddCommand(checkCmd)
}
