# Postgres Operator

[![Build Status](https://travis-ci.org/zalando-incubator/postgres-operator.svg?branch=master)](https://travis-ci.org/zalando-incubator/postgres-operator)
[![Coverage Status](https://coveralls.io/repos/github/zalando-incubator/postgres-operator/badge.svg)](https://coveralls.io/github/zalando-incubator/postgres-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/zalando-incubator/postgres-operator)](https://goreportcard.com/report/github.com/zalando-incubator/postgres-operator)

The Postgres operator manages PostgreSQL clusters on Kubernetes using the [operator pattern](https://coreos.com/blog/introducing-operators.html).
During the initial run it registers the [Custom Resource Definition (CRD)](https://kubernetes.io/docs/concepts/api-extension/custom-resources/#customresourcedefinitions) for Postgres.
The `postgresql` CRD is essentially the schema that describes the contents of the manifests for deploying individual
Postgres clusters using StatefulSets and [Patroni](https://github.com/zalando/patroni).

Once the operator is running, it performs the following actions:

* watches for new `postgresql` manifests and deploys new clusters
* watches for updates to existing manifests and changes corresponding properties of the running clusters
* watches for deletes of the existing manifests and deletes corresponding clusters
* acts on an update to the operator configuration itself and changes the running clusters when necessary
  (i.e. the Docker image changes for a minor release update)
* periodically checks running clusters against the manifests and syncs changes

Example: When a user creates a new custom object of type ``postgresql`` by submitting a new manifest with
``kubectl``, the operator fetches that object and creates the required Kubernetes entities to spawn a new Postgres cluster
(StatefulSets, Services, Secrets).

Update example: After changing the Docker image inside the operator's configuration, the operator first goes to all StatefulSets
it manages and updates them with the new Docker image; afterwards, all pods from each StatefulSet are killed one by one
and the replacements are spawned automatically by each StatefulSet with the new Docker image. This is called the Rolling update.


## Quickstart 

Prerequisites: [minikube](https://github.com/kubernetes/minikube/releases) and [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/#install-kubectl-binary-via-curl)

### Local execution

```bash
git clone https://github.com/zalando-incubator/postgres-operator.git
cd postgres-operator

minikube start

# start the operator; may take a few seconds
kubectl create -f manifests/configmap.yaml         # operator config
kubectl create -f manifests/operator-rbac.yaml     # operator identity and persmissions
kubectl create -f manifests/postgres-operator.yaml # operator deployment

# submit a Postgres cluster
kubectl create -f manifests/minimal-postgres-manifest.yaml 

# tear down cleanly
minikube delete
```

We have automated these steps for you:
```bash
cd postgres-operator
./run_operator_locally.sh
```

## Scope

The scope of the postgres operator is on provisioning, modifying configuration and cleaning up Postgres clusters that use Patroni, basically to make it easy and convenient to run Patroni based clusters on Kubernetes.
The provisioning and modifying includes Kubernetes resources on one side but also e.g. database and role provisioning once the cluster is up and running.
We try to leave as much work as possible to Kubernetes and to Patroni where it fits, especially the cluster bootstrap and high availability.
The operator is however involved in some overarching orchestration, like rolling updates to improve the user experience.

Monitoring of clusters is not in scope, for this good tools already exist from ZMON to Prometheus and more Postgres specific options.

## Status

This project is currently in active development. It is however already [used internally by Zalando](https://jobs.zalando.com/tech/blog/postgresql-in-a-time-of-kubernetes/) in order to run Postgres clusters on Kubernetes in larger numbers for staging environments and a growing number of production clusters. In this environment the operator is deployed to multiple Kubernetes clusters, where users deploy manifests via our CI/CD infrastructure or rely on a slim user interface to create manifests.

Please, report any issues discovered to https://github.com/zalando-incubator/postgres-operator/issues.

## Talks

1. "Blue elephant on-demand: Postgres + Kubernetes" talk by Oleksii Kliukin and Jan Mussler, FOSDEM 2018: [video](https://fosdem.org/2018/schedule/event/blue_elephant_on_demand_postgres_kubernetes/) | [slides (pdf)](https://www.postgresql.eu/events/fosdem2018/sessions/session/1735/slides/59/FOSDEM%202018_%20Blue_Elephant_On_Demand.pdf)

2. "Kube-Native Postgres" talk by Josh Berkus, KubeCon 2017: [video](https://www.youtube.com/watch?v=Zn1vd7sQ_bc)

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

### Select the namespace to deploy to

The operator can run in a namespace other than `default`. For example, to use the `test` namespace, run the following before deploying the operator's manifests:

    kubectl create namespace test
    kubectl config set-context minikube --namespace=test

All subsequent `kubectl` commands will work with the `test` namespace. The operator  will run in this namespace and look up needed resources - such as its config map - there.

### Specify the namespace to watch

Watching a namespace for an operator means tracking requests to change Postgresql clusters in the namespace such as "increase the number of Postgresql replicas to 5" and reacting to the requests, in this example by actually scaling up.

By default, the operator watches the namespace it is deployed to. You can change this by altering the `WATCHED_NAMESPACE` env var in the operator deployment manifest or the `watched_namespace` field in the operator configmap. In the case both are set, the env var takes the precedence. To make the operator listen to all namespaces, explicitly set the field/env var to "`*`".

Note that for an operator to manage pods in the watched namespace, the operator's service account (as specified in the operator deployment manifest) has to have appropriate privileges to access the watched namespace. The operator may not be able to function in the case it watches all namespaces but lacks access rights to any of them (except Kubernetes system namespaces like `kube-system`). The reason is that for multiple namespaces operations such as 'list pods' execute at the cluster scope and fail at the first violation of access rights.

The watched namespace also needs to have a (possibly different) service account in the case database pods need to talk to the Kubernetes API (e.g. when using Kubernetes-native configuration of Patroni). The operator checks that the `pod_service_account_name` exists in the target namespace, and, if not, deploys there the `pod_service_account_definition` from the operator [`Config`](pkg/util/config/config.go) with the default value of:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
 name: operator
```

 In this definition, the operator overwrites the account's name to match `pod_service_account_name` and the `default` namespace to match the target namespace. The operator  performs **no** further syncing of this account.

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

### Role-based access control for the operator

The `manifests/operator-rbac.yaml` defines cluster roles and bindings needed for the operator to function under access control restrictions. To deploy the operator with this RBAC policy use:

```bash
kubectl create -f manifests/configmap.yaml
kubectl create -f manifests/operator-rbac.yaml
kubectl create -f manifests/postgres-operator.yaml
kubectl create -f manifests/minimal-postgres-manifest.yaml
```

Note that the service account in `operator-rbac.yaml` is named `zalando-postgres-operator` and not
the `operator` default that is created in the `serviceaccount.yaml`. So you will have to change the `service_account_name` in the operator configmap and `serviceAccountName` in the postgres-operator deployment appropriately.

This is done intentionally, as to avoid breaking those setups that
already work with the default `operator` account. In the future the operator should ideally be run under the
`zalando-postgres-operator` service account.

The service account defined in  `operator-rbac.yaml` acquires some privileges not really
used by the operator (i.e. we only need list and watch on configmaps),
this is also done intentionally to avoid breaking things if someone
decides to configure the same service account in the operator's
configmap to run postgres clusters.

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

### Using the operator to minimize the amount of failovers during the cluster upgrade

Postgres operator moves master pods out of to be decommissioned Kubernetes nodes. The decommission status of the node is derived
from the presence of the set of labels defined by the `node_readiness_label` parameter. The operator makes sure that the Postgres
master pods are moved elsewhere from the node that is pending to be decommissioned , but not on another node that is also
about to be shut down. It achieves that via a combination of several properties set on the postgres pods:

* [nodeAffinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#node-affinity-beta-feature) is configured to avoid scheduling the pod on nodes without all labels from the `node_readiness_label` set.
* [PodDisruptionBudget](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#how-disruption-budgets-work) is defined to keep the master pods running until they are moved out by the operator.

The operator starts moving master pods when the node is drained and doesn't have all labels from the `node_readiness_label` set.
By default this parameter is set to an empty string, disabling this feature altogether. It can be set to a string containing one
or more key:value parameters, i.e:
```
node_readiness_label: "lifecycle-status:ready,disagnostic-checks:ok"

```

when multiple labels are set the operator will require all of them to be present on a node (and set to the specified value) in order to consider
it ready.

#### Custom Pod Environment Variables

It is possible to configure a config map which is used by the Postgres pods as an additional provider for environment variables.

One use case is to customize the Spilo image and configure it with environment variables. The config map with the additional settings is configured in the operator's main config map:

**postgres-operator ConfigMap**

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: postgres-operator
data:
  # referencing config map with custom settings
  pod_environment_configmap: postgres-pod-config
  ...
```

**referenced ConfigMap `postgres-pod-config`**

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: postgres-pod-config
  namespace: default
data:
  MY_CUSTOM_VAR: value
```

This ConfigMap is then added as a source of environment variables to the Postgres StatefulSet/pods.

:exclamation: Note that there are environment variables defined by the operator itself in order to pass parameters to the Spilo image. The values from the operator for those variables will take precedence over those defined in the `pod_environment_configmap`.

### Limiting the number of instances in clusters with `min_instances` and `max_instances`

As a preventive measure, one can restrict the minimum and the maximum number of instances permitted by each Postgres cluster managed by the operator.
If either `min_instances` or `max_instances` is set to a non-zero value, the operator may adjust the number of instances specified in the cluster manifest to match either the min or the max boundary.
For instance, of a cluster manifest has 1 instance and the min_instances is set to 3, the cluster will be created with 3 instances. By default, both parameters are set to -1.

### Load balancers

For any Postgresql/Spilo cluster an operator creates two separate k8s services: one for the master pod and one for replica pods. To expose these services to an outer network, one can attach load balancers to them by setting `enableMasterLoadBalancer` and/or `enableReplicaLoadBalancer` to `true` in the cluster manifest. In the case any of these variables is omitted from the manifest, the operator configmap's settings `enable_master_load_balancer` and `enable_replica_load_balancer` apply. Note that the operator settings affect all Postgresql services running in a namespace watched by the operator.

For backward compatibility with already configured clusters we maintain in a cluster manifest older parameter names, namely `useLoadBalancer` for enabling the master service's load balancer and `replicaLoadBalancer` for the replica service. If set, these params take precedence over the newer `enableMasterLoadBalancer` and `enableReplicaLoadBalancer`. Note that in older versions of the operator (before PR #258) `replicaLoadBalancer` was responsible for both creating the replica service and attaching an LB to it; now the service is always created (since k8s service typically is free in the cloud setting), and this param only attaches an LB (that typically costs money).

For the same reason of compatibility, we maintain the `enable_load_balancer` setting in the operator config map that was previously used to attach a LB to the master service. Its value is examined after the deprecated `useLoadBalancer` setting from the Postgresql manifest but before the recommended `enableMasterLoadBalancer`. There is no equivalent option for the replica service since the service used to be always created with a load balancer.

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
* protected_role_names - a list of role names that should be forbidden as the manifest, infrastructure and teams API roles.
The default value is `admin`. Operator will also disallow superuser and replication roles to be redefined.


### Defining database roles in the operator

Postgres operator allows defining roles to be created in the resulting database cluster. It covers three use-cases:

* create application roles specific to the cluster described in the manifest: `manifest roles`.
* create application roles that should be automatically created on every cluster managed by the operator: `infrastructure roles`.
* automatically create users for every member of the team owning the database cluster: `teams API roles`.

In the next sections, we will cover those use cases in more details.

#### Manifest roles

Manifest roles are defined directly in the cluster manifest. See [minimal postgres manifest](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/minimal-postgres-manifest.yaml) for an example of `zalando` role, defined with `superuser` and `createdb` flags.

Manifest roles are defined as a dictionary, with a role name as a key and a list of role options as a value. For a role without any options supply an empty list.

The operator accepts the following options:  `superuser`, `inherit`, `login`, `nologin`, `createrole`, `createdb`, `replication`, `bypassrls`.

By default, manifest roles are login roles (aka users), unless `nologin` is specified explicitly.

The operator automatically generates a password for each manifest role and places it in the secret named
`{username}.{team}-{clustername}.credentials.postgresql.acid.zalan.do` in the same namespace as the cluster.
This way, the application running in the Kubernetes cluster and working with the database can obtain the password right from the secret, without ever sharing it outside of the cluster.  

At the moment it is not possible to define membership of the manifest role in other roles.

#### Infrastructure roles

An infrastructure role is a role that should be present on every PostgreSQL cluster managed by the operator. An example of such a role is a monitoring user. There are two ways to define them:

* Exclusively via the infrastructure roles secret (specified by the `infrastructure_roles_secret_name` parameter).

The role definition looks like this (values are base64 encoded):


    user1: ZGJ1c2Vy
    password1: c2VjcmV0
    inrole1: b3BlcmF0b3I=

A block above describes the infrastructure role 'dbuser' with the password 'secret' that is the member of the 'operator' role.
For the following definitions one must increase the index, i.e. the next role will be defined as 'user2' and so on. Note that there is no way to specify role options (like superuser or nologin) this way, and the resulting role will automatically be a login role.

*  Via both the infrastructure roles secret and the infrastructure role configmap (with the same name as the infrastructure roles secret).

The infrastructure roles secret should contain an entry with 'rolename: rolepassword' for each role, and the role description should be specified in the configmap. Below is the example:


    dbuser: c2VjcmV0

and the configmap definition for that user:

    data:
      dbuser: |
        inrole: [operator, admin]  # following roles will be assigned to the new user
        user_flags:
          - createdb
        db_parameters:  # db parameters, applied for this particular user
          log_statement: all

Note that the definition above allows for more details than the one that relies solely on the infrastructure role secret.
In particular, one can allow membership in multiple roles via the `inrole` array parameter, define role flags via the `user_flags` list
and supply per-role options through the `db_parameters` dictionary. All those parameters are optional.

The definitions that solely use the infrastructure roles secret are more limited and considered legacy ones; one should use the new style that specifies infrastructure roles using both the secret and the configmap. You can mix both in the infrastructure role secret, as long as your new-style definition can be clearly distinguished from the old-style one (for instance, do not name new-style roles`userN`).

Since an infrastructure role is created uniformly on all clusters managed by the operator, it makes no sense to define it without the password. Such definitions will be ignored with a prior warning.

See [infrastructure roles secret](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/infrastructure-roles.yaml)
and [infrastructure roles configmap](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/infrastructure-roles-configmap.yaml) for the examples.

#### Teams API roles

Teams API roles cover the task of creating human users on the cluster. The operator calls a special Teams API endpoint (configured via the `teams_api_url` parameter) to get the list of human users for the particular cluster. It provides the team id (configured via the `teamId` parameter on the cluster itself) to the teams API.

There is a demo implementation of the teams API server at [fake teams api project](https://github.com/ikitiki/fake-teams-api).
The operator expects an OAuth2 authentication for the teams API endpoint. To fetch the OAuth2 token, it reads the secret with the name specified by the `oauth_token_secret_name` operator configuration. That secret should contain two fields:
`read-only-token-type` equal to `Bearer` and `read-only-token-secret`, containing the actual token. It is the task of some external service to rotate those tokens properly.

Once the operator gets the list of team members from the teams API, it creates them as members of the `pam_role_name` role (configured in the operator configuration).  The operator creates them as LOGIN roles and optionally assigns them superuser (if `enable_team_superuser` is set) and `team_admin_role` role (if it is set).

Note that the operator does not create any password for those roles, as those are supposed to authenticate against the OAuth2 endpoint using the [pam-oauth](https://github.com/CyberDem0n/pam-oauth2) module that is the part of [Spilo](https://github.com/zalando/spilo). The operator passes the URL specified in the `pam_configuration` parameter to Spilo, which configures the `pg_hba.conf` authentication for `pam_role_name` group to pass the token provided by the user (as the password) to that URL, together with the username.

The pre-requisite to this is an OAuth2 service that generates tokens for users and provides an URL for authenticating them. Once this infrastructure is in place, it will, combined with `pam_oauth`, give human users strong auto-expiring passwords.

For small installations, the teams API can be disabled by setting `enable_teams_api` to `false` in the operator configuration; then it is the task of the cluster admin to manage human users manually.

#### Role priorities

When there is a naming conflict between roles coming from different origins (i.e. an infrastructure role defined with the same name as the manifest role), the operator will choose the one with the highest priority origin.

System roles (configured with `super_username` and `replication_username` in the operator) have the highest priority; next are team API roles, infrastructure roles and manifest roles.

There is a mechanism that prevents overriding critical roles: it is not possible to override system roles (the operator will give an error even before applying priority rules);  the same applies to the roles mentioned in the `protected_role_names` list in the operator configuration.

### Debugging the operator itself

There is a web interface in the operator to observe its internal state. The operator listens on port 8080. It is possible to expose it to the localhost:8080 by doing:

    $ kubectl --context minikube port-forward $(kubectl --context minikube get pod -l name=postgres-operator -o jsonpath={.items..metadata.name}) 8080:8080

The inner 'query' gets the name of the postgres operator pod, and the outer enables port forwarding. Afterwards, you can access the operator API with:

    $ curl http://127.0.0.1:8080/$endpoint| jq .

The available endpoints are listed below. Note that the worker ID is an integer from 0 up to 'workers' - 1 (value configured in the operator configuration and defaults to 4)

* /databases - all databases per cluster
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

### Unit tests

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
