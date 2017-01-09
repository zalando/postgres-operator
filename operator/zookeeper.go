package operator

import (
	"fmt"
	"log"
	"sync"
	"time"

	etcdclient "github.com/coreos/etcd/client"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/fields"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
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

type PgZooKeeper struct {
	podEvents   chan podEvent
	podWatchers chan podWatcher
	SpiloClient *rest.RESTClient
	Clientset   *kubernetes.Clientset

	spiloInformer cache.SharedIndexInformer
	podInformer   cache.SharedIndexInformer
	etcdApiClient etcdclient.KeysAPI
}

func podsListWatch(client *kubernetes.Clientset) *cache.ListWatch {
	return cache.NewListWatchFromClient(client.Core().RESTClient(), "pods", api.NamespaceAll, fields.Everything())
}

func newZookeeper(spiloClient *rest.RESTClient, clientset *kubernetes.Clientset) *PgZooKeeper {
	pgZooKeeper := &PgZooKeeper{
		SpiloClient: spiloClient,
		Clientset:   clientset,
	}

	spiloInformer := cache.NewSharedIndexInformer(
		cache.NewListWatchFromClient(spiloClient, "spilos", api.NamespaceAll, fields.Everything()),
		&Spilo{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	spiloInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pgZooKeeper.spiloAdd,
		UpdateFunc: pgZooKeeper.spiloUpdate,
		DeleteFunc: pgZooKeeper.spiloDelete,
	})

	podInformer := cache.NewSharedIndexInformer(
		podsListWatch(clientset),
		&v1.Pod{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    pgZooKeeper.podAdd,
		UpdateFunc: pgZooKeeper.podUpdate,
		DeleteFunc: pgZooKeeper.podDelete,
	})

	pgZooKeeper.spiloInformer = spiloInformer
	pgZooKeeper.podInformer = podInformer

	cfg := etcdclient.Config{
		Endpoints:               []string{etcdHostOutside},
		Transport:               etcdclient.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	}

	c, err := etcdclient.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	pgZooKeeper.etcdApiClient = etcdclient.NewKeysAPI(c)
	pgZooKeeper.podEvents = make(chan podEvent)

	return pgZooKeeper
}

func (d *PgZooKeeper) podAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  pod.Namespace,
		name:       pod.Name,
		actionType: ACTION_ADD,
	}
}

func (d *PgZooKeeper) podDelete(obj interface{}) {
	pod := obj.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  pod.Namespace,
		name:       pod.Name,
		actionType: ACTION_DELETE,
	}
}

func (d *PgZooKeeper) podUpdate(old, cur interface{}) {
	oldPod := old.(*v1.Pod)
	d.podEvents <- podEvent{
		namespace:  oldPod.Namespace,
		name:       oldPod.Name,
		actionType: ACTION_UPDATE,
	}
}

func (z *PgZooKeeper) Run(stopCh <-chan struct{}, wg *sync.WaitGroup) {
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

func (z *PgZooKeeper) spiloAdd(obj interface{}) {
	spilo := obj.(*Spilo)

	clusterName := (*spilo).Metadata.Name
	ns := (*spilo).Metadata.Namespace

	z.CreateEndPoint(ns, clusterName)
	z.CreateService(ns, clusterName)
	z.CreateSecrets(ns, clusterName)
	z.CreateStatefulSet(spilo)
}

func (z *PgZooKeeper) spiloUpdate(old, cur interface{}) {
	oldSpilo := old.(*Spilo)
	curSpilo := cur.(*Spilo)

	if oldSpilo.Spec.NumberOfInstances != curSpilo.Spec.NumberOfInstances {
		z.UpdateStatefulSet(curSpilo)
	}

	if oldSpilo.Spec.DockerImage != curSpilo.Spec.DockerImage {
		z.UpdateStatefulSetImage(curSpilo)
	}

	log.Printf("Update spilo old: %+v cur: %+v", *oldSpilo, *curSpilo)
}

func (z *PgZooKeeper) spiloDelete(obj interface{}) {
	spilo := obj.(*Spilo)

	err := z.DeleteStatefulSet(spilo.Metadata.Namespace, spilo.Metadata.Name)
	if err != nil {
		log.Printf("Error while deleting stateful set: %+v", err)
	}
}

func (z *PgZooKeeper) DeleteStatefulSet(ns, clusterName string) error {
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

	z.DeleteEtcdKey(clusterName)

	return nil
}

func (z *PgZooKeeper) UpdateStatefulSet(spilo *Spilo) {
	ns := (*spilo).Metadata.Namespace

	statefulSet := z.createSetFromSpilo(spilo)
	_, err := z.Clientset.StatefulSets(ns).Update(&statefulSet)

	if err != nil {
		log.Printf("Error while updating StatefulSet: %s", err)
	}
}

func (z *PgZooKeeper) UpdateStatefulSetImage(spilo *Spilo) {
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

		//TODO: wait until Pod recreated
	}

	//TODO: do manual failover
	err = z.Clientset.Pods(ns).Delete(masterPodName, &deleteOptions)
	if err != nil {
		log.Printf("Error while deleting Pod %s.%s: %s", ns, masterPodName, err)
	} else {
		log.Printf("Pod deleted: %s.%s", ns, masterPodName)
	}
}

func (z *PgZooKeeper) podWatcher(stopCh <-chan struct{}) {
	watchers := make(map[string]chan podEvent)
	for {
		select {
		case watcher := <-z.podWatchers:
			if watcher.subscribe {
				watchers[watcher.podName] = watcher.eventsChannel
			} else {
				close(watcher.eventsChannel)
				delete(watchers, watcher.podName)
			}
		case podEvent := <-z.podEvents:
			podChannel, ok := watchers[podEvent.name]
			if ok == false {
				continue
			}

			podChannel <- podEvent
		}
	}
}
