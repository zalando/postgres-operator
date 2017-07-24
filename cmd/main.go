package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zalando-incubator/postgres-operator/pkg/controller"
	"github.com/zalando-incubator/postgres-operator/pkg/spec"
	"github.com/zalando-incubator/postgres-operator/pkg/util/k8sutil"
)

var (
	KubeConfigFile   string
	podNamespace     string
	configMapName    spec.NamespacedName
	OutOfCluster     bool
	noTeamsAPI       bool
	noDatabaseAccess bool
	version          string
)

func init() {
	flag.StringVar(&KubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.BoolVar(&noDatabaseAccess, "nodatabaseaccess", false, "Disable all access to the database from the operator side.")
	flag.BoolVar(&noTeamsAPI, "noteamsapi", false, "Disable all access to the teams API")
	flag.Parse()

	podNamespace = os.Getenv("MY_POD_NAMESPACE")
	if podNamespace == "" {
		podNamespace = "default"
	}

	configMap := os.Getenv("CONFIG_MAP_NAME")
	if configMap != "" {
		configMapName.Decode(configMap)
	}
}

func ControllerConfig() *controller.Config {
	restConfig, err := k8sutil.RestConfig(KubeConfigFile, OutOfCluster)
	if err != nil {
		log.Fatalf("couldn't get REST config: %v", err)
	}

	return &controller.Config{
		RestConfig:       restConfig,
		NoDatabaseAccess: noDatabaseAccess,
		NoTeamsAPI:       noTeamsAPI,
		ConfigMapName:    configMapName,
		Namespace:        podNamespace,
	}
}

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	c := controller.New(ControllerConfig())
	c.Run(stop, wg)

	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
