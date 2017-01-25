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


func getEtcdServiceName(cls *kubernetes.Clientset, config *rest.Config, outOfCluster bool) (etcdServiceName string) {
	etcdService, _ := cls.Services("default").Get("etcd-client")
	if outOfCluster {
		ports := etcdService.Spec.Ports[0]
		if ports.NodePort == 0 {
			log.Fatal("Etcd port is not exposed\nHint: add NodePort to your Etcd service")
		}
		nodeurl, _ := url.Parse(config.Host)
		etcdServiceName = fmt.Sprintf("http://%s:%d", strings.Split(nodeurl.Host, ":")[0], ports.NodePort)
	} else {
		if len(etcdService.Spec.Ports) != 1 {
			log.Fatal("Can't find Etcd service named 'etcd-client'")
		}
		etcdServiceName = fmt.Sprintf("%s.%s.svc.cluster.local", etcdService.Name, etcdService.Namespace)
	}
	return
}

func New(options Options) *SpiloOperator {
	config := KubernetesConfig(options)

	spiloClient, err := newKubernetesSpiloClient(config)
	if err != nil {
		log.Fatalf("Couldn't create Spilo client: %s", err)
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Couldn't create Kubernetes client: %s", err)
	}

	etcdClient := etcd.NewEctdClient(getEtcdServiceName(clientSet, config, options.OutOfCluster))

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
