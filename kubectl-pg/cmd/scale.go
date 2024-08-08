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
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// scaleCmd represents the scale command
var scaleCmd = &cobra.Command{
	Use:   "scale",
	Short: "Add/remove pods to a Postgres cluster",
	Long: `Scales the postgres objects using cluster-name.
Scaling to 0 leads to down time.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, err := cmd.Flags().GetString("cluster")
		if err != nil {
			log.Fatal(err)
		}
		namespace, err := cmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}

		if len(args) > 0 {
			numberOfInstances, err := strconv.Atoi(args[0])
			if err != nil {
				log.Fatal(err)
			}
			scale(int32(numberOfInstances), clusterName, namespace)
		} else {
			fmt.Println("Please enter number of instances to scale.")
		}

	},
	Example: `
#Usage
kubectl pg scale [NUMBER-OF-INSTANCES] -c [CLUSTER-NAME] -n [NAMESPACE]

#Scales the number of instances of the provided cluster
kubectl pg scale 5 -c cluster01
`,
}

func scale(numberOfInstances int32, clusterName string, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	postgresql, err := postgresConfig.Postgresqls(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	minInstances, maxInstances := allowedMinMaxInstances(config)

	if minInstances == -1 && maxInstances == -1 {
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else if numberOfInstances <= maxInstances && numberOfInstances >= minInstances {
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else if minInstances == -1 && numberOfInstances < postgresql.Spec.NumberOfInstances ||
		maxInstances == -1 && numberOfInstances > postgresql.Spec.NumberOfInstances {
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else {
		log.Fatalf("cannot scale to the provided instances as they don't adhere to MIN_INSTANCES: %v and MAX_INSTANCES: %v provided in configmap or operatorconfiguration", maxInstances, minInstances)
	}

	if numberOfInstances == 0 {
		fmt.Printf("Scaling to zero leads to down time. please type %s/%s and hit Enter this serves to confirm the action\n", namespace, clusterName)
		confirmAction(clusterName, namespace)
	}

	patchInstances := scalePatch(numberOfInstances)
	UpdatedPostgres, err := postgresConfig.Postgresqls(namespace).Patch(context.TODO(), postgresql.Name, types.MergePatchType, patchInstances, metav1.PatchOptions{})
	if err != nil {
		log.Fatal(err)
	}

	if UpdatedPostgres.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("scaled postgresql %s/%s to %d instances\n", UpdatedPostgres.Namespace, UpdatedPostgres.Name, UpdatedPostgres.Spec.NumberOfInstances)
		return
	}
	fmt.Printf("postgresql %s is unchanged.\n", postgresql.Name)
}

func scalePatch(value int32) []byte {
	instances := map[string]map[string]int32{"spec": {"numberOfInstances": value}}
	patchInstances, err := json.Marshal(instances)
	if err != nil {
		log.Fatal(err, "unable to parse patch for scale")
	}
	return patchInstances
}

func allowedMinMaxInstances(config *rest.Config) (int32, int32) {
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	operator := getPostgresOperator(k8sClient)

	operatorContainer := operator.Spec.Template.Spec.Containers
	var configMapName, operatorConfigName string
	// -1 indicates no limitations for min/max instances
	minInstances := -1
	maxInstances := -1
	for _, envData := range operatorContainer[0].Env {
		if envData.Name == "CONFIG_MAP_NAME" {
			configMapName = envData.Value
		}
		if envData.Name == "POSTGRES_OPERATOR_CONFIGURATION_OBJECT" {
			operatorConfigName = envData.Value
		}
	}

	if operatorConfigName == "" {
		configMap, err := k8sClient.CoreV1().ConfigMaps(operator.Namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}

		configMapData := configMap.Data
		for key, value := range configMapData {
			if key == "min_instances" {
				minInstances, err = strconv.Atoi(value)
				if err != nil {
					log.Fatalf("invalid min instances in configmap %v", err)
				}
			}

			if key == "max_instances" {
				maxInstances, err = strconv.Atoi(value)
				if err != nil {
					log.Fatalf("invalid max instances in configmap %v", err)
				}
			}
		}
	} else if configMapName == "" {
		pgClient, err := PostgresqlLister.NewForConfig(config)
		if err != nil {
			log.Fatal(err)
		}

		operatorConfig, err := pgClient.OperatorConfigurations(operator.Namespace).Get(context.TODO(), operatorConfigName, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("unable to read operator configuration %v", err)
		}

		minInstances = int(operatorConfig.Configuration.MinInstances)
		maxInstances = int(operatorConfig.Configuration.MaxInstances)
	}
	return int32(minInstances), int32(maxInstances)
}

func init() {
	namespace := getCurrentNamespace()
	scaleCmd.Flags().StringP("namespace", "n", namespace, "namespace of the cluster to be scaled")
	scaleCmd.Flags().StringP("cluster", "c", "", "provide the cluster name.")
	rootCmd.AddCommand(scaleCmd)
}
