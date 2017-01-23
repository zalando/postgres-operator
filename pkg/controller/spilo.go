package controller

import (
	"encoding/json"
	"log"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/meta"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/rest"
    "net/url"
	"fmt"
    "strings"
)

type SpiloOperator struct {
	Options

	ClientSet  *kubernetes.Clientset
	Client     *rest.RESTClient
	Supervisor *SpiloSupervisor
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
	etcdHostOutside = fmt.Sprintf("http://%s:%d", strings.Split(nodeurl.Host, ":")[0], ports.NodePort)

	spiloClient, err := newKubernetesSpiloClient(config)
	if err != nil {
		log.Fatalf("Couldn't create Spilo client: %s", err)
	}

	operator := &SpiloOperator{
		Options:     options,
		ClientSet:   clientSet,
		Client:      spiloClient,
		Supervisor:  newSupervisor(spiloClient, clientSet),
	}

	return operator
}

func (o *SpiloOperator) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	log.Printf("Spilo operator %v\n", VERSION)

	go o.Supervisor.Run(stopCh, wg)

	log.Println("Started working in background")
}

// The code below is used only to work around a known problem with third-party
// resources and ugorji. If/when these issues are resolved, the code below
// should no longer be required.
//

func (s *Spilo) GetObjectKind() unversioned.ObjectKind {
	return &s.TypeMeta
}

func (s *Spilo) GetObjectMeta() meta.Object {
	return &s.Metadata
}
func (sl *SpiloList) GetObjectKind() unversioned.ObjectKind {
	return &sl.TypeMeta
}

func (sl *SpiloList) GetListMeta() unversioned.List {
	return &sl.Metadata
}

type SpiloListCopy SpiloList
type SpiloCopy Spilo

func (e *Spilo) UnmarshalJSON(data []byte) error {
	tmp := SpiloCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := Spilo(tmp)
	*e = tmp2

	return nil
}

func (el *SpiloList) UnmarshalJSON(data []byte) error {
	tmp := SpiloListCopy{}
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	tmp2 := SpiloList(tmp)
	*el = tmp2

	return nil
}
