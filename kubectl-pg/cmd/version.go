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
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

var KubectlPgVersion string = "1.0"

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version of kubectl-pg & postgres-operator",
	Long:  `version of kubectl-pg and current running postgres-operator`,
	Run: func(cmd *cobra.Command, args []string) {
		namespace, err := cmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}
		version(namespace)
	},
	Example: `
#Lists the version of kubectl pg plugin and postgres operator in current namespace
kubectl pg version

#Lists the version of kubectl pg plugin and postgres operator in provided namespace
kubectl pg version -n namespace01
`,
}

func version(namespace string) {
	fmt.Printf("kubectl-pg: %s\n", KubectlPgVersion)

	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	operatorDeployment := getPostgresOperator(client)
	if operatorDeployment.Name == "" {
		log.Fatal("make sure zalando's postgres operator is running")
	}
	operatorImage := operatorDeployment.Spec.Template.Spec.Containers[0].Image
	imageDetails := strings.Split(operatorImage, ":")
	imageSplit := len(imageDetails)
	imageVersion := imageDetails[imageSplit-1]
	fmt.Printf("Postgres-Operator: %s\n", imageVersion)
}

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().StringP("namespace", "n", DefaultNamespace, "provide the namespace.")
}
