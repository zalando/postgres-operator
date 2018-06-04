# How to create a new PostgreSQL cluster

## Create a manifest for a new PostgreSQL cluster

As an example you can take this
[minimal example](manifests/minimal-postgres-manifest.yaml):

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
spec:
  teamId: "ACID"
  volume:
    size: 1Gi
  numberOfInstances: 2
  users:
    # database owner
    zalando:
    - superuser
    - createdb

    # role for application foo
    foo_user:

  #databases: name->owner
  databases:
    foo: zalando
  postgresql:
    version: "10"
```

## Create a new Spilo cluster

```bash
$ kubectl create -f manifests/minimal-postgres-manifest.yaml
```

## Watch pods being created

```bash
$ kubectl get pods -w --show-labels
```

## Connect to PostgreSQL

We can use the generated secret of the `postgres` robot user to connect to our `acid-minimal-cluster` master running in Minikube:

```bash
$ export PGHOST=db_host
$ export PGPORT=db_port
$ export PGPASSWORD=$(kubectl get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
$ psql -U postgres
```

# Defining database roles in the operator

Postgres operator allows defining roles to be created in the resulting database
cluster. It covers three use-cases:

* create application roles specific to the cluster described in the manifest:
  `manifest roles`.
* create application roles that should be automatically created on every
  cluster managed by the operator: `infrastructure roles`.
* automatically create users for every member of the team owning the database
  cluster: `teams API roles`.

In the next sections, we will cover those use cases in more details.

## Manifest roles

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

## Infrastructure roles

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

## Use taints and tolerations for dedicated PostgreSQL nodes

To ensure Postgres pods are running on nodes without any other application
pods, you can use
[taints and tolerations](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/)
and configure the required toleration in the manifest.

```yaml
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

## How to clone an existing PostgreSQL cluster

You can spin up a new cluster as a clone of the existing one, using a clone
section in the spec. There are two options here:

* Clone directly from a source cluster using `pg_basebackup`

* Clone from an S3 bucket

### Clone directly

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql

metadata:
  name: acid-test-cluster
spec:
  clone:
    cluster: "acid-batman"
```

Here `cluster` is a name of a source cluster that is going to be cloned. The
cluster to clone is assumed to be running and the clone procedure invokes
`pg_basebackup` from it.

### Clone from S3

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql

metadata:
  name: acid-test-cluster
spec:
  clone:
    uid: "efd12e58-5786-11e8-b5a7-06148230260c"
    cluster: "acid-batman"
    timestamp: "2017-12-19T12:40:33+01:00"
```

Here `cluster` is a name of a source cluster that is going to be cloned. A new
cluster will be cloned from an S3, using the latest backup before the
`timestamp`. In this case, `uid` field is also mandatory - operator will use it
to find a correct key inside an S3 bucket. You can find this field from
metadata of a source cluster:

```yaml
apiVersion: acid.zalan.do/v1
kind: postgresql
metadata:
  name: acid-test-cluster
  uid: efd12e58-5786-11e8-b5a7-06148230260c
```

Note that timezone required for `timestamp` (offset relative to UTC, see RFC
3339 section 5.6)
