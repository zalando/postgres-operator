package controller

import (
	"fmt"
	"log"
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

    "github.bus.zalan.do/acid/postgres-operator/pkg/spec"
    "github.bus.zalan.do/acid/postgres-operator/pkg/etcd"
)

const (
	ACTION_DELETE = "delete"
	ACTION_UPDATE = "update"
	ACTION_ADD    = "add"
)

type podEvent struct {
	namespace  string
	name       string
	actionType string
}

type podWatcher struct {
	podNamespace  string
	podName       string
	eventsChannel chan podEvent
	subscribe     bool
}

type SpiloController struct {
	podEvents     chan podEvent
	podWatchers   chan podWatcher
	SpiloClient   *rest.RESTClient
	Clientset     *kubernetes.Clientset
    etcdApiClient *etcd.EtcdClient

	spiloInformer cache.SharedIndexInformer
	podInformer   cache.SharedIndexInformer
}

func podsListWatch(client *kubernetes.Clientset) *cache.ListWatch {
	return cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "pods", api.NamespaceAll, fields.Everything())
}

func newController(spiloClient *rest.RESTClient, clientset *kubernetes.Clientset, etcdClient *etcd.EtcdClient) *SpiloController {
	spiloController := &SpiloController{
		SpiloClient:   spiloClient,
		Clientset:     clientset,
        etcdApiClient: etcdClient,
	}

	spiloInformer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(spiloClient, "spilos", api.NamespaceAll, fields.Everything()),
		&spec.Spilo{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	spiloInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    spiloController.spiloAdd,
		UpdateFunc: spiloController.spiloUpdate,
		DeleteFunc: spiloController.spiloDelete,
	})

	podInformer := cache.NewSharedIndexInformer(
		podsListWatch(clientset),
		&v1.Pod{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    spiloController.podAdd,
		UpdateFunc: spiloController.podUpdate,
		DeleteFunc: spiloController.podDelete,
	})

	spiloController.spiloInformer = spiloInformer
	spiloController.podInformer = podInformer

	spiloController.podEvents = make(chan podEvent)

	return spiloController
}

func (d *SpiloController) podAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  pod.Namespace,
		name:       pod.Name,
		actionType: ACTION_ADD,
	}
}

func (d *SpiloController) podDelete(obj interface{}) {
	pod := obj.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  pod.Namespace,
		name:       pod.Name,
		actionType: ACTION_DELETE,
	}
}

func (d *SpiloController) podUpdate(old, cur interface{}) {
	oldPod := old.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  oldPod.Namespace,
		name:       oldPod.Name,
		actionType: ACTION_UPDATE,
	}
}

func (z *SpiloController) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	wg.Add(1)

	if err := EnsureSpiloThirdPartyResource(z.Clientset); err != nil {
		log.Fatalf("Couldn't create ThirdPartyResource: %s", err)
	}

	go z.spiloInformer.Run(stopCh)
	go z.podInformer.Run(stopCh)
	go z.podWatcher(stopCh)

	<-stopCh
}

func (z *SpiloController) spiloAdd(obj interface{}) {
	spilo := obj.(*spec.Spilo)

	clusterName := (*spilo).Metadata.Name
	ns := (*spilo).Metadata.Namespace

	//TODO: check if object already exists before creating
	z.CreateEndPoint(ns, clusterName)
	z.CreateService(ns, clusterName)
	z.CreateSecrets(ns, clusterName)
	z.CreateStatefulSet(spilo)
}

func (z *SpiloController) spiloUpdate(old, cur interface{}) {
	oldSpilo := old.(*spec.Spilo)
	curSpilo := cur.(*spec.Spilo)

	if oldSpilo.Spec.NumberOfInstances != curSpilo.Spec.NumberOfInstances {
		z.UpdateStatefulSet(curSpilo)
	}

	if oldSpilo.Spec.DockerImage != curSpilo.Spec.DockerImage {
        log.Printf("Updating DockerImage: %s.%s",
            curSpilo.Metadata.Namespace,
            curSpilo.Metadata.Name)

		z.UpdateStatefulSetImage(curSpilo)
	}

	log.Printf("Update spilo old: %+v\ncurrent: %+v", *oldSpilo, *curSpilo)
}

func (z *SpiloController) spiloDelete(obj interface{}) {
	spilo := obj.(*spec.Spilo)

	err := z.DeleteStatefulSet(spilo.Metadata.Namespace, spilo.Metadata.Name)
	if err != nil {
		log.Printf("Error while deleting stateful set: %+v", err)
	}
}

