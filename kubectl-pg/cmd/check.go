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
	"flag"
	"fmt"
	"github.com/spf13/cobra"
	apiextbeta1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	postgresConstants "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
)


// checkCmd represent kubectl pg check.
var checkCmd = &cobra.Command{
	Use:   "checks the postgres CRD presence in k8s cluster.",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple`,
	Run: func(cmd *cobra.Command, args []string) {
		check()
	},
}

// check validates postgresql CRD registered or not.
func check() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	apiexclient, err := apiextbeta1.NewForConfig(config)
	if(err!=nil){
		panic(err)
	}

	crdInfo,_:=apiexclient.CustomResourceDefinitions().Get(postgresCrdName,metav1.GetOptions{})

	if(crdInfo.Name == postgresConstants.PostgresCRDResouceName){
		fmt.Println("postgresql CRD successfully registered.")
	} else {
		fmt.Println("postgresql CRD not registered.")
	}
}


func init() {
	rootCmd.AddCommand(checkCmd)
}
