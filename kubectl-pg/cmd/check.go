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

	"github.com/spf13/cobra"
	postgresConstants "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// checkCmd represent kubectl pg check.
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Checks the Postgres operator is installed in the k8s cluster",
	Long: `Checks that the Postgres CRD is registered in a k8s cluster.
This means that the operator pod was able to start normally.`,
	Run: func(cmd *cobra.Command, args []string) {
		check()
	},
	Example: `
kubectl pg check
`,
}

// check validates postgresql CRD registered or not.
func check() *v1.CustomResourceDefinition {
	config := getConfig()
	apiExtClient, err := apiextv1.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	crdInfo, err := apiExtClient.CustomResourceDefinitions().Get(context.TODO(), postgresConstants.PostgresCRDResouceName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	if crdInfo.Name == postgresConstants.PostgresCRDResouceName {
		fmt.Printf("Postgres Operator is installed in the k8s cluster.\n")
	} else {
		fmt.Printf("Postgres Operator is not installed in the k8s cluster.\n")
	}
	return crdInfo
}

func init() {
	rootCmd.AddCommand(checkCmd)
}
