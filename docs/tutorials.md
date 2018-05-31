# Tutorials

## Installing and starting minikube

### Intro

See [minikube installation guide](https://github.com/kubernetes/minikube/releases)

Make sure you use the latest version of Minikube.

After the installation, issue

```bash
    $ minikube start
```

Note: if you are running on a Mac, make sure to use the [xhyve
driver](https://github.com/kubernetes/minikube/blob/master/docs/drivers.md#xhyve-driver)
instead of the default docker-machine one for performance reasons.

Once you have it started successfully, use [the quickstart
guide](https://github.com/kubernetes/minikube#quickstart) in order to test your
that your setup is working.

Note: if you use multiple Kubernetes clusters, you can switch to Minikube with
`kubectl config use-context minikube`

### Create ConfigMap

ConfigMap is used to store the configuration of the operator

```bash
    $ kubectl --context minikube  create -f manifests/configmap.yaml
```

### Deploying the operator

First you need to install the service account definition in your Minikube cluster.

```bash
    $ kubectl --context minikube create -f manifests/operator-service-account-rbac.yaml
```

Next deploy the postgres-operator from the docker image Zalando is using:

```bash
    $ kubectl --context minikube create -f manifests/postgres-operator.yaml
```

If you prefer to build the image yourself follow up down below.

### Check if CustomResourceDefinition has been registered

```bash
    $ kubectl --context minikube   get crd

	NAME                          KIND
	postgresqls.acid.zalan.do     CustomResourceDefinition.v1beta1.apiextensions.k8s.io
```

### Create a new Spilo cluster

```bash
    $ kubectl --context minikube  create -f manifests/minimal-postgres-manifest.yaml
```

### Watch pods being created

```bash
    $ kubectl --context minikube  get pods -w --show-labels
```

### Connect to PostgreSQL

We can use the generated secret of the `postgres` robot user to connect to our `acid-minimal-cluster` master running in Minikube:

```bash
    $ export HOST_PORT=$(minikube service acid-minimal-cluster --url | sed 's,.*/,,')
    $ export PGHOST=$(echo $HOST_PORT | cut -d: -f 1)
    $ export PGPORT=$(echo $HOST_PORT | cut -d: -f 2)
    $ export PGPASSWORD=$(kubectl --context minikube get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
    $ psql -U postgres
```

## Setup development environment

The following steps guide you through the setup to work on the operator itself.

### Setting up Go

Postgres operator is written in Go. Use the [installation
instructions](https://golang.org/doc/install#install) if you don't have Go on
your system. You won't be able to compile the operator with Go older than 1.7.
We recommend installing [the latest one](https://golang.org/dl/).

Go projects expect their source code and all the dependencies to be located
under the [GOPATH](https://github.com/golang/go/wiki/GOPATH). Normally, one
would create a directory for the GOPATH (i.e. ~/go) and place the source code
under the ~/go/src subdirectories.

Given the schema above, the postgres operator source code located at
`github.com/zalando-incubator/postgres-operator` should be put at
-`~/go/src/github.com/zalando-incubator/postgres-operator`.

```bash
    $ export GOPATH=~/go
    $ mkdir -p ${GOPATH}/src/github.com/zalando-incubator/
    $ cd ${GOPATH}/src/github.com/zalando-incubator/
    $ git clone https://github.com/zalando-incubator/postgres-operator.git
```

### Building the operator

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

### Deploying self build image

The fastest way to run your docker image locally is to reuse the docker from
minikube. The following steps will get you the docker image built and deployed.

```bash
    $ eval $(minikube docker-env)
    $ export TAG=$(git describe --tags --always --dirty)
    $ make docker
    $ sed -e "s/\(image\:.*\:\).*$/\1$TAG/" manifests/postgres-operator.yaml|kubectl --context minikube create  -f -
```

## Defining database roles in the operator

Postgres operator allows defining roles to be created in the resulting database
cluster. It covers three use-cases:

* create application roles specific to the cluster described in the manifest:
  `manifest roles`.
* create application roles that should be automatically created on every
  cluster managed by the operator: `infrastructure roles`.
* automatically create users for every member of the team owning the database
  cluster: `teams API roles`.

In the next sections, we will cover those use cases in more details.

### Manifest roles

Manifest roles are defined directly in the cluster manifest. See
[minimal postgres manifest](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/minimal-postgres-manifest.yaml)
for an example of `zalando` role, defined with `superuser` and `createdb`
flags.

Manifest roles are defined as a dictionary, with a role name as a key and a
list of role options as a value. For a role without any options supply an empty
list.

The operator accepts the following options:  `superuser`, `inherit`, `login`,
`nologin`, `createrole`, `createdb`, `replication`, `bypassrls`.

By default, manifest roles are login roles (aka users), unless `nologin` is
specified explicitly.

The operator automatically generates a password for each manifest role and
places it in the secret named
`{username}.{team}-{clustername}.credentials.postgresql.acid.zalan.do` in the
same namespace as the cluster. This way, the application running in the
Kubernetes cluster and working with the database can obtain the password right
from the secret, without ever sharing it outside of the cluster.

At the moment it is not possible to define membership of the manifest role in
other roles.

### Infrastructure roles

An infrastructure role is a role that should be present on every PostgreSQL
cluster managed by the operator. An example of such a role is a monitoring
user. There are two ways to define them:

* Exclusively via the infrastructure roles secret (specified by the
  `infrastructure_roles_secret_name` parameter).

The role definition looks like this (values are base64 encoded):

```yaml
    user1: ZGJ1c2Vy
    password1: c2VjcmV0
    inrole1: b3BlcmF0b3I=
```

A block above describes the infrastructure role 'dbuser' with the password
'secret' that is the member of the 'operator' role. For the following
definitions one must increase the index, i.e. the next role will be defined as
'user2' and so on. Note that there is no way to specify role options (like
superuser or nologin) this way, and the resulting role will automatically be a
login role.

*  Via both the infrastructure roles secret and the infrastructure role
   configmap (with the same name as the infrastructure roles secret).

The infrastructure roles secret should contain an entry with 'rolename:
rolepassword' for each role, and the role description should be specified in
the configmap. Below is the example:

```yaml
    dbuser: c2VjcmV0
```

and the configmap definition for that user:

```yaml
    data:
      dbuser: |
        inrole: [operator, admin]  # following roles will be assigned to the new user
        user_flags:
          - createdb
        db_parameters:  # db parameters, applied for this particular user
          log_statement: all
```

Note that the definition above allows for more details than the one that relies
solely on the infrastructure role secret. In particular, one can allow
membership in multiple roles via the `inrole` array parameter, define role
flags via the `user_flags` list and supply per-role options through the
`db_parameters` dictionary. All those parameters are optional.

The definitions that solely use the infrastructure roles secret are more
limited and considered legacy ones; one should use the new style that specifies
infrastructure roles using both the secret and the configmap. You can mix both
in the infrastructure role secret, as long as your new-style definition can be
clearly distinguished from the old-style one (for instance, do not name
new-style roles`userN`).

Since an infrastructure role is created uniformly on all clusters managed by
the operator, it makes no sense to define it without the password. Such
definitions will be ignored with a prior warning.

See [infrastructure roles secret](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/infrastructure-roles.yaml)
and [infrastructure roles configmap](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/infrastructure-roles-configmap.yaml) for the examples.

#### Teams API roles

Teams API roles cover the task of creating human users on the cluster. The
operator calls a special Teams API endpoint (configured via the `teams_api_url`
parameter) to get the list of human users for the particular cluster. It
provides the team id (configured via the `teamId` parameter on the cluster
itself) to the teams API.

There is a demo implementation of the teams API server at [fake teams api
project](https://github.com/ikitiki/fake-teams-api). The operator expects an
OAuth2 authentication for the teams API endpoint. To fetch the OAuth2 token, it
reads the secret with the name specified by the `oauth_token_secret_name`
operator configuration. That secret should contain two fields:
`read-only-token-type` equal to `Bearer` and `read-only-token-secret`,
containing the actual token. It is the task of some external service to rotate
those tokens properly.

Once the operator gets the list of team members from the teams API, it creates
them as members of the `pam_role_name` role (configured in the operator
configuration).  The operator creates them as LOGIN roles and optionally
assigns them superuser (if `enable_team_superuser` is set) and
`team_admin_role` role (if it is set).

Note that the operator does not create any password for those roles, as those
are supposed to authenticate against the OAuth2 endpoint using the
[pam-oauth](https://github.com/CyberDem0n/pam-oauth2) module that is the part
of [Spilo](https://github.com/zalando/spilo). The operator passes the URL
specified in the `pam_configuration` parameter to Spilo, which configures the
`pg_hba.conf` authentication for `pam_role_name` group to pass the token
provided by the user (as the password) to that URL, together with the username.

The pre-requisite to this is an OAuth2 service that generates tokens for users
and provides an URL for authenticating them. Once this infrastructure is in
place, it will, combined with `pam_oauth`, give human users strong
auto-expiring passwords.

For small installations, the teams API can be disabled by setting
`enable_teams_api` to `false` in the operator configuration; then it is the
task of the cluster admin to manage human users manually.

#### Role priorities

When there is a naming conflict between roles coming from different origins
(i.e. an infrastructure role defined with the same name as the manifest role),
the operator will choose the one with the highest priority origin.

System roles (configured with `super_username` and `replication_username` in
the operator) have the highest priority; next are team API roles,
infrastructure roles and manifest roles.

There is a mechanism that prevents overriding critical roles: it is not
possible to override system roles (the operator will give an error even before
applying priority rules);  the same applies to the roles mentioned in the
`protected_role_names` list in the operator configuration.

## Debugging the operator itself

There is a web interface in the operator to observe its internal state. The
operator listens on port 8080. It is possible to expose it to the
localhost:8080 by doing:

    $ kubectl --context minikube port-forward $(kubectl --context minikube get pod -l name=postgres-operator -o jsonpath={.items..metadata.name}) 8080:8080

The inner 'query' gets the name of the postgres operator pod, and the outer
enables port forwarding. Afterwards, you can access the operator API with:

    $ curl http://127.0.0.1:8080/$endpoint| jq .

The available endpoints are listed below. Note that the worker ID is an integer
from 0 up to 'workers' - 1 (value configured in the operator configuration and
defaults to 4)

* /databases - all databases per cluster
* /workers/all/queue - state of the workers queue (cluster events to process)
* /workers/$id/queue - state of the queue for the worker $id
* /workers/$id/logs - log of the operations performed by a given worker
* /clusters/ - list of teams and clusters known to the operator
* /clusters/$team - list of clusters for the given team
* /cluster/$team/$clustername - detailed status of the cluster, including the
  specifications for CRD, master and replica services, endpoints and
  statefulsets, as well as any errors and the worker that cluster is assigned
  to.
* /cluster/$team/$clustername/logs/ - logs of all operations performed to the
  cluster so far.
* /cluster/$team/$clustername/history/ - history of cluster changes triggered
  by the changes of the manifest (shows the somewhat obscure diff and what
  exactly has triggered the change)

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
