package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.bus.zalan.do/acid/postgres-operator/pkg/controller"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/constants"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/k8sutil"
	"github.bus.zalan.do/acid/postgres-operator/pkg/util/teams"
)

var (
	KubeConfigFile string
	Namespace      string
	OutOfCluster   bool
	version        string
)

func init() {
	flag.StringVar(&KubeConfigFile, "kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	flag.BoolVar(&OutOfCluster, "outofcluster", false, "Whether the operator runs in- our outside of the Kubernetes cluster.")
	flag.Parse()

	Namespace = os.Getenv("MY_POD_NAMESPACE")
	if len(Namespace) == 0 {
		Namespace = "default"
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

	teamsApi := teams.NewTeamsAPI(constants.TeamsAPIUrl)
	return &controller.Config{
		Namespace:      Namespace,
		KubeClient:     client,
		RestClient:     restClient,
		TeamsAPIClient: teamsApi,
	}
}

func main() {
	log.SetOutput(os.Stdout)
	log.Printf("Spilo operator %s\n", version)

	sigs := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // Push signals into channel

	wg := &sync.WaitGroup{} // Goroutines can add themselves to this to be waited on

	cfg := ControllerConfig()

	c := controller.New(cfg)
	c.Run(stop, wg)

	sig := <-sigs
	log.Printf("Shutting down... %+v", sig)

	close(stop) // Tell goroutines to stop themselves
	wg.Wait()   // Wait for all to be stopped
}
