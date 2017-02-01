package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.bus.zalan.do/acid/postgres-operator/pkg/controller"
	"github.com/spf13/pflag"
)

var options controller.Options
var version string

func init() {
	pflag.StringVar(&options.KubeConfig, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	pflag.BoolVar(&options.OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
}

func main() {
	// Set logging output to standard console out
	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	c := controller.New(options)
	c.Run(stop, wg)

	sig := <-sigs // Wait for signals (this hangs until a signal arrives)
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
