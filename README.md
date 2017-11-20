# Postgres Operator

[![Build Status](https://travis-ci.org/zalando-incubator/postgres-operator.svg?branch=master)](https://travis-ci.org/zalando-incubator/postgres-operator)
[![Coverage Status](https://coveralls.io/repos/github/zalando-incubator/postgres-operator/badge.svg)](https://coveralls.io/github/zalando-incubator/postgres-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/zalando-incubator/postgres-operator)](https://goreportcard.com/report/github.com/zalando-incubator/postgres-operator)

The Postgres operator manages PostgreSQL clusters on Kubernetes using the [operator pattern](https://coreos.com/blog/introducing-operators.html).
During the initial run it registers the [Custom Resource Definition (CRD)](https://kubernetes.io/docs/concepts/api-extension/custom-resources/#customresourcedefinitions) for Postgres.
The PostgreSQL CRD is essentially the schema that describes the contents of the manifests for deploying individual 
PostgreSQL clusters using StatefulSets and [Patroni](https://github.com/zalando/patroni).

Once the operator is running, it performs the following actions:

* watches for new PostgreSQL cluster manifests and deploys corresponding clusters
* watches for updates to existing manifests and changes corresponding properties of the running clusters
* watches for deletes of the existing manifests and deletes corresponding clusters
* acts on an update to the operator definition itself and changes the running clusters when necessary 
  (i.e. when the docker image inside the operator definition has been updated)
* periodically checks running clusters against the manifests and acts on the differences found

For instance, when the user creates a new custom object of type ``postgresql`` by submitting a new manifest with 
``kubectl``, the operator fetches that object and creates the corresponding Kubernetes structures 
(StatefulSets, Services, Secrets) according to its definition.

Another example is changing the docker image inside the operator. In this case, the operator first goes to all StatefulSets
it manages and updates them with the new docker images; afterwards, all pods from each StatefulSet are killed one by one
(rolling upgrade) and the replacements are spawned automatically by each StatefulSet with the new docker image.

## Status

This project is currently in development. It is used internally by Zalando in order to run staging databases on Kubernetes.
Please, report any issues discovered to https://github.com/zalando-incubator/postgres-operator/issues.

## Running and testing the operator

The best way to test the operator is to run it in [minikube](https://kubernetes.io/docs/getting-started-guides/minikube/). 
Minikube is a tool to run Kubernetes cluster locally.

### Installing and starting minikube

See [minikube installation guide](https://github.com/kubernetes/minikube/releases)

Make sure you use the latest version of Minikube.

After the installation, issue

    $ minikube start

Note: if you are running on a Mac, make sure to use the [xhyve driver](https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#xhyve-driver)
instead of the default docker-machine one for performance reasons.

Once you have it started successfully, use [the quickstart guide](https://github.com/kubernetes/minikube#quickstart) in order
to test your that your setup is working.

Note: if you use multiple Kubernetes clusters, you can switch to Minikube with `kubectl config use-context minikube`

### Create ConfigMap

ConfigMap is used to store the configuration of the operator

    $ kubectl --context minikube  create -f manifests/configmap.yaml

### Deploying the operator

First you need to install the service account definition in your Minikube cluster.

    $ kubectl --context minikube create -f manifests/serviceaccount.yaml

Next deploy the postgres-operator from the docker image Zalando is using:

    $ kubectl --context minikube create -f manifests/postgres-operator.yaml

If you prefer to build the image yourself follow up down below.

### Check if CustomResourceDefinition has been registered

    $ kubectl --context minikube   get crd

	NAME                          KIND
	postgresqls.acid.zalan.do     CustomResourceDefinition.v1beta1.apiextensions.k8s.io


### Create a new Spilo cluster

    $ kubectl --context minikube  create -f manifests/minimal-postgres-manifest.yaml

### Watch pods being created

    $ kubectl --context minikube  get pods -w --show-labels

### Connect to PostgreSQL

We can use the generated secret of the `postgres` robot user to connect to our `acid-minimal-cluster` master running in Minikube:

    $ export HOST_PORT=$(minikube service acid-minimal-cluster --url | sed 's,.*/,,')
    $ export PGHOST=$(echo $HOST_PORT | cut -d: -f 1)
    $ export PGPORT=$(echo $HOST_PORT | cut -d: -f 2)
    $ export PGPASSWORD=$(kubectl --context minikube get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
    $ psql -U postgres


### Configuration Options

The operator can be configured with the provided ConfigMap (`manifests/configmap.yaml`).

#### Use taints and tolerations for dedicated PostgreSQL nodes

To ensure Postgres pods are running on nodes without any other application pods, you can use 
[taints and tolerations](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/) and configure the 
required toleration in the operator ConfigMap.

As an example you can set following node taint:

```
$ kubectl taint nodes <nodeName> postgres=:NoSchedule
```

And configure the toleration for the PostgreSQL pods by adding following line to the ConfigMap:

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: postgres-operator
data:
  toleration: "key:postgres,operator:Exists,effect:NoSchedule"
  ...
```

Or you can specify and/or overwrite the tolerations for each PostgreSQL instance in the manifest:

```
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
spec:
  teamId: "ACID"
  tolerations:
  - key: postgres
    operator: Exists
    effect: NoSchedule
```

Please be aware that the taint and toleration only ensures that no other pod gets scheduled to a PostgreSQL node 
but not that PostgreSQL pods are placed on such a node. This can be achieved by setting a node affinity rule in the ConfigMap.


# Setup development environment

The following steps guide you through the setup to work on the operator itself.

## Setting up Go

Postgres operator is written in Go. Use the [installation instructions](https://golang.org/doc/install#install) if you don't have Go on your system.
You won't be able to compile the operator with Go older than 1.7. We recommend installing [the latest one](https://golang.org/dl/).

Go projects expect their source code and all the dependencies to be located under the [GOPATH](https://github.com/golang/go/wiki/GOPATH).
Normally, one would create a directory for the GOPATH (i.e. ~/go) and place the source code under the ~/go/src subdirectories.

Given the schema above, the postgres operator source code located at `github.com/zalando-incubator/postgres-operator` should be put at
-`~/go/src/github.com/zalando-incubator/postgres-operator`.

    $ export GOPATH=~/go
    $ mkdir -p ${GOPATH}/src/github.com/zalando-incubator/
    $ cd ${GOPATH}/src/github.com/zalando-incubator/ && git clone https://github.com/zalando-incubator/postgres-operator.git


## Building the operator

You need Glide to fetch all dependencies. Install it with:

    $ make tools

Next, install dependencies with glide by issuing:

    $ make deps

This would take a while to complete. You have to redo `make deps` every time you dependencies list changes, i.e. after adding a new library dependency.

Build the operator docker image and pushing it to Pier One:

    $ make docker push

You may define the TAG variable to assign an explicit tag to your docker image and the IMAGE to set the image name.
By default, the tag is computed with `git describe --tags --always --dirty` and the image is `pierone.stups.zalan.do/acid/postgres-operator`

Building the operator binary (for testing the out-of-cluster option):

    $ make

The binary will be placed into the build directory.

### Deploying self build image

The fastest way to run your docker image locally is to reuse the docker from minikube.
The following steps will get you the docker image built and deployed.

    $ eval $(minikube docker-env)
    $ export TAG=$(git describe --tags --always --dirty)
    $ make docker
    $ sed -e "s/\(image\:.*\:\).*$/\1$TAG/" manifests/postgres-operator.yaml|kubectl --context minikube create  -f -


### Operator Configuration Parameters

* team_api_role_configuration - a map represented as *"key1:value1,key2:value2"*
of configuration parameters applied to the roles fetched from the API.
For instance, `team_api_role_configuration: log_statement:all,search_path:'public,"$user"'`.
By default is set to *"log_statement:all"*. See [PostgreSQL documentation on ALTER ROLE .. SET](https://www.postgresql.org/docs/current/static/sql-alterrole.html) for to learn about the available options.


### Debugging the operator itself

There is a web interface in the operator to observe its internal state. The operator listens on port 8080. It is possible to expose it to the localhost:8080 by doing:

    $ kubectl --context minikube port-forward $(kubectl --context minikube get pod -l name=postgres-operator -o jsonpath={.items..metadata.name}) 8080:8080

The inner 'query' gets the name of the postgres operator pod, and the outer enables port forwarding. Afterwards, you can access the operator API with:

    $ curl http://127.0.0.1:8080/$endpoint| jq .

The available endpoints are listed below. Note that the worker ID is an integer from 0 up to 'workers' - 1 (value configured in the operator configuration and defaults to 4)

* /workers/all/queue - state of the workers queue (cluster events to process)
* /workers/$id/queue - state of the queue for the worker $id
* /workers/$id/logs - log of the operations performed by a given worker
* /clusters/ - list of teams and clusters known to the operator
* /clusters/$team - list of clusters for the given team
* /cluster/$team/$clustername - detailed status of the cluster, including the specifications for CRD, master and replica services, endpoints and statefulsets, as well as any errors and the worker that cluster is assigned to.
* /cluster/$team/$clustername/logs/ - logs of all operations performed to the cluster so far.
* /cluster/$team/$clustername/history/ - history of cluster changes triggered by the changes of the manifest (shows the somewhat obscure diff and what exactly has triggered the change)

The operator also supports pprof endpoints listed at the [pprof package](https://golang.org/pkg/net/http/pprof/), such as:

* /debug/pprof/
* /debug/pprof/cmdline
* /debug/pprof/profile
* /debug/pprof/symbol
* /debug/pprof/trace
