package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zalando-incubator/postgres-operator/pkg/controller"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

var (
	KubeConfigFile string
	OutOfCluster   bool
	version        string

	config controller.Config
)

func init() {
	flag.StringVar(&KubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.BoolVar(&config.NoDatabaseAccess, "nodatabaseaccess", false, "Disable all access to the database from the operator side.")
	flag.BoolVar(&config.NoTeamsAPI, "noteamsapi", false, "Disable all access to the teams API")
	flag.Parse()

	config.Namespace = os.Getenv("MY_POD_NAMESPACE")
	if config.Namespace == "" {
		config.Namespace = "default"
	}

	configMap := os.Getenv("CONFIG_MAP_NAME")
	if configMap != "" {
		err := config.ConfigMapName.Decode(configMap)
		if err != nil {
			log.Fatalf("incorrect config map name")
		}
	}
}

func main() {
	var err error

	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	config.RestConfig, err = k8sutil.RestConfig(KubeConfigFile, OutOfCluster)
	if err != nil {
		log.Fatalf("couldn't get REST config: %v", err)
	}

	c := controller.NewController(&config)

	c.Run(stop, wg)

	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