func (z *SpiloController) DeleteStatefulSet(ns, clusterName string) error {
	orphanDependents := false
	deleteOptions := v1.DeleteOptions{
		OrphanDependents: &orphanDependents,
	}

	listOptions := v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "spilo-cluster", clusterName),
	}

	podList, err := z.Clientset.Pods(ns).List(listOptions)
	if err != nil {
		log.Printf("Error: %+v", err)
	}

	err = z.Clientset.StatefulSets(ns).Delete(clusterName, &deleteOptions)
	if err != nil {
		return err
	}
	log.Printf("StatefulSet %s.%s has been deleted\n", ns, clusterName)

	for _, pod := range podList.Items {
		err = z.Clientset.Pods(pod.Namespace).Delete(pod.Name, &deleteOptions)
		if err != nil {
			log.Printf("Error while deleting Pod %s: %+v", pod.Name, err)
			return err
		}

		log.Printf("Pod %s.%s has been deleted\n", pod.Namespace, pod.Name)
	}

	serviceList, err := z.Clientset.Services(ns).List(listOptions)
	if err != nil {
		return err
	}

	for _, service := range serviceList.Items {
		err = z.Clientset.Services(service.Namespace).Delete(service.Name, &deleteOptions)
		if err != nil {
			log.Printf("Error while deleting Service %s: %+v", service.Name, err)

			return err
		}

		log.Printf("Service %s.%s has been deleted\n", service.Namespace, service.Name)
	}

    z.etcdApiClient.DeleteEtcdKey(clusterName)

	return nil
}

func (z *SpiloController) UpdateStatefulSet(spilo *spec.Spilo) {
	ns := (*spilo).Metadata.Namespace

	statefulSet := z.createSetFromSpilo(spilo)
	_, err := z.Clientset.StatefulSets(ns).Update(&statefulSet)

	if err != nil {
		log.Printf("Error while updating StatefulSet: %s", err)
	}
}

func (z *SpiloController) UpdateStatefulSetImage(spilo *spec.Spilo) {
	ns := (*spilo).Metadata.Namespace

	z.UpdateStatefulSet(spilo)

	listOptions := v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "spilo-cluster", (*spilo).Metadata.Name),
	}

	pods, err := z.Clientset.Pods(ns).List(listOptions)
	if err != nil {
		log.Printf("Error while getting pods: %s", err)
	}

	orphanDependents := true
	deleteOptions := v1.DeleteOptions{
		OrphanDependents: &orphanDependents,
	}

	var masterPodName string
	for _, pod := range pods.Items {
		log.Printf("Pod processing: %s", pod.Name)

		role, ok := pod.Labels["spilo-role"]
		if ok == false {
			log.Println("No spilo-role label")
			continue
		}
		if role == "master" {
			masterPodName = pod.Name
			log.Printf("Skipping master: %s", masterPodName)
			continue
		}

		err := z.Clientset.Pods(ns).Delete(pod.Name, &deleteOptions)
		if err != nil {
			log.Printf("Error while deleting Pod %s.%s: %s", pod.Namespace, pod.Name, err)
		} else {
			log.Printf("Pod deleted: %s.%s", pod.Namespace, pod.Name)
		}

        w1 := podWatcher{
            podNamespace: pod.Namespace,
            podName: pod.Name,
            eventsChannel: make(chan podEvent, 1),
            subscribe: true,
        }

        log.Printf("Watching pod %s.%s being recreated", pod.Namespace, pod.Name)
        z.podWatchers <- w1
        for e := range w1.eventsChannel {
            if e.actionType == ACTION_ADD { break }
        }

        log.Printf("Pod %s.%s has been recreated", pod.Namespace, pod.Name)
	}

	//TODO: do manual failover
	err = z.Clientset.Pods(ns).Delete(masterPodName, &deleteOptions)
	if err != nil {
		log.Printf("Error while deleting Pod %s.%s: %s", ns, masterPodName, err)
	} else {
		log.Printf("Pod deleted: %s.%s", ns, masterPodName)
	}
}

func (z *SpiloController) podWatcher(stopCh <-chan struct{}) {
	//TODO: mind the namespace of the pod

	watchers := make(map[string] podWatcher)
	for {
		select {
		case watcher := <-z.podWatchers:
			if watcher.subscribe {
				watchers[watcher.podName] = watcher
			} else {
				close(watcher.eventsChannel)
				delete(watchers, watcher.podName)
			}
		case event := <-z.podEvents:
            log.Printf("Pod watcher event: %s.%s - %s", event.namespace, event.name, event.actionType)
            log.Printf("Current watchers: %+v", watchers)
			podWatcher, ok := watchers[event.name]
			if ok == false {
				continue
			}

			podWatcher.eventsChannel <- event
		}
	}
}
