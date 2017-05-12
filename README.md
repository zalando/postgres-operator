# postgres operator prototype (WIP)

Postgres operator manages Postgres clusters in Kubernetes using the [operator pattern](https://coreos.com/blog/introducing-operators.html)
During the initial run it registers the [Third Party Resource (TPR)](https://kubernetes.io/docs/user-guide/thirdpartyresources/) for Postgres.
The Postgres TPR is essentially the schema that describes the contents of the manifests for deploying individual Postgres clusters.

Once the operator is running, it performs the following actions:

* watches for new postgres cluster manifests and deploys corresponding clusters.
* watches for updates to existing manifests and changes corresponding properties of the running clusters.
* watches for deletes of the existing manifests and deletes corresponding database clusters.
* acts on an update to the operator definition itself and changes the running clusters when necessary (i.e. when the docker image inside the operator definition has been updated.)
* periodically checks running clusters against the manifests and acts on the differences found.

For instance, when the user creates a new custom object of type postgresql by submitting a new manifest with kubectl, the operator fetches that object and creates the corresponding kubernetes structures (StatefulSets, Services, Secrets) according to its definition.

Another example is changing the docker image inside the operator. In this case, the operator first goes to all Statefulsets
it manages and updates them with the new docker images; afterwards, all pods from each Statefulset are killed one by one
(rolling upgrade) and the replacements are spawned automatically by each Statefulset with the new docker image.

## Setting up Go.

Postgres operator is written in Go. Use the [installation instructions](https://golang.org/doc/install#install) if you don't have Go on your system.
You won't be able to compile the operator with Go older than 1.7. We recommend installing [the latest one](https://golang.org/dl/).

Go projects expect their source code and all the dependencies to be located under the [GOPATH](https://github.com/golang/go/wiki/GOPATH).
Normally, one would create a directory for the GOPATH (i.e. ~/go) and place the source code under the ~/go/src subdirectories.

Given the schema above, the postgres operator source code located at `github.com/zalando-incubator/postgres-operator` should be put at
`~/go/src/github.com/zalando-incubator/postgres-operator`.

    $ export GOPATH=~/go
    $ mkdir -p ${GOPATH}/src/github.bus.zalan.do/acid/
    $ cd ${GOPATH}/src/github.bus.zalan.do/acid/ && git clone git@github.bus.zalan.do:acid/postgres-operator.git


## Building the operator

You need Glide to fetch all dependencies. Install it with:

    $ make tools

Next, install dependencies with glide by issuing:

    $ make deps

This would take a while to complete. You have to redo `make deps` every time you dependencies list changes, i.e. after adding a new library dependency.

Build the operator docker image and pushing it to pierone:

    $ make docker push

You may define the TAG variable to assign an explicit tag to your docker image and the IMAGE to set the image name.
By default, the tag is computed with `git describe --tags --always --dirty` and the image is `pierone.example.com/acid/postgres-operator`

Building the operator binary (for testing the out-of-cluster option):

    $ make

The binary will be placed into the build directory.

## Testing the operator

The best way to test the operator is to run it in minikube. Minikube is a tool to run Kubernetes cluster locally.

### Installing and starting minikube

See [minikube installation guide](https://github.com/kubernetes/minikube/releases)

Make sure you use Minikube 1.7.
After the installation, issue the

    $ minikube start

Note: if you are running on a Mac, make sure to use the [xhyve driver](https://github.com/kubernetes/minikube/blob/master/DRIVERS.md#xhyve-driver)
instead of the default docker-machine one for performance reasons.

One you have it started successfully, use [the quickstart guide](https://github.com/kubernetes/minikube#quickstart) in order
to test your that your setup is working.

Note: if you use multiple kubernetes clusters, you can switch to minikube with `kubectl config use-context minikube`