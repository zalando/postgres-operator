package main

import (
	"flag"
	"io/ioutil"
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
	kubeConfigFile string
	outOfCluster   bool
	version        string
	config         spec.ControllerConfig
)

func init() {
	flag.StringVar(&kubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&outOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.BoolVar(&config.NoDatabaseAccess, "nodatabaseaccess", false, "Disable all access to the database from the operator side.")
	flag.BoolVar(&config.NoTeamsAPI, "noteamsapi", false, "Disable all access to the teams API")
	flag.Parse()

	operatorNamespaceBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		log.Fatalf("Unable to detect operator namespace from within its pod  due to %v", err)
	}

	configMapRawName := os.Getenv("CONFIG_MAP_NAME")
	if configMapRawName != "" {

		operatorNamespace := string(operatorNamespaceBytes)
		namespacedConfigMapName := operatorNamespace + "/" + configMapRawName
		log.Printf("Looking for the operator configmap at the same namespace the operator resides. Fully qualified configmap name: %v", namespacedConfigMapName)

		err := config.ConfigMapName.Decode(namespacedConfigMapName)
		if err != nil {
			log.Fatalf("incorrect config map name: %v", namespacedConfigMapName)
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

	config.RestConfig, err = k8sutil.RestConfig(kubeConfigFile, outOfCluster)
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
