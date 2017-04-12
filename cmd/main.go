package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.bus.zalan.do/acid/postgres-operator/pkg/controller"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/config"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
)

var (
	KubeConfigFile string
	podNamespace   string
	OutOfCluster   bool
	version        string
	cfg            *config.Config
)

func init() {
	flag.StringVar(&KubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.Parse()

	podNamespace = os.Getenv("MY_POD_NAMESPACE")
	if len(podNamespace) == 0 {
		podNamespace = "default"
	}

	cfg = config.LoadFromEnv()
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
		PodNamespace: podNamespace, //TODO: move to config.Config
		KubeClient:   client,
		RestClient:   restClient,
	}
}

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)
	log.Printf("Config: %s", cfg.MustMarshal())

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	controllerConfig := ControllerConfig()

	c := controller.New(controllerConfig, cfg)
	c.Run(stop, wg)

	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
