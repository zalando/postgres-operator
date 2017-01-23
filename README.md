# postgres operator prototype (WIP)

### Create minikube

    $ minikube start

### Deploy etcd

    $ kubectl create -f https://raw.githubusercontent.com/coreos/etcd/master/hack/kubernetes-deploy/etcd.yml

##  Set your go path and put the sources so that go build finds them

    $ export GOPATH=~/git/go
    $ mkdir -p ${GOPATH}/src/github.bus.zalan.do/acid/
    $ cd ${GOPATH}/src/github.bus.zalan.do/acid/ && git clone https://github.bus.zalan.do/acid/postgres-operator -b prototype
    
### Install Glide on OS X

    $ brew install glide

### Install Glide on Ubuntu

    # sudo add-apt-repository ppa:masterminds/glide && sudo apt-get update
    # sudo apt-get install glide

### Install dependencies with Glide

   $ glide update && glide install

### Run operator (outside kubernetes cluster)
    
    $ go run main.go
    
### Check if ThirdPartyResource has been registered

    $ kubectl get thirdpartyresources
    
    NAME                  DESCRIPTION                             VERSION(S)
    spilo.acid.zalan.do   A specification of Spilo StatefulSets   v1
    

### Create a new spilo cluster

    $ kubectl create -f testcluster.yaml
    
### Watch Pods being created

    $ kubectl get pods -w
