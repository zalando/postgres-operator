<h1>User Guide</h1>

Learn how to work with the Postgres Operator in a Kubernetes (K8s) environment.

## Create a manifest for a new PostgreSQL cluster

Make sure you have [set up](quickstart.md) the operator. Then you can create a
new Postgres cluster by applying manifest like this [minimal example](https://github.com/zalando/postgres-operator/blob/master/manifests/minimal-postgres-manifest.yaml):

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
spec:
  teamId: "acid"
  volume:
    size: 1Gi
  numberOfInstances: 2
  users:
    # database owner
    zalando:
    - superuser
    - createdb

    # role for application foo
    foo_user: # or 'foo_user: []'

  #databases: name->owner
  databases:
    foo: zalando
  postgresql:
    version: "17"
```

Once you cloned the Postgres Operator [repository](https://github.com/zalando/postgres-operator)
you can find this example also in the manifests folder:

```bash
kubectl create -f manifests/minimal-postgres-manifest.yaml
```

Make sure, the `spec` section of the manifest contains at least a `teamId`, the
`numberOfInstances` and the `postgresql` object with the `version` specified.
The minimum volume size to run the `postgresql` resource on Elastic Block
Storage (EBS) is `1Gi`.

Note, that when `enable_team_id_clustername_prefix` is set to `true` the name
of the cluster must start with the `teamId` and `-`. At Zalando we use team IDs
(nicknames) to lower chances of duplicate cluster names and colliding entities.
The team ID would also be used to query an API to get all members of a team
and create [database roles](#teams-api-roles) for them. Besides, the maximum
cluster name length is 53 characters.

## Watch pods being created

Check if the database pods are coming up. Use the label `application=spilo` to
filter and list the label `spilo-role` to see when the master is promoted and
replicas get their labels.

```bash
kubectl get pods -l application=spilo -L spilo-role -w
```

The operator also emits K8s events to the Postgresql CRD which can be inspected
in the operator logs or with:

```bash
kubectl describe postgresql acid-minimal-cluster
```

## Connect to PostgreSQL

With a `port-forward` on one of the database pods (e.g. the master) you can
connect to the PostgreSQL database from your machine. Use labels to filter for
the master pod of our test cluster.

```bash
# get name of master pod of acid-minimal-cluster
export PGMASTER=$(kubectl get pods -o jsonpath={.items..metadata.name} -l application=spilo,cluster-name=acid-minimal-cluster,spilo-role=master -n default)

# set up port forward
kubectl port-forward $PGMASTER 6432:5432 -n default
```

Open another CLI and connect to the database using e.g. the psql client.
When connecting with a manifest role like `foo_user` user, read its password
from the K8s secret which was generated when creating `acid-minimal-cluster`.
As non-encrypted connections are rejected by default set SSL mode to `require`:

```bash
export PGPASSWORD=$(kubectl get secret postgres.acid-minimal-cluster.credentials.postgresql.acid.zalan.do -o 'jsonpath={.data.password}' | base64 -d)
export PGSSLMODE=require
psql -U postgres -h localhost -p 6432
```

## Password encryption

Passwords are encrypted with `md5` hash generation by default. However, it is
possible to use the more recent `scram-sha-256` method by changing the
`password_encryption` parameter in the Postgres config. You can define it
directly from the cluster manifest:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
spec:
  [...]
  postgresql:
    version: "17"
    parameters:
      password_encryption: scram-sha-256
```

## Defining database roles in the operator

Postgres Operator allows defining roles to be created in the resulting database
cluster. It covers three use-cases:

* `manifest roles`: create application roles specific to the cluster described
in the manifest.
* `infrastructure roles`: create application roles that should be automatically
created on every cluster managed by the operator.
* `teams API roles`: automatically create users for every member of the team
owning the database cluster.

In the next sections, we will cover those use cases in more details. Note, that
the Postgres Operator can also create databases with pre-defined owner, reader
and writer roles which saves you the manual setup. Read more in the next
chapter.

### Manifest roles

Manifest roles are defined directly in the cluster manifest. See
[minimal postgres manifest](https://github.com/zalando/postgres-operator/blob/master/manifests/minimal-postgres-manifest.yaml)
for an example of `zalando` role, defined with `superuser` and `createdb` flags.

Manifest roles are defined as a dictionary, with a role name as a key and a
list of role options as a value. For a role without any options it is best to
supply the empty list `[]`. It is also possible to leave this field empty as in
our example manifests. In certain cases such empty field may be missing later
removed by K8s [due to the `null` value it gets](https://kubernetes.io/docs/concepts/overview/object-management-kubectl/declarative-config/#how-apply-calculates-differences-and-merges-changes)
(`foobar_user:` is equivalent to `foobar_user: null`).

The operator accepts the following options:  `superuser`, `inherit`, `login`,
`nologin`, `createrole`, `createdb`, `replication`, `bypassrls`.

By default, manifest roles are login roles (aka users), unless `nologin` is
specified explicitly.

The operator automatically generates a password for each manifest role and
places it in the secret named
`{username}.{clustername}.credentials.postgresql.acid.zalan.do` in the
same namespace as the cluster. This way, the application running in the
K8s cluster and connecting to Postgres can obtain the password right from the
secret, without ever sharing it outside of the cluster.

At the moment it is not possible to define membership of the manifest role in
other roles.

To define the secrets for the users in a different namespace than that of the
cluster, one can set `enable_cross_namespace_secret` and declare the namespace
for the secrets in the manifest in the following manner (note, that it has to
be reflected in the `database` section, too),

```yaml
spec:
  users:
    # users with secret in different namespace
    appspace.db_user:
    - createdb
  databases:
    # namespace notation is part of user name
    app_db: appspace.db_user
```

Here, anything before the first dot is considered the namespace and the text after
the first dot is the username. Also, the postgres roles of these usernames would
be in the form of `namespace.username`.

For such usernames, the secret is created in the given namespace and its name is
of the following form,
`{namespace}.{username}.{clustername}.credentials.postgresql.acid.zalan.do`

### Infrastructure roles

An infrastructure role is a role that should be present on every PostgreSQL
cluster managed by the operator. An example of such a role is a monitoring
user. There are two ways to define them:

* With the infrastructure roles secret only
* With both the the secret and the infrastructure role ConfigMap.

#### Infrastructure roles secret

Infrastructure roles can be specified by the `infrastructure_roles_secrets`
parameter where you can reference multiple existing secrets. Prior to `v1.6.0`
the operator could only reference one secret with the
`infrastructure_roles_secret_name` option. However, this secret could contain
multiple roles using the same set of keys plus incrementing index.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: postgresql-infrastructure-roles
data:
  user1: ZGJ1c2Vy
  password1: c2VjcmV0
  inrole1: b3BlcmF0b3I=
  user2: ...
```

The block above describes the infrastructure role 'dbuser' with password
'secret' that is a member of the 'operator' role. The resulting role will
automatically be a login role.

With the new option users can configure the names of secret keys that contain
the user name, password etc. The secret itself is referenced by the
`secretname` key. If the secret uses a template for multiple roles as described
above list them separately.

```yaml
apiVersion: "acid.zalan.do/v1"
kind: OperatorConfiguration
metadata:
  name: postgresql-operator-configuration
configuration:
  kubernetes:
    infrastructure_roles_secrets:
    - secretname: "postgresql-infrastructure-roles"
      userkey: "user1"
      passwordkey: "password1"
      rolekey: "inrole1"
    - secretname: "postgresql-infrastructure-roles"
      userkey: "user2"
      ...
```

Note, only the CRD-based configuration allows for referencing multiple secrets.
As of now, the ConfigMap is restricted to either one or the existing template
option with `infrastructure_roles_secret_name`. Please, refer to the example
manifests to understand how `infrastructure_roles_secrets` has to be configured
for the [configmap](https://github.com/zalando/postgres-operator/blob/master/manifests/configmap.yaml) or [CRD configuration](https://github.com/zalando/postgres-operator/blob/master/manifests/postgresql-operator-default-configuration.yaml).

If both `infrastructure_roles_secret_name` and `infrastructure_roles_secrets`
are defined the operator will create roles for both of them. So make sure,
they do not collide. Note also, that with definitions that solely use the
infrastructure roles secret there is no way to specify role options (like
superuser or nologin) or role memberships. This is where the additional
ConfigMap comes into play.

#### Secret plus ConfigMap

A [ConfigMap](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-configmap/)
allows for defining more details regarding the infrastructure roles. Therefore,
one should use the new style that specifies infrastructure roles using both the
secret and a ConfigMap. The ConfigMap must have the same name as the secret.
The secret should contain an entry with 'rolename:rolepassword' for each role.

```yaml
dbuser: c2VjcmV0
```

And the role description for that user should be specified in the ConfigMap.

```yaml
data:
  dbuser: |
    inrole: [operator, admin]  # following roles will be assigned to the new user
    user_flags:
    - createdb
    db_parameters:  # db parameters, applied for this particular user
      log_statement: all
```

One can allow membership in multiple roles via the `inrole` array parameter,
define role flags via the `user_flags` list and supply per-role options through
the `db_parameters` dictionary. All those parameters are optional.

Both definitions can be mixed in the infrastructure role secret, as long as
your new-style definition can be clearly distinguished from the old-style one
(for instance, do not name new-style roles `userN`).

Since an infrastructure role is created uniformly on all clusters managed by
the operator, it makes no sense to define it without the password. Such
definitions will be ignored with a prior warning.

See [infrastructure roles secret](https://github.com/zalando/postgres-operator/blob/master/manifests/infrastructure-roles.yaml)
and [infrastructure roles configmap](https://github.com/zalando/postgres-operator/blob/master/manifests/infrastructure-roles-configmap.yaml)
for the examples.

### Teams API roles

These roles are meant for database activity of human users. It's possible to
configure the operator to automatically create database roles for lets say all
employees of one team. They are not listed in the manifest and there are no K8s
secrets created for them. Instead they would use an OAuth2 token to connect. To
get all members of the team the operator queries a defined API endpoint that
returns usernames. A minimal Teams API should work like this:

```
/.../<teamname> -> ["name","anothername"]
```

A ["fake" Teams API](https://github.com/zalando/postgres-operator/blob/master/manifests/fake-teams-api.yaml) deployment is provided
in the manifests folder to set up a basic API around whatever services is used
for user management. The Teams API's URL is set in the operator's
[configuration](reference/operator_parameters.md#automatic-creation-of-human-users-in-the-database)
and `enable_teams_api` must be set to `true`. There are more settings available
to choose superusers, group roles, [PAM configuration](https://github.com/CyberDem0n/pam-oauth2)
etc. An OAuth2 token can be passed to the Teams API via a secret. The name for
this secret is configurable with the `oauth_token_secret_name` parameter.

### Additional teams and members per cluster

Postgres clusters are associated with one team by providing the `teamID` in
the manifest. Additional superuser teams can be configured as mentioned in
the previous paragraph. However, this is a global setting. To assign
additional teams, superuser teams and single users to clusters of a given
team, use the [PostgresTeam CRD](https://github.com/zalando/postgres-operator/blob/master/manifests/postgresteam.crd.yaml).

Note, by default the `PostgresTeam` support is disabled in the configuration.
Switch `enable_postgres_team_crd` flag to `true` and the operator will start to
watch for this CRD. Make sure, the cluster role is up to date and contains a
section for [PostgresTeam](https://github.com/zalando/postgres-operator/blob/master/manifests/operator-service-account-rbac.yaml#L30).

#### Additional teams

To assign additional teams and single users to clusters of a given team,
define a mapping with the `PostgresTeam` Kubernetes resource. The Postgres
Operator will read such team mappings each time it syncs all Postgres clusters.

```yaml
apiVersion: "acid.zalan.do/v1"
kind: PostgresTeam
metadata:
  name: custom-team-membership
spec:
  additionalTeams:
    a-team:
    - "b-team"
```

With the example above the operator will create login roles for all members
of `b-team` in every cluster owned by `a-team`. It's possible to do vice versa
for clusters of `b-team` in one manifest:

```yaml
spec:
  additionalTeams:
    a-team:
    - "b-team"
    b-team:
    - "a-team"
```

You see, the `PostgresTeam` CRD is a global team mapping and independent from
the Postgres manifests. It is possible to define multiple mappings, even with
redundant content - the Postgres operator will create one internal cache from
it. Additional teams are resolved transitively, meaning you will also add
users for their `additionalTeams`, e.g.:

```yaml
spec:
  additionalTeams:
    a-team:
    - "b-team"
    - "c-team"
    b-team:
    - "a-team"
```

This creates roles for members of the `c-team` team not only in all clusters
owned by `a-team`, but as well in cluster owned by `b-team`, as `a-team` is
an `additionalTeam` to `b-team`

Not, you can also define `additionalSuperuserTeams` in the `PostgresTeam`
manifest. By default, this option is disabled and must be configured with
`enable_postgres_team_crd_superusers` to make it work.

#### Virtual teams

There can be "virtual teams" that do not exist in the Teams API. It can make
it easier to map a group of teams to many other teams:

```yaml
spec:
  additionalTeams:
    a-team:
    - "virtual-team"
    b-team:
    - "virtual-team"
    virtual-team:
    - "c-team"
    - "d-team"
```

This example would create roles for members of `c-team` and `d-team` plus
additional `virtual-team` members in clusters owned by `a-team` or `b-team`.

#### Teams changing their names

With `PostgresTeams` it is also easy to cover team name changes. Just add
the mapping between old and new team name and the rest can stay the same.
E.g. if team `a-team`'s name would change to `f-team` in the teams API it
could be reflected in a `PostgresTeam` mapping with just two lines:

```yaml
spec:
  additionalTeams:
    a-team:
    - "f-team"
```

This is helpful, because Postgres cluster names are immutable and can not
be changed. Only via cloning it could get a different name starting with the
new `teamID`.

#### Additional members

Single members might be excluded from teams although they continue to work
with the same people. However, the teams API would not reflect this anymore.
To still add a database role for former team members list their role under
the `additionalMembers` section of the `PostgresTeam` resource:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: PostgresTeam
metadata:
  name: custom-team-membership
spec:
  additionalMembers:
    a-team:
    - "tia"
```

This will create the login role `tia` in every cluster owned by `a-team`.
The user can connect to databases like the other team members.

The `additionalMembers` map can also be used to define users of virtual
teams, e.g. for `virtual-team` we used above:

```yaml
spec:
  additionalMembers:
    virtual-team:
    - "flynch"
    - "rdecker"
    - "briggs"
```

#### Removed members

The Postgres Operator does not delete database roles when users are removed
from manifests. But, using the `PostgresTeam` custom resource or Teams API it
is very easy to add roles to many clusters. Manually reverting such a change
is cumbersome. Therefore, if members are removed from a `PostgresTeam` or the
Teams API the operator can rename roles appending a configured suffix to the
name (see `role_deletion_suffix` option) and revoke the `LOGIN` privilege.
The suffix makes it easy then for a cleanup script to remove those deprecated
roles completely. Switch `enable_team_member_deprecation` to `true` to enable
this behavior.

When a role is re-added to a `PostgresTeam` manifest (or to the source behind
the Teams API) the operator will check for roles with the configured suffix
and if found, rename the role back to the original name and grant `LOGIN`
again.

## Prepared databases with roles and default privileges

The `users` section in the manifests only allows for creating database roles
with global privileges. Fine-grained data access control or role membership can
not be defined and must be set up by the user in the database. But, the Postgres
Operator offers a separate section to specify `preparedDatabases` that will be
created with pre-defined owner, reader and writer roles for each individual
database and, optionally, for each database schema, too. `preparedDatabases`
also enable users to specify PostgreSQL extensions that shall be created in a
given database schema.

### Default database and schema

A prepared database is already created by adding an empty `preparedDatabases`
section to the manifest. The database will then be called like the Postgres
cluster manifest (`-` are replaced with `_`) and will also contain a schema
called `data`.

```yaml
spec:
  preparedDatabases: {}
```

### Default NOLOGIN roles

Given an example with a specified database and schema:

```yaml
spec:
  preparedDatabases:
    foo:
      schemas:
        bar: {}
```

Postgres Operator will create the following NOLOGIN roles:

| Role name      | Member of      | Admin         |
| -------------- | -------------- | ------------- |
| foo_owner      |                | admin         |
| foo_reader     |                | foo_owner     |
| foo_writer     | foo_reader     | foo_owner     |
| foo_bar_owner  |                | foo_owner     |
| foo_bar_reader |                | foo_bar_owner |
| foo_bar_writer | foo_bar_reader | foo_bar_owner |

The `<dbname>_owner` role is the database owner and should be used when creating
new database objects. All members of the `admin` role, e.g. teams API roles, can
become the owner with the `SET ROLE` command. [Default privileges](https://www.postgresql.org/docs/17/sql-alterdefaultprivileges.html)
are configured for the owner role so that the `<dbname>_reader` role
automatically gets read-access (SELECT) to new tables and sequences and the
`<dbname>_writer` receives write-access (INSERT, UPDATE, DELETE on tables,
USAGE and UPDATE on sequences). Both get USAGE on types and EXECUTE on
functions.

The same principle applies for database schemas which are owned by the
`<dbname>_<schema>_owner` role. `<dbname>_<schema>_reader` is read-only,
`<dbname>_<schema>_writer` has write access and inherit reading from the reader
role. Note, that the `<dbname>_*` roles have access incl. default privileges on
all schemas, too. If you don't need the dedicated schema roles - i.e. you only
use one schema - you can disable the creation like this:

```yaml
spec:
  preparedDatabases:
    foo:
      schemas:
        bar:
          defaultRoles: false
```

Then, the schemas are owned by the database owner, too.

### Default LOGIN roles

The roles described in the previous paragraph can be granted to LOGIN roles from
the `users` section in the manifest. Optionally, the Postgres Operator can also
create default LOGIN roles for the database and each schema individually. These
roles will get the `_user` suffix and they inherit all rights from their NOLOGIN
counterparts. Therefore, you cannot have `defaultRoles` set to `false` and enable
`defaultUsers` at the same time.

| Role name           | Member of      | Admin         |
| ------------------- | -------------- | ------------- |
| foo_owner_user      | foo_owner      | admin         |
| foo_reader_user     | foo_reader     | foo_owner     |
| foo_writer_user     | foo_writer     | foo_owner     |
| foo_bar_owner_user  | foo_bar_owner  | foo_owner     |
| foo_bar_reader_user | foo_bar_reader | foo_bar_owner |
| foo_bar_writer_user | foo_bar_writer | foo_bar_owner |

These default users are enabled in the manifest with the `defaultUsers` flag:

```yaml
spec:
  preparedDatabases:
    foo:
      defaultUsers: true
      schemas:
        bar:
          defaultUsers: true
```

Default access privileges are also defined for LOGIN roles on database and
schema creation. This means they are currently not set when `defaultUsers`
(or `defaultRoles` for schemas) are enabled at a later point in time.

For all LOGIN roles the operator will create K8s secrets in the namespace
specified in `secretNamespace`, if `enable_cross_namespace_secret` is set to
`true` in the config. Otherwise, they are created in the same namespace like
the Postgres cluster. Unlike roles specified with `namespace.username` under
`users`, the namespace will not be part of the role name here. Keep in mind
that the underscores in a role name are replaced with dashes in the K8s
secret name.

```yaml
spec:
  preparedDatabases:
    foo:
      defaultUsers: true
      secretNamespace: appspace
```

### Schema `search_path` for default roles

The schema [`search_path`](https://www.postgresql.org/docs/17/ddl-schemas.html#DDL-SCHEMAS-PATH)
for each role will include the role name and the schemas, this role should have
access to. So `foo_bar_writer` does not have to schema-qualify tables from
schemas `foo_bar_writer, bar`, while `foo_writer` can look up `foo_writer` and
any schema listed under `schemas`. To register the default `public` schema in
the `search_path` (because some extensions are installed there) one has to add
the following (assuming no extra roles are desired only for the public schema):

```yaml
spec:
  preparedDatabases:
    foo:
      schemas:
        public:
          defaultRoles: false
```

### Database extensions

Prepared databases also allow for creating Postgres extensions. They will be
created by the database owner in the specified schema.

```yaml
spec:
  preparedDatabases:
    foo:
      extensions:
        pg_partman: public
        postgis: data
```

Some extensions require SUPERUSER rights on creation unless they are not
allowed by the [pgextwlist](https://github.com/dimitri/pgextwlist) extension,
that is shipped with the Spilo image. To see which extensions are on the list
check the `extwlist.extension` parameter in the postgresql.conf file.

```bash
SHOW extwlist.extensions;
```

Make sure that `pgextlist` is also listed under `shared_preload_libraries` in
the PostgreSQL configuration. Then the database owner should be able to create
the extension specified in the manifest.

### From `databases` to `preparedDatabases`

If you wish to create the role setup described above for databases listed under
the `databases` key, you have to make sure that the owner role follows the
`<dbname>_owner` naming convention of `preparedDatabases`. As roles are synced
first, this can be done with one edit:

```yaml
# before
spec:
  databases:
    foo: db_owner

# after
spec:
  databases:
    foo: foo_owner
  preparedDatabases:
    foo:
      schemas:
        my_existing_schema: {}
```

Adding existing database schemas to the manifest to create roles for them as
well is up the user and not done by the operator. Remember that if you don't
specify any schema a new database schema called `data` will be created. When
everything got synced (roles, schemas, extensions), you are free to remove the
database from the `databases` section. Note, that the operator does not delete
database objects or revoke privileges when removed from the manifest.

## Resource definition

The compute resources to be used for the Postgres containers in the pods can be
specified in the postgresql cluster manifest.

```yaml
spec:
  resources:
    requests:
      cpu: 10m
      memory: 100Mi
    limits:
      cpu: 300m
      memory: 300Mi
```

The minimum limits to properly run the `postgresql` resource are configured to
`250m` for `cpu` and `250Mi` for `memory`. If a lower value is set in the
manifest the operator will raise the limits to the configured minimum values.
If no resources are defined in the manifest they will be obtained from the
configured [default requests](reference/operator_parameters.md#kubernetes-resource-requests).
If neither defaults nor minimum limits are configured the operator will not
specify any resources and it's up to K8s (or your own) admission hooks to
handle it.

### HugePages support

The operator supports [HugePages](https://www.postgresql.org/docs/17/kernel-resources.html#LINUX-HUGEPAGES).
To enable HugePages, set the matching resource requests and/or limits in the manifest:

```yaml
spec:
  resources:
    requests:
      hugepages-2Mi: 250Mi
      hugepages-1Gi: 1Gi
    limits:
      hugepages-2Mi: 500Mi
      hugepages-1Gi: 2Gi
```

There are no minimums or maximums and the default is 0 for both HugePage sizes,
but Kubernetes will not spin up the pod if the requested HugePages cannot be allocated.
For more information on HugePages in Kubernetes, see also
[https://kubernetes.io/docs/tasks/manage-hugepages/scheduling-hugepages/](https://kubernetes.io/docs/tasks/manage-hugepages/scheduling-hugepages/)

## Use taints, tolerations and node affinity for dedicated PostgreSQL nodes

To ensure Postgres pods are running on nodes without any other application pods,
you can use [taints and tolerations](https://kubernetes.io/docs/concepts/configuration/taint-and-toleration/)
and configure the required toleration in the manifest. Tolerations can also be
defined in the [operator config](administrator.md#use-taints-and-tolerations-for-dedicated-postgresql-nodes)
to apply for all Postgres clusters.

```yaml
spec:
  tolerations:
  - key: postgres
    operator: Exists
    effect: NoSchedule
```

If you need the pods to be scheduled on specific nodes you may use [node affinity](https://kubernetes.io/docs/tasks/configure-pod-container/assign-pods-nodes-using-node-affinity/)
to specify a set of label(s), of which a prospective host node must have at least one. This could be used to
place nodes with certain hardware capabilities (e.g. SSD drives) in certain environments or network segments,
e.g. for PCI compliance.

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster
spec:
  teamId: "ACID"
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: environment
          operator: In
          values:
          - pci
```

If you need to define a `nodeAffinity` for all your Postgres clusters use the
`node_readiness_label` [configuration](administrator.md#node-readiness-labels).

## In-place major version upgrade

Starting with Spilo 13, operator supports in-place major version upgrade to a
higher major version (e.g. from PG 14 to PG 16). To trigger the upgrade,
simply increase the version in the manifest. It is your responsibility to test
your applications against the new version before the upgrade; downgrading is
not supported. The easiest way to do so is to try the upgrade on the cloned
cluster first (see next chapter). More details can be found in the
[admin docs](administrator.md#minor-and-major-version-upgrade).

## How to clone an existing PostgreSQL cluster

You can spin up a new cluster as a clone of the existing one, using a `clone`
section in the spec. There are two options here:

* Clone from an S3 bucket (recommended)
* Clone directly from a source cluster

Note, that cloning can also be used for [major version upgrades](administrator.md#minor-and-major-version-upgrade)
of PostgreSQL.

### Clone from S3

Cloning from S3 has the advantage that there is no impact on your production
database. A new Postgres cluster is created by restoring the data of another
source cluster. If you create it in the same Kubernetes environment, use a
different name.

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql
metadata:
  name: acid-minimal-cluster-clone
spec:
  clone:
    uid: "efd12e58-5786-11e8-b5a7-06148230260c"
    cluster: "acid-minimal-cluster"
    timestamp: "2017-12-19T12:40:33+01:00"
```

Here `cluster` is a name of a source cluster that is going to be cloned. A new
cluster will be cloned from S3, using the latest backup before the `timestamp`.
Note, a time zone is required for `timestamp` in the format of `+00:00` (UTC).

The operator will try to find the WAL location based on the configured
`wal_[s3|gs]_bucket` or `wal_az_storage_account` and the specified `uid`.
You can find the UID of the source cluster in its metadata:

```yaml
apiVersion: acid.zalan.do/v1
kind: postgresql
metadata:
  name: acid-minimal-cluster
  uid: efd12e58-5786-11e8-b5a7-06148230260c
```

If your source cluster uses a WAL location different from the global
configuration you can specify the full path under `s3_wal_path`. For
[Google Cloud Platform](administrator.md#google-cloud-platform-setup)
or [Azure](administrator.md#azure-setup)
it can only be set globally with [custom Pod environment variables](administrator.md#custom-pod-environment-variables)
or locally in the Postgres manifest's [`env`](administrator.md#via-postgres-cluster-manifest) section.


For non AWS S3 following settings can be set to support cloning from other S3
implementations:

```yaml
spec:
  clone:
    uid: "efd12e58-5786-11e8-b5a7-06148230260c"
    cluster: "acid-minimal-cluster"
    timestamp: "2017-12-19T12:40:33+01:00"
    s3_wal_path: "s3://custom/path/to/bucket"
    s3_endpoint: https://s3.acme.org
    s3_access_key_id: 0123456789abcdef0123456789abcdef
    s3_secret_access_key: 0123456789abcdef0123456789abcdef
    s3_force_path_style: true
```

### Clone directly

Another way to get a fresh copy of your source DB cluster is via
[pg_basebackup](https://www.postgresql.org/docs/17/app-pgbasebackup.html). To
use this feature simply leave out the timestamp field from the clone section.
The operator will connect to the service of the source cluster by name. If the
cluster is called test, then the connection string will look like host=test
port=5432), which means that you can clone only from clusters within the same
namespace.

```yaml
spec:
  clone:
    cluster: "acid-minimal-cluster"
```

Be aware that on a busy source database this can result in an elevated load!

## Restore in place

There is also a possibility to restore a database without cloning it. The
advantage to this is that there is no need to change anything on the
application side. However, as it involves deleting the database first, this
process is of course riskier than cloning (which involves adjusting the
connection parameters of the app).

First, make sure there is no writing activity on your DB, and save the UID.
Then delete the `postgresql` K8S resource:

```bash
zkubectl delete postgresql acid-test-restore
```

Then deploy a new manifest with the same name, referring to itself
(both name and UID) in the `clone` section:

```yaml
metadata:
  name: acid-minimal-cluster
  # [...]
spec:
  # [...]
  clone:
    cluster: "acid-minimal-cluster"  # the same as metadata.name above!
    uid: "<original_UID>"
    timestamp: "2022-04-01T10:11:12.000+00:00"
```

This will create a new database cluster with the same name but different UID,
whereas the database will be in the state it was at the specified time.

:warning: The backups and WAL files for the original DB are retained under the
original UID, making it possible retry restoring. However, it is probably
better to create a temporary clone for experimenting or finding out to which
point you should restore.

## Setting up a standby cluster

Standby cluster is a [Patroni feature](https://github.com/zalando/patroni/blob/master/docs/replica_bootstrap.rst#standby-cluster)
that first clones a database, and keeps replicating changes afterwards. It can
exist in a different location than its source database, but unlike cloning,
the PostgreSQL version between source and target cluster has to be the same.

To start a cluster as standby, add the following `standby` section in the YAML
file. You can stream changes from archived WAL files (AWS S3 or Google Cloud
Storage) or from a remote primary. Only one option can be specified in the
manifest:

```yaml
spec:
  standby:
    s3_wal_path: "s3://<bucketname>/spilo/<source_db_cluster>/<UID>/wal/<PGVERSION>"
```

For GCS, you have to define STANDBY_GOOGLE_APPLICATION_CREDENTIALS as a
[custom pod environment variable](administrator.md#custom-pod-environment-variables).
It is not set from the config to allow for overriding.

```yaml
spec:
  standby:
    gs_wal_path: "gs://<bucketname>/spilo/<source_db_cluster>/<UID>/wal/<PGVERSION>"
```

For a remote primary you specify the host address and optionally the port.
If you leave out the port Patroni will use `"5432"`.

```yaml
spec:
  standby:
    standby_host: "acid-minimal-cluster.default"
    standby_port: "5433"
```

Note, that the pods and services use the same role labels like for normal clusters:
The standby leader is labeled as `master`. When using the `standby_host` option
you have to copy the credentials from the source cluster's secrets to successfully
bootstrap a standby cluster (see next chapter).

### Providing credentials of source cluster

A standby cluster is replicating the data (including users and passwords) from
the source database and is read-only. The system and application users (like
standby, postgres etc.) all have a password that does not match the credentials
stored in secrets which are created by the operator. You have two options:

a. Create secrets manually beforehand and paste the credentials of the source
   cluster
b. Let the operator create the secrets when it bootstraps the standby cluster.
   Patch the secrets with the credentials of the source cluster. Replace the
   spilo pods.

Otherwise, you will see errors in the Postgres logs saying users cannot log in
and the operator logs will complain about not being able to sync resources.
If you stream changes from a remote primary you have to align the secrets or
the standby cluster will not start up.

If you stream changes from WAL files and you only run a standby leader, you
can safely ignore the secret mismatch, as it will be sorted out once the
cluster is detached from the source. It is also harmless if you do not plan it.
But, when you create a standby replica, too, fix the credentials right away.
WAL files will pile up on the standby leader if no connection can be
established between standby replica(s).

### Promote the standby

One big advantage of standby clusters is that they can be promoted to a proper
database cluster. This means it will stop replicating changes from the source,
and start accept writes itself. This mechanism makes it possible to move
databases from one place to another with minimal downtime.

Before promoting a standby cluster, make sure that the standby is not behind
the source database. You should ideally stop writes to your source cluster and
then create a dummy database object that you check for being replicated in the
target to verify all data has been copied.

To promote, remove the `standby` section from the postgres cluster manifest.
A rolling update will be triggered removing the `STANDBY_*` environment
variables from the pods, followed by a Patroni config update that promotes the
cluster.

### Adding standby section after promotion

Turning a running cluster into a standby is not easily possible and should be
avoided. The best way is to remove the cluster and resubmit the manifest
after a short wait of a few minutes. Adding the `standby` section would turn
the database cluster in read-only mode on next operator SYNC cycle but it
does not sync automatically with the source cluster again.

## Sidecar Support

Each cluster can specify arbitrary sidecars to run. These containers could be
used for log aggregation, monitoring, backups or other tasks. A sidecar can be
specified like this:

```yaml
spec:
  sidecars:
    - name: "container-name"
      image: "company/image:tag"
      resources:
        limits:
          cpu: 500m
          memory: 500Mi
        requests:
          cpu: 100m
          memory: 100Mi
      env:
        - name: "ENV_VAR_NAME"
          value: "any-k8s-env-things"
      command: ['sh', '-c', 'echo "logging" > /opt/logs.txt']
```

In addition to any environment variables you specify, the following environment
variables are always passed to sidecars:

  - `POD_NAME` - field reference to `metadata.name`
  - `POD_NAMESPACE` - field reference to `metadata.namespace`
  - `POSTGRES_USER` - the superuser that can be used to connect to the database
  - `POSTGRES_PASSWORD` - the password for the superuser

The PostgreSQL volume is shared with sidecars and is mounted at
`/home/postgres/pgdata`.

**Note**: The operator will not create a cluster if sidecar containers are
specified but globally disabled in the configuration. The `enable_sidecars`
option must be set to `true`.

If you want to add a sidecar to every cluster managed by the operator, you can specify it in the [operator configuration](administrator.md#sidecars-for-postgres-clusters) instead.

### Accessing the PostgreSQL socket from sidecars

If enabled by the `share_pgsocket_with_sidecars` option in the operator
configuration the PostgreSQL socket is placed in a volume of type `emptyDir`
named `postgresql-run`. To allow access to the socket from any sidecar
container simply add a VolumeMount to this volume to your sidecar spec.

```yaml
  - name: "container-name"
    image: "company/image:tag"
    volumeMounts:
    - mountPath: /var/run
      name: postgresql-run
```

If you do not want to globally enable this feature and only use it for single
Postgres clusters, specify an `EmptyDir` volume under `additionalVolumes` in
the manifest:

```yaml
spec:
  additionalVolumes:
  - name: postgresql-run
    mountPath: /var/run/postgresql
    targetContainers:
    - all
    volumeSource:
      emptyDir: {}
  sidecars: 
  - name: "container-name"
    image: "company/image:tag"
    volumeMounts:
    - mountPath: /var/run
      name: postgresql-run
```

## InitContainers Support

Each cluster can specify arbitrary init containers to run. These containers can
be used to run custom actions before any normal and sidecar containers start.
An init container can be specified like this:

```yaml
spec:
  initContainers:
    - name: "container-name"
      image: "company/image:tag"
      env:
        - name: "ENV_VAR_NAME"
          value: "any-k8s-env-things"
```

`initContainers` accepts full `v1.Container` definition.

**Note**: The operator will not create a cluster if `initContainers` are
specified but globally disabled in the configuration. The
`enable_init_containers` option must be set to `true`.

## Increase volume size

Postgres operator supports statefulset volume resize without doing a rolling
update. For that you need to change the size field of the volume description
in the cluster manifest and apply the change:

```yaml
spec:
  volume:
    size: 5Gi # new volume size
```

The operator compares the new value of the size field with the previous one and
acts on differences. The `storage_resize_mode` can be configured. By default,
the operator will adjust the PVCs and leave it to K8s and the infrastructure to
apply the change.

When using AWS with gp3 volumes you should set the mode to `mixed` because it
will also adjust the IOPS and throughput that can be defined in the manifest.
Check the [AWS docs](https://aws.amazon.com/ebs/general-purpose/) to learn
about default and maximum values. Keep in mind that AWS rate-limits updating
volume specs to no more than once every 6 hours.

```yaml
spec:
  volume:
    size: 5Gi # new volume size
    iops: 4000
    throughput: 500
```

The operator can only enlarge volumes. Shrinking is not supported and will emit
a warning. However, it can be done manually after updating the manifest. You
have to delete the PVC, which will hang until you also delete the corresponding
pod. Proceed with the next pod when the cluster is healthy again and replicas
are streaming.

## Logical backups

You can enable logical backups (SQL dumps) from the cluster manifest by adding
the following parameter in the spec section:

```yaml
spec:
  enableLogicalBackup: true
```

The operator will create and sync a K8s cron job to do periodic logical backups
of this particular Postgres cluster. Due to the [limitation of K8s cron jobs](https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#cron-job-limitations)
it is highly advisable to set up additional monitoring for this feature; such
monitoring is outside the scope of operator responsibilities. See
[configuration reference](reference/cluster_manifest.md) and
[administrator documentation](administrator.md) for details on how backups are
executed.

## Connection pooler

The operator can create a database side connection pooler for those applications
where an application side pooler is not feasible, but a number of connections is
high. To create a connection pooler together with a database, modify the
manifest:

```yaml
spec:
  enableConnectionPooler: true
  enableReplicaConnectionPooler: true
```

This will tell the operator to create a connection pooler with default
configuration, through which one can access the master via a separate service
`{cluster-name}-pooler`. With the first option, connection pooler for master service
is created and with the second option, connection pooler for replica is created.
Note that both of these flags are independent of each other and user can set or
unset any of them as per their requirements without any effect on the other.

In most of the cases the
[default configuration](reference/operator_parameters.md#connection-pooler-configuration)
should be good enough. To configure a new connection pooler individually for
each Postgres cluster, specify:

```
spec:
  connectionPooler:
    # how many instances of connection pooler to create
    numberOfInstances: 2

    # in which mode to run, session or transaction
    mode: "transaction"

    # schema, which operator will create in each database
    # to install credentials lookup function for connection pooler
    schema: "pooler"

    # user, which operator will create for connection pooler
    user: "pooler"

    # resources for each instance
    resources:
      requests:
        cpu: 500m
        memory: 100Mi
      limits:
        cpu: "1"
        memory: 100Mi
```

The `enableConnectionPooler` flag is not required when the `connectionPooler`
section is present in the manifest. But, it can be used to disable/remove the
pooler while keeping its configuration.

By default, [`PgBouncer`](https://www.pgbouncer.org/) is used as connection pooler.
To find out about pool modes read the `PgBouncer` [docs](https://www.pgbouncer.org/config.html#pooler_mode)
(but it should be the general approach between different implementation).

Note, that using `PgBouncer` a meaningful resource CPU limit should be 1 core
or less (there is a way to utilize more than one, but in K8s it's easier just to
spin up more instances).

## Custom TLS certificates

By default, the Spilo image generates its own TLS certificate during startup.
However, this certificate cannot be verified and thus doesn't protect from
active MITM attacks. In this section we show how to specify a custom TLS
certificate which is mounted in the database pods via a K8s Secret.

Before applying these changes, in k8s the operator must also be configured with
the `spilo_fsgroup` set to the GID matching the postgres user group. If you
don't know the value, use `103` which is the GID from the default Spilo image
(`spilo_fsgroup=103` in the cluster request spec).

OpenShift allocates the users and groups dynamically (based on scc), and their
range is different in every namespace. Due to this dynamic behaviour, it's not
trivial to know at deploy time the uid/gid of the user in the cluster.
Therefore, instead of using a global `spilo_fsgroup` setting in operator
configuration or use the `spiloFSGroup` field per Postgres cluster manifest.

For testing purposes, you can generate a self-signed certificate with openssl:
```sh
openssl req -x509 -nodes -newkey rsa:2048 -keyout tls.key -out tls.crt -subj "/CN=acid.zalan.do"
```

Upload the cert as a kubernetes secret:
```sh
kubectl create secret tls pg-tls \
  --key tls.key \
  --cert tls.crt
```

When doing client auth, CA can come optionally from the same secret:
```sh
kubectl create secret generic pg-tls \
  --from-file=tls.crt=server.crt \
  --from-file=tls.key=server.key \
  --from-file=ca.crt=ca.crt
```

Then configure the postgres resource with the TLS secret:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql

metadata:
  name: acid-test-cluster
spec:
  tls:
    secretName: "pg-tls"
    caFile: "ca.crt" # add this if the secret is configured with a CA
```

Optionally, the CA can be provided by a different secret:
```sh
kubectl create secret generic pg-tls-ca --from-file=ca.crt=ca.crt
```

Then configure the postgres resource with the TLS secret:

```yaml
apiVersion: "acid.zalan.do/v1"
kind: postgresql

metadata:
  name: acid-test-cluster
spec:
  tls:
    secretName: "pg-tls"    # this should hold tls.key and tls.crt
    caSecretName: "pg-tls-ca" # this should hold ca.crt
    caFile: "ca.crt" # add this if the secret is configured with a CA
```

Alternatively, it is also possible to use
[cert-manager](https://cert-manager.io/docs/) to generate these secrets.

Certificate rotation is handled in the Spilo image which checks every 5
minutes if the certificates have changed and reloads postgres accordingly.

### TLS certificates for connection pooler

By default, the pgBouncer image generates its own TLS certificate like Spilo.
When the `tls` section is specified in the manifest it will be used for the
connection pooler pod(s) as well. The security context options are hard coded
to `runAsUser: 100` and `runAsGroup: 101`. The `fsGroup` will be the same
like for Spilo.

As of now, the operator does not sync the pooler deployment automatically
which means that changes in the pod template are not caught. You need to
toggle `enableConnectionPooler` to set environment variables, volumes, secret
mounts and securityContext required for TLS support in the pooler pod.
