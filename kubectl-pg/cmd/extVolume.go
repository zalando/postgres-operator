/*
Copyright © 2019 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	PostgresqlLister "github.com/zalando/postgres-operator/pkg/generated/clientset/versioned/typed/acid.zalan.do/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
)

// extVolumeCmd represents the extVolume command
var extVolumeCmd = &cobra.Command{
	Use:   "ext-volume",
	Short: "Increases the volume size of a given Postgres cluster",
	Long:  `Extends the volume of the postgres cluster. But volume cannot be shrinked.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("clusterName")
		if len(args) > 0 {
			volume := args[0]
			extVolume(volume, clusterName)
		}
	},
	Example: "kubectl pg ext-volume [VOLUME] -c [CLUSTER-NAME]",
}

// extend volume with provided size & cluster name
func extVolume(increasedVolumeSize string, clusterName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	namespace := getCurrentNamespace()
	postgresql, err := postgresConfig.Postgresqls(namespace).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	oldSize, err := resource.ParseQuantity(postgresql.Spec.Volume.Size)
	if err != nil {
		log.Fatal(err)
	}
	newSize, err := resource.ParseQuantity(increasedVolumeSize)
	if err != nil {
		log.Fatal(err)
	}
	if newSize.Value() >= oldSize.Value() {
		postgresql.APIVersion = "acid.zalan.do/v1"
		postgresql.Kind = "postgresql"
		postgresql.Spec.Volume.Size = increasedVolumeSize
		response, err := postgresConfig.Postgresqls(namespace).Update(postgresql)
		if err != nil {
			log.Fatal(err)
		}
		if postgresql.ResourceVersion != response.ResourceVersion {
			fmt.Printf("%s volume is extended with %s.\n", response.Name, increasedVolumeSize)
		} else {
			fmt.Printf("%s volume %s is unchanged.\n", response.Name, postgresql.Spec.Volume.Size)
		}
	} else {
		fmt.Printf("volume %s size cannot be shrinked.\n",postgresql.Spec.Volume.Size)
	}
}

func init() {
	extVolumeCmd.Flags().StringP("clusterName", "c", "", "provide cluster name.")
	rootCmd.AddCommand(extVolumeCmd)
}
