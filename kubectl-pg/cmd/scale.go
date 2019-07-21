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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		clusterName, err := cmd.Flags().GetString("clusterName")
		namespace, err := cmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}
		if len(args) > 0 {
			numberofInstances, err := strconv.Atoi(args[0])
			if err != nil {
				log.Fatal(err)
			}
			scale(int32(numberofInstances), clusterName, namespace)
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
	postgresql.Kind = "postgresql"
	postgresql.APIVersion = "acid.zalan.do/v1"
	UpdatedPostgres, err := postgresConfig.Postgresqls(namespace).Update(postgresql)
	if err != nil {
		log.Fatal(err)
	}
	if UpdatedPostgres.ResourceVersion != postgresql.ResourceVersion {
		fmt.Printf("scaled postgresql %s/%s to %d instances\n", UpdatedPostgres.Namespace, UpdatedPostgres.Name, UpdatedPostgres.Spec.NumberOfInstances)
		return
	}
	fmt.Printf("postgresql %s is unchanged.\n", postgresql.Name)
}

func allowedMinMaxInstances(config *rest.Config) (int32, int32){
	k8sClient,err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	operator,err :=k8sClient.AppsV1().Deployments(getCurrentNamespace()).Get("postgres-operator",metav1.GetOptions{})
	if err!= nil {
		log.Fatal(err)
	}
	operatorContainer:=operator.Spec.Template.Spec.Containers
	var configMapName, operatorConfigName string
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
		configMap, err := k8sClient.CoreV1().ConfigMaps(getCurrentNamespace()).Get(configMapName, metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}
		configMapData := configMap.Data
		for key, value := range configMapData {
			if key == "min_instances" {
				minInstances,_ = strconv.Atoi(value)
			}
			if key == "max_instances" {
				maxInstances,_ = strconv.Atoi(value)
			}
		}
	} else if configMapName == "" {
		pgClient,err :=	PostgresqlLister.NewForConfig(config)
		if err != nil {
			log.Fatal(err)
		}
		operatorConfig,_ := pgClient.OperatorConfigurations("s").Get(operatorConfigName,metav1.GetOptions{})
		minInstances = int(operatorConfig.Configuration.MinInstances)
		maxInstances = int(operatorConfig.Configuration.MaxInstances)
	}
	return int32(minInstances), int32(maxInstances)
}

func init() {
	namespace := getCurrentNamespace()
	scaleCmd.Flags().StringP("namespace", "n", namespace, "provide the namespace.")
	scaleCmd.Flags().StringP("clusterName", "c", "", "provide the cluster name.")
	rootCmd.AddCommand(scaleCmd)
}
