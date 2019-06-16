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
)

// extVolumeCmd represents the extVolume command
var extVolumeCmd = &cobra.Command{
	Use:   "ext-volume",
	Short: "Extend Volume command to extend k8s postgresql objects by using cluster name",
	Long:  `Extend volume command extends the volume of the postgres cluster. Bit volume cannot be shrinked.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("clusterName")
		if len(args) > 0 {
			volume := args[0]
			extVolume(volume,clusterName)
		}
	},
}

// extend volume with provided size & cluster name
func extVolume(extendVolume string, clusterName string) {
	config := getConfig()
	postgresConfig,err:=PostgresqlLister.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	namespace := getCurrentNamespace()
	postgresql,err := postgresConfig.Postgresqls(namespace).Get(clusterName,metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	oldSize, err := resource.ParseQuantity(postgresql.Spec.Volume.Size)
	if err != nil {
		panic(err)
	}
	newSize, err := resource.ParseQuantity(extendVolume)
	if err != nil {
		panic(err)
	}
	if newSize.Value() >= oldSize.Value() {
		postgresql.Spec.Volume.Size = extendVolume
		response, err := postgresConfig.Postgresqls(namespace).Update(postgresql)
		if err != nil {
			panic(err)
		}
		if postgresql.ResourceVersion != response.ResourceVersion {
			fmt.Printf("%s volume is extended with %s.\n", response.Name,extendVolume)
		} else {
			fmt.Printf("%s is unchanged.\n", response.Name)
		}
	} else {
		fmt.Println("volume cannot be shrinked.")
	}
}

func init() {
	extVolumeCmd.Flags().StringP("clusterName", "c", "", "provide cluster name.")
	rootCmd.AddCommand(extVolumeCmd)
}
