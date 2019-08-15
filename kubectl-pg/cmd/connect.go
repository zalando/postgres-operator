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
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"log"
	"os"
)

// connectCmd represents the kubectl pg connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connects to the shell prompt, psql prompt of postgres cluster",
	Long: `Connects to the shell prompt, psql prompt of postgres cluster and also to specified replica or master.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName,_ := cmd.Flags().GetString("cluster")
		master,_ := cmd.Flags().GetBool("master")
		replica,_ := cmd.Flags().GetString("replica")
		psql,_ := cmd.Flags().GetBool("psql")
		user,_ := cmd.Flags().GetString("user")

		if psql {
			if user == "" {
				log.Fatal("please provide user for psql prompt")
			}
		}

		connect(clusterName,master,replica,psql,user)
	},
}

func connect(clusterName string,master bool,replica string,psql bool,user string) {
	config := getConfig()
	client,er := kubernetes.NewForConfig(config)
	if er != nil {
		log.Fatal(er)
	}

	podName := getPodName(clusterName,master,replica)
	execRequest := &rest.Request{}

	if psql {
		execRequest = client.CoreV1().RESTClient().Post().Resource("pods").
			Name(podName).
			Namespace("default").
			SubResource("exec").
			Param("container", "postgres").
			Param("command", "psql").
			Param("command", "-U").
			Param("command", user).
			Param("stdin", "true").
			Param("stdout", "true").
			Param("stderr", "true").
			Param("tty", "true")
	} else {
		execRequest = client.CoreV1().RESTClient().Post().Resource("pods").
			Name(podName).
			Namespace("default").
			SubResource("exec").
			Param("container", "postgres").
			Param("command", "su").
			Param("command", "postgres").
			Param("stdin", "true").
			Param("stdout", "true").
			Param("stderr", "true").
			Param("tty", "true")
	}

	exec,err := remotecommand.NewSPDYExecutor(config,"POST",execRequest.URL())
	if err != nil {
		log.Fatal(err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin: os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty: true,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	connectCmd.Flags().StringP("cluster", "c", "", "provide the cluster name.")
	connectCmd.Flags().BoolP("master", "m", false, "connect to master.")
	connectCmd.Flags().StringP("replica", "r","", "connect to replica.")
	connectCmd.Flags().BoolP("psql","p",false,"connect to psql prompt.")
	connectCmd.Flags().StringP("user","u","","provide user.")
	rootCmd.AddCommand(connectCmd)
}
