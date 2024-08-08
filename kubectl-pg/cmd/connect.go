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
	"log"
	"os"
	user "os/user"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// connectCmd represents the kubectl pg connect command
var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connects to the shell prompt, psql prompt of postgres cluster",
	Long:  `Connects to the shell prompt, psql prompt of postgres cluster and also to specified replica or master.`,
	Run: func(cmd *cobra.Command, args []string) {
		clusterName, _ := cmd.Flags().GetString("cluster")
		master, _ := cmd.Flags().GetBool("master")
		replica, _ := cmd.Flags().GetString("replica")
		psql, _ := cmd.Flags().GetBool("psql")
		userName, _ := cmd.Flags().GetString("user")
		dbName, _ := cmd.Flags().GetString("database")

		if psql {
			if userName == "" {
				userInfo, err := user.Current()
				if err != nil {
					log.Fatal(err)
				}
				userName = userInfo.Username
			}
		}
		if dbName == "" {
			dbName = userName
		}

		connect(clusterName, master, replica, psql, userName, dbName)
	},
	Example: `
#connects to the master of postgres cluster
kubectl pg connect -c cluster -m

#connects to the random replica of postgres cluster
kubectl pg connect -c cluster

#connects to the provided replica number of postgres cluster
kubectl pg connect -c cluster -r 2

#connects to psql prompt of master for provided postgres cluster with current shell user
kubectl pg connect -c cluster -p -m

#connects to psql prompt of random replica for provided postgres cluster with provided user and db
kubectl pg connect -c cluster -p -u user01 -d db01
`,
}

func connect(clusterName string, master bool, replica string, psql bool, user string, dbName string) {
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	podName := getPodName(clusterName, master, replica)
	var execRequest *rest.Request

	if psql {
		execRequest = client.CoreV1().RESTClient().Post().Resource("pods").
			Name(podName).
			Namespace(getCurrentNamespace()).
			SubResource("exec").
			Param("container", "postgres").
			Param("command", "psql").
			Param("command", dbName).
			Param("command", user).
			Param("stdin", "true").
			Param("stdout", "true").
			Param("stderr", "true").
			Param("tty", "true")
	} else {
		execRequest = client.CoreV1().RESTClient().Post().Resource("pods").
			Name(podName).
			Namespace(getCurrentNamespace()).
			SubResource("exec").
			Param("container", "postgres").
			Param("command", "su").
			Param("command", "postgres").
			Param("stdin", "true").
			Param("stdout", "true").
			Param("stderr", "true").
			Param("tty", "true")
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", execRequest.URL())
	if err != nil {
		log.Fatal(err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty:    true,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	connectCmd.Flags().StringP("cluster", "c", "", "provide the cluster name.")
	connectCmd.Flags().BoolP("master", "m", false, "connect to master.")
	connectCmd.Flags().StringP("replica", "r", "", "connect to replica. Specify replica number.")
	connectCmd.Flags().BoolP("psql", "p", false, "connect to psql prompt.")
	connectCmd.Flags().StringP("user", "u", "", "provide user.")
	connectCmd.Flags().StringP("database", "d", "", "provide database name.")
	rootCmd.AddCommand(connectCmd)
}
