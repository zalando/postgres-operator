# postgres operator prototype (WIP)

### Create minikube

    $ minikube start

### Deploy etcd

    $ kubectl create -f github.com/coreos/etcd/hack/kubernetes-deploy/etcd.yaml
    
### Run operator
    
    $ go run main.go
    
### Check if ThirdPartyResource has been registered

    $ kubectl get thirdpartyresources
    
    NAME                  DESCRIPTION                             VERSION(S)
    spilo.acid.zalan.do   A specification of Spilo StatefulSets   v1
    

### Create a new spilo cluster

    $ kubectl create -f testcluster.yaml
    
### Watch Pods being created

    $ kubectl get pods -w