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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// extVolumeCmd represents the extVolume command
var extVolumeCmd = &cobra.Command{
	Use:   "ext-volume",
	Short: "Increases the volume size of a given Postgres cluster",
	Long:  `Extends the volume of the postgres cluster. But volume cannot be shrinked.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		if len(args) > 0 {
			volume := args[0]
			extVolume(volume, clusterName)
		} else {
			fmt.Println("please enter the cluster name with -c flag & volume in desired units")
		}
	},
	Example: `
#Extending the volume size of provided cluster 
kubectl pg ext-volume 2Gi -c cluster01
`,
}

// extend volume with provided size & cluster name
func extVolume(increasedVolumeSize string, clusterName string) {
	config := getConfig()
	postgresConfig, err := PostgresqlLister.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	namespace := getCurrentNamespace()
	postgresql, err := postgresConfig.Postgresqls(namespace).Get(context.TODO(), clusterName, metav1.GetOptions{})
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

	_, err = strconv.Atoi(newSize.String())
	if err == nil {
		fmt.Println("provide the valid volume size with respective units i.e Ki, Mi, Gi")
		return
	}

	if newSize.Value() > oldSize.Value() {
		patchInstances := volumePatch(newSize)
		response, err := postgresConfig.Postgresqls(namespace).Patch(context.TODO(), postgresql.Name, types.MergePatchType, patchInstances, metav1.PatchOptions{})
		if err != nil {
			log.Fatal(err)
		}
		if postgresql.ResourceVersion != response.ResourceVersion {
			fmt.Printf("%s volume is extended to %s.\n", response.Name, increasedVolumeSize)
		} else {
			fmt.Printf("%s volume %s is unchanged.\n", response.Name, postgresql.Spec.Volume.Size)
		}
	} else if newSize.Value() == oldSize.Value() {
		fmt.Println("volume already has the desired size.")
	} else {
		fmt.Printf("volume %s size cannot be shrinked.\n", postgresql.Spec.Volume.Size)
	}
}

func volumePatch(volume resource.Quantity) []byte {
	patchData := map[string]map[string]map[string]resource.Quantity{"spec": {"volume": {"size": volume}}}
	patch, err := json.Marshal(patchData)
	if err != nil {
		log.Fatal(err, "unable to parse patch to extend volume")
	}
	return patch
}

func init() {
	extVolumeCmd.Flags().StringP("cluster", "c", "", "provide cluster name.")
	rootCmd.AddCommand(extVolumeCmd)
}
