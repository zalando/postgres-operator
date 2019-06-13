## Intro

See [minikube installation guide](https://github.com/kubernetes/minikube/releases)

Make sure you use the latest version of Minikube.

After the installation, issue

```bash
    $ minikube start
```

Note: if you are running on a Mac, you may also use Docker for Mac Kubernetes
instead of a docker-machine.

Once you have it started successfully, use [the quickstart
guide](https://github.com/kubernetes/minikube#quickstart) in order to test your
that your setup is working.

Note: if you use multiple Kubernetes clusters, you can switch to Minikube with
`kubectl config use-context minikube`

## Deploying the operator

### Kubernetes manifest

A ConfigMap is used to store the configuration of the operator. Alternatively,
a CRD-based configuration can be used, as described [here](reference/operator_parameters).

```bash
    $ kubectl --context minikube  create -f manifests/configmap.yaml
```

First you need to install the service account definition in your Minikube cluster.

```bash
    $ kubectl --context minikube create -f manifests/operator-service-account-rbac.yaml
```

Next deploy the postgres-operator from the docker image Zalando is using:

```bash
    $ kubectl --context minikube create -f manifests/postgres-operator.yaml
```

If you prefer to build the image yourself follow up down below.

### Helm chart

Alternatively, the operator can be installed by using the provided [Helm](https://helm.sh/)
chart which saves you the manual steps. Therefore, you would need to install
the helm CLI on your machine. After initializing helm (and its server
component Tiller) in your local cluster you can install the operator chart.
You can define a release name that is prepended to the operator resource's
names.

Use `--name zalando` to match with the default service account name as older
operator versions do not support custom names for service accounts. When relying
solely on the CRD-based configuration edit the `serviceAccount` section in the
[values yaml file](../charts/values.yaml) by setting the name to `"operator"`.

```bash
    $ helm init
    $ helm install --name zalando ./charts/postgres-operator
```

## Check if CustomResourceDefinition has been registered

```bash
    $ kubectl --context minikube   get crd

	NAME                          KIND
	postgresqls.acid.zalan.do     CustomResourceDefinition.v1beta1.apiextensions.k8s.io
```

## Create a new Spilo cluster

```bash
    $ kubectl --context minikube  create -f manifests/minimal-postgres-manifest.yaml
```

## Watch pods being created

```bash
    $ kubectl --context minikube  get pods -w --show-labels
```

## Connect to PostgreSQL

We can use the generated secret of the `postgres` robot user to connect to our
`acid-minimal-cluster` master running in Minikube:

```bash
    $ export HOST_PORT=$(minikube service acid-minimal-cluster --url | sed 's,.*/,,')
    $ export PGHOST=$(echo $HOST_PORT | cut -d: -f 1)
    $ export PGPORT=$(echo $HOST_PORT | cut -d: -f 2)
    $ export PGPASSWORD=$(kubectl --context minikube get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
    $ psql -U postgres
```

# Setup development environment

The following steps guide you through the setup to work on the operator itself.

## Setting up Go

Postgres operator is written in Go. Use the [installation
instructions](https://golang.org/doc/install#install) if you don't have Go on
your system. You won't be able to compile the operator with Go older than 1.7.
We recommend installing [the latest one](https://golang.org/dl/).

Go projects expect their source code and all the dependencies to be located
under the [GOPATH](https://github.com/golang/go/wiki/GOPATH). Normally, one
would create a directory for the GOPATH (i.e. ~/go) and place the source code
under the ~/go/src subdirectories.

Given the schema above, the postgres operator source code located at
`github.com/zalando/postgres-operator` should be put at
-`~/go/src/github.com/zalando/postgres-operator`.

```bash
    $ export GOPATH=~/go
    $ mkdir -p ${GOPATH}/src/github.com/zalando/
    $ cd ${GOPATH}/src/github.com/zalando/
    $ git clone https://github.com/zalando/postgres-operator.git
```

## Building the operator

You need Glide to fetch all dependencies. Install it with:

```bash
    $ make tools
```

Next, install dependencies with glide by issuing:

```bash
    $ make deps
```

This would take a while to complete. You have to redo `make deps` every time
you dependencies list changes, i.e. after adding a new library dependency.

Build the operator docker image and pushing it to Pier One:

```bash
    $ make docker push
```

You may define the TAG variable to assign an explicit tag to your docker image
and the IMAGE to set the image name. By default, the tag is computed with
`git describe --tags --always --dirty` and the image is
`pierone.stups.zalan.do/acid/postgres-operator`

Building the operator binary (for testing the out-of-cluster option):

```bash
    $ make
```

The binary will be placed into the build directory.

## Deploying self build image

The fastest way to run your docker image locally is to reuse the docker from
minikube. The following steps will get you the docker image built and deployed.

```bash
    $ eval $(minikube docker-env)
    $ export TAG=$(git describe --tags --always --dirty)
    $ make docker
    $ sed -e "s/\(image\:.*\:\).*$/\1$TAG/" manifests/postgres-operator.yaml|kubectl --context minikube create  -f -
```

# Code generation

The operator employs k8s-provided code generation to obtain deep copy methods
and Kubernetes-like APIs for its custom resource definitons, namely the Postgres
CRD and the operator CRD. The usage of the code generation follows conventions
from the k8s community. Relevant scripts live in the `hack` directory:
* `update-codegen.sh` triggers code generation for the APIs defined in `pkg/apis/acid.zalan.do/`,
* `verify-codegen.sh` checks if the generated code is up-to-date (to be used within CI).

The `/pkg/generated/` contains the resultant code. To make these scripts work,
you may need to `export GOPATH=$(go env GOPATH)`

References for code generation are:
* [Relevant pull request](https://github.com/zalando/postgres-operator/pull/369)
See comments there for minor issues that can sometimes broke the generation process.
* [Code generator source code](https://github.com/kubernetes/code-generator)
* [Code Generation for CustomResources](https://blog.openshift.com/kubernetes-deep-dive-code-generation-customresources/) - intro post on the topic
* Code generation in [Prometheus](https://github.com/coreos/prometheus-operator) and [etcd](https://github.com/coreos/etcd-operator) operators

To debug the generated API locally, use the
[kubectl proxy](https://kubernetes.io/docs/tasks/access-kubernetes-api/http-proxy-access-api/)
and `kubectl --v=8` log level to display contents of HTTP requests (run the
operator itself with `--v=8` to log all REST API requests). To attach a debugger
to the operator, use the `-outofcluster` option to run the operator locally on
the developer's laptop (and not in a docker container).

# Debugging the operator

There is a web interface in the operator to observe its internal state. The
operator listens on port 8080. It is possible to expose it to the
localhost:8080 by doing:

    $ kubectl --context minikube port-forward $(kubectl --context minikube get pod -l name=postgres-operator -o jsonpath={.items..metadata.name}) 8080:8080

The inner 'query' gets the name of the postgres operator pod, and the outer
enables port forwarding. Afterwards, you can access the operator API with:

    $ curl --location http://127.0.0.1:8080/$endpoint | jq .

The available endpoints are listed below. Note that the worker ID is an integer
from 0 up to 'workers' - 1 (value configured in the operator configuration and
defaults to 4)

* /databases - all databases per cluster
* /workers/all/queue - state of the workers queue (cluster events to process)
* /workers/$id/queue - state of the queue for the worker $id
* /workers/$id/logs - log of the operations performed by a given worker
* /clusters/ - list of teams and clusters known to the operator
* /clusters/$team - list of clusters for the given team
* /clusters/$team/$namespace/$clustername - detailed status of the cluster,
  including the specifications for CRD, master and replica services, endpoints
  and statefulsets, as well as any errors and the worker that cluster is
  assigned to.
* /clusters/$team/$namespace/$clustername/logs/ - logs of all operations
  performed to the cluster so far.
* /clusters/$team/$namespace/$clustername/history/ - history of cluster changes
  triggered by the changes of the manifest (shows the somewhat obscure diff and
  what exactly has triggered the change)

The operator also supports pprof endpoints listed at the
[pprof package](https://golang.org/pkg/net/http/pprof/), such as:

* /debug/pprof/
* /debug/pprof/cmdline
* /debug/pprof/profile
* /debug/pprof/symbol
* /debug/pprof/trace

It's possible to attach a debugger to troubleshoot postgres-operator inside a
docker container. It's possible with gdb and
[delve](https://github.com/derekparker/delve). Since the latter one is a
specialized debugger for golang, we will use it as an example. To use it you
need:

* Install delve locally

```
go get -u github.com/derekparker/delve/cmd/dlv
```

* Add following dependencies to the `Dockerfile`

```
RUN apk --no-cache add go git musl-dev
RUN go get github.com/derekparker/delve/cmd/dlv
```

* Update the `Makefile` to build the project with debugging symbols. For that
  you need to add `gcflags` to a build target for corresponding OS (e.g. linux)

```
-gcflags "-N -l"
```

* Run `postgres-operator` under the delve. For that you need to replace
  `ENTRYPOINT` with the following `CMD`:

```
CMD ["/root/go/bin/dlv", "--listen=:DLV_PORT", "--headless=true", "--api-version=2", "exec", "/postgres-operator"]
```

* Forward the listening port

```
kubectl port-forward POD_NAME DLV_PORT:DLV_PORT
```

* Attach to it

```
$ dlv connect 127.0.0.1:DLV_PORT
```

## Unit tests

To run all unit tests, you can simply do:

```
$ go test ./...
```

For go 1.9 `vendor` directory would be excluded automatically. For previous
versions you can exclude it manually:

```
$ go test $(glide novendor)
```

In case if you need to debug your unit test, it's possible to use delve:

```
$ dlv test ./pkg/util/retryutil/
Type 'help' for list of commands.
(dlv) c
PASS
```

To test the multinamespace setup, you can use

```
./run_operator_locally.sh --rebuild-operator
```
It will automatically create an `acid-minimal-cluster` in the namespace `test`.
Then you can for example check the Patroni logs:

```
kubectl logs acid-minimal-cluster-0
```

## End-to-end tests

The operator provides reference e2e (end-to-end) tests to ensure various infra parts work smoothly together.
Each e2e execution tests a Postgres operator image built from the current git branch. The test runner starts a [kind](https://kind.sigs.k8s.io/) (local k8s) cluster and Docker container with tests. The k8s API client from within the container connects to the `kind` cluster using the standard Docker `bridge` network.
The tests utilize examples from `/manifests` (ConfigMap is used for the operator configuration) to avoid maintaining yet another set of configuration files. The kind cluster is deleted if tests complete successfully.

End-to-end tests are executed automatically during builds; to invoke them locally use `make e2e-run` from the project's top directory. Run `make e2e-tools e2e-build` to install `kind` and build the tests' image locally before the first run. 

End-to-end tests are written in Python and use `flake8` for code quality. Please run flake8 [before submitting a PR](http://flake8.pycqa.org/en/latest/user/using-hooks.html).

## Introduce additional configuration parameters

In the case you want to add functionality to the operator that shall be
controlled via the operator configuration there are a few places that need to
be updated. As explained [here](reference/operator_parameters.md), it's possible
to configure the operator either with a ConfigMap or CRD, but currently we aim
to synchronize parameters everywhere.

When choosing a parameter name for a new option in a PG manifest, keep in mind
the naming conventions there. The `snake_case` variables come from the Patroni/Postgres world, while the `camelCase` from the k8s world.

Note: If one option is defined in the operator configuration and in the cluster
[manifest](../manifests/complete-postgres-manifest.yaml), the latter takes
precedence.

So, first define the parameters in:
* the [ConfigMap](../manifests/configmap.yaml) manifest
* the CR's [default configuration](../manifests/postgresql-operator-default-configuration.yaml)
* the Helm chart [values](../charts/postgres-operator/values.yaml)

Update the following Go files that obtain the configuration parameter from the
manifest files:
* [operator_configuration_type.go](../pkg/apis/acid.zalan.do/v1/operator_configuration_type.go)
* [operator_config.go](../pkg/controller/operator_config.go)
* [config.go](../pkg/util/config/config.go)

The operator behavior has to be implemented at least in [k8sres.go](../pkg/cluster/k8sres.go).
Please, reflect your changes in tests, for example in:
* [config_test.go](../pkg/util/config/config_test.go)
* [k8sres_test.go](../pkg/cluster/k8sres_test.go)
* [util_test.go](../pkg/apis/acid.zalan.do/v1/util_test.go)

Finally, document the new configuration option(s) for the operator in its
[reference](reference/operator_parameters.md) document and explain the feature
in the [administrator docs](administrator.md).
