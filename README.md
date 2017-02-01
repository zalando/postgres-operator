# postgres operator prototype (WIP)

### Create minikube

    $ minikube start

### Deploy etcd

    $ kubectl create -f https://raw.githubusercontent.com/coreos/etcd/master/hack/kubernetes-deploy/etcd.yml

##  Set your go path and put the sources so that go build finds them

    $ export GOPATH=~/git/go
    $ mkdir -p ${GOPATH}/src/github.bus.zalan.do/acid/
    $ cd ${GOPATH}/src/github.bus.zalan.do/acid/ && git clone https://github.bus.zalan.do/acid/postgres-operator -b prototype

### Install Glide and Staticcheck

    $ make tools 

### Install dependencies with Glide

    $ make deps

### Build dependencies

    $ go build -i -v cmd

## Run operator (as a pod)

    $ docker build -t postgres-operator:0.1 .
    $ kubectl create -f postgres-operator.yaml

    If you are building docker image by yourself on OS X make sure postgres-operator is compiled with GOOS=linux flag
    
### Check if ThirdPartyResource has been registered

    $ kubectl get thirdpartyresources
    
    NAME                  DESCRIPTION                             VERSION(S)
    spilo.acid.zalan.do   A specification of Spilo StatefulSets   v1
    

### Create a new spilo cluster

    $ kubectl create -f manifests/testspilo.yaml
    
### Watch Pods being created

    $ kubectl get pods -w
