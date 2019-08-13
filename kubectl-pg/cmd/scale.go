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
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"strconv"
)

// scaleCmd represents the scale command
var scaleCmd = &cobra.Command{
	Use:   "scale",
	Short: "Add/remove pods to a Postgres cluster",
	Long: `Scales the postgres objects using cluster-name.
Scaling to 0 leads to down time.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, err := cmd.Flags().GetString("cluster")
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
	Example: "kubectl pg scale [NUMBER-OF-INSTANCES] -c [CLUSTER-NAME] -n [NAMESPACE]",
}

func scale(numberOfInstances int32, clusterName string, namespace string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	postgresql, err := postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}

	minInstances, maxInstances := allowedMinMaxInstances(config)

	if minInstances == -1 && maxInstances == -1 {
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else if numberOfInstances <= maxInstances && numberOfInstances>= minInstances {
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else if minInstances == -1 && numberOfInstances < postgresql.Spec.NumberOfInstances ||
		 maxInstances == -1 && numberOfInstances > postgresql.Spec.NumberOfInstances{
		postgresql.Spec.NumberOfInstances = numberOfInstances
	} else {
		log.Fatalf("cannot scale to the provided instances as they don't adhere to MIN_INSTANCES: %v and MAX_INSTANCES: %v provided in configmap or operatorconfiguration",maxInstances,minInstances)
	}

	if numberOfInstances == 0 {
		fmt.Printf("Scaling to zero leads to down time. please type %s/%s and hit Enter this serves to confirm the action\n", namespace, clusterName)
		confirmAction(clusterName, namespace)
	}

	patchInstances := scalePatch(numberOfInstances)
	UpdatedPostgres, err := postgresConfig.Postgresqls(namespace).Patch(postgresql.Name,types.MergePatchType, patchInstances,"")
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
	instances := map[string]map[string]int32{"spec":{"numberOfInstances": value }}
	patchInstances, err := json.Marshal(instances)
	if err != nil {
		log.Fatal(err, "unable to parse number of instances json")
	}
	return patchInstances
}

func allowedMinMaxInstances(config *rest.Config) (int32, int32){
	k8sClient,err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	var operator *v1.Deployment
	operator = getOperatorFromOtherNamespace(k8sClient)

	operatorContainer := operator.Spec.Template.Spec.Containers
	var configMapName, operatorConfigName string
	// -1 indicates no limitations for min/max instances
	minInstances := -1
	maxInstances := -1
	for _,envData := range operatorContainer[0].Env {
		if envData.Name == "CONFIG_MAP_NAME"  {
			configMapName = envData.Value
		}
		if envData.Name == "POSTGRES_OPERATOR_CONFIGURATION_OBJECT" {
			operatorConfigName = envData.Value
		}
	}

	if operatorConfigName == "" {
		configMap, err := k8sClient.CoreV1().ConfigMaps(operator.Namespace).Get(configMapName, metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}

		configMapData := configMap.Data
		for key, value := range configMapData {
			if key == "min_instances" {
				minInstances,err = strconv.Atoi(value)
				if err != nil {
					log.Fatalf("invalid min instances in configmap %v",err)
				}
			}

			if key == "max_instances" {
				maxInstances,err = strconv.Atoi(value)
				if err != nil {
					log.Fatalf("invalid max instances in configmap %v",err)
				}
			}
		}
	} else if configMapName == "" {
		pgClient,err :=	PostgresqlLister.NewForConfig(config)
		if err != nil {
			log.Fatal(err)
		}

		operatorConfig,err := pgClient.OperatorConfigurations(operator.Namespace).Get(operatorConfigName,metav1.GetOptions{})
		if err != nil {
			log.Fatalf("unable to read operator configuration %v",err)
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
