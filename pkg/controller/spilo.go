package controller

import (
	"log"
	"sync"
	"net/url"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

    "github.bus.zalan.do/acid/postgres-operator/pkg/etcd"
)

type SpiloOperator struct {
	Options

	ClientSet  *kubernetes.Clientset
	Client     *rest.RESTClient
	Controller *SpiloController
    EtcdClient *etcd.EtcdClient
}

func New(options Options) *SpiloOperator {
	config := KubernetesConfig(options)

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Couldn't create Kubernetes client: %s", err)
	}

    etcdService, _ := clientSet.Services("default").Get("etcd-client")
    if len(etcdService.Spec.Ports) != 1 {
        log.Fatalln("Can't find Etcd cluster")
    }
    ports := etcdService.Spec.Ports[0]
	nodeurl, _ := url.Parse(config.Host)

    if ports.NodePort == 0 {
        log.Fatalln("Etcd port is not exposed")
    }

	etcdHostOutside := fmt.Sprintf("http://%s:%d", strings.Split(nodeurl.Host, ":")[0], ports.NodePort)

	spiloClient, err := newKubernetesSpiloClient(config)
	if err != nil {
		log.Fatalf("Couldn't create Spilo client: %s", err)
	}

    etcdClient := etcd.NewEctdClient(etcdHostOutside)

	operator := &SpiloOperator{
		Options:     options,
		ClientSet:   clientSet,
		Client:      spiloClient,
		Controller:  newController(spiloClient, clientSet, etcdClient),
	}

	return operator
}

func (o *SpiloOperator) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	log.Printf("Spilo operator %v\n", VERSION)

	go o.Controller.Run(stopCh, wg)

	log.Println("Started working in background")
}
