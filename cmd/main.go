package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.bus.zalan.do/acid/postgres-operator/pkg/controller"
	"github.bus.zalan.do/acid/postgres-operator/pkg/spec"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/config"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
)

var (
	KubeConfigFile string
	podNamespace   string
	configMapName  spec.NamespacedName
	OutOfCluster   bool
	version        string
)

func init() {
	flag.StringVar(&KubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
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
		log.Fatalf("Can't get REST config: %s", err)
	}

	client, err := k8sutil.KubernetesClient(restConfig)
	if err != nil {
		log.Fatalf("Can't create client: %s", err)
	}

	restClient, err := k8sutil.KubernetesRestClient(restConfig)

	return &controller.Config{
		KubeClient: client,
		RestClient: restClient,
	}
}

func main() {
	configMapData := make(map[string]string)
	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	controllerConfig := ControllerConfig()

	if configMapName != (spec.NamespacedName{}) {
		configMap, err := controllerConfig.KubeClient.ConfigMaps(configMapName.Namespace).Get(configMapName.Name)
		if err != nil {
			panic(err)
		}

		configMapData = configMap.Data
	} else {
		log.Printf("No ConfigMap specified. Loading default values")
	}
	if configMapData["namespace"] == "" { // Namespace in ConfigMap has priority over env var
		configMapData["namespace"] = podNamespace
	}
	cfg := config.NewFromMap(configMapData)

	log.Printf("Config: %s", cfg.MustMarshal())

	c := controller.New(controllerConfig, cfg)
	c.Run(stop, wg)

	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
