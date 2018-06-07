Postgres Cluster Manifest
=========================

Individual Postgres clusters are described by the Kubernetes object called
cluster manifest that has the structure defined by the Postgres CRD (custom
resource definition). The following section describes the structure of the
manifest and the purpose of individual fields. You can take a look at the
examples of the
[minimal](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/minimal-postgres-manifest.yaml)
and the
[complete](https://github.com/zalando-incubator/postgres-operator/blob/master/manifests/complete-postgres-manifest.yaml)
cluster manifests.

Manifest structure
------------------

Postgres manifest is a YAML document. At the root both individual parameters and parameter groups can be defined that describe the Postgres cluster to be deployed. Parameter names are written in camelCase.

### Cluster metadata

Those parameters are grouped under the `metadata` root key.

* **name**
  the name of the cluster. Should start with the `teamId` following by a dash.
  Changing it after the cluster creation is not supported. Required field.

* **namespace**
  the namespace for cluster Kubernetes objects (i.e. pods, services, secrets).
  The operator should listen to that namespace in order to pick up the cluster
  object. Changing it after the cluster creation will result in deploying or
  updating a completely separate cluster in the target namespace. 

### Root parameters

* **teamId**
  text name of the team the cluster belongs to. The operator requires the
  cluster name to start with the team name and may reference to the team name
  in the DNS name template defined by the operator configuration. The team name
  is also used to populate initial users to te cluster when the
  `enable_teams_api` operator parameter is set. Changing it after the cluster
  creation is not supported. Required field.

* **dockerImage**
  optional custom docker image that overrides the **docker_image** operator
  parameter.

* **enableMasterReplicaLoadBalancer**
  boolean flag to override the operator defaults (set by the
  `enable_master_load_balancer` parameter) to define whether to enable the load
  balancer pointing to the Postgres primary. Optional.

* **enableReplicaLoadBalancer**
  boolean flag to override the operator defaults (set by the
  `enable_replica_load_balancer` parameter) to define whether to enable the
  load balancer pointing to the Postgres standby instances. Optional.

* **allowedSourceRanges**
  when one or more load balancers are enabled for the cluster, this parameter
  defines the comma-separated range of networks (in CIDR-notation). The
  corresponding load balancer will be accessible only to the networks defined
  by this parameter. Optional, when empty the corresponding load balancer
  service becomes inaccessible from outside of the Kubernetes cluster.

* **numberOfInstances**
  total number of  instances for a given cluster. The number here may be
  adjusted by the operator parameters `max_instances` and `min_instances`.
  Required field.

* **users**
  a dictionary of user names to user flags for the users that should be created
  in the cluster by the operator. User flags is itself a list, allowed flags
  are `SUPERUSER` `REPLICATION` `INHERIT`, `LOGIN`, `NOLOGIN`, `CREATEROLE`,
  `CREATEDB`, `REPLICATION`, `BYPASSURL`. A login user is created by default,
  unless NOLOGIN is specified, in which case the operator creates a role. One
  can specify empty flags by providing a JSON empty array '*[]*'. Optional.

* **databases**
  a dictionary of database names to database owners for the databases that
  should be created by the operator. The owner users should already exist on
  the cluster (a role from the `user` dictionary would work). Optional.

### Postgres parameters

Those parameters are grouped under the `postgresql` root key.

* **version**
  postgres major version the cluster should run. Looks at the [Spilo
  project](https://github.com/zalando/spilo) for the list of supported
  versions. Changing the cluster version once the cluster has been bootstrapped
  is not supported. Required field.

* **parameters**
  a dictionary of postgres parameter names and values to apply to the resulting
  cluster. Optional (reasonable defaults for parameters like work_mem or
  max_connections are automatically set by Spilo).

### Patroni parameters

Those parameters are grouped under the `patroni` root key. See the [patroni
documentation](https://patroni.readthedocs.io/en/latest/SETTINGS.html) for the
explanation of `ttl` and `loop_wait` parameters.

* **initdb**
  a map of key-value pairs describing initdb parameters. For `data-checksum`,
  `debug`, `no-locale`, `noclean`, `nosync` and `sync-only` parameters use
  `true` as the value if you want to set them. Changes to this option have no
  effect once the cluster is initialized. Optional. 

* **pg_hba**
  list of custom `pg_hba` lines to replace default ones. Note that the default
  ones include `hostsll all +pamrole all pam`, where pamrole is the name of the
  pamrole; any custom lines should include the pam line to avoid breaking pam
  authentication. Optional.

* **ttl**
  patroni `ttl` parameter value, optional. The default is set by the Spilo
  docker image.

* **loop_wait**
  patroni `loop_wait` parameter value, optional. The default is set by the
  Spilo docker image.

* **retry_timeout**
  patroni `retry_timeout` parameter value, optional. The default is set by the
  Spilo docker image.

* **maximum_lag_on_failover**
  patroni `maximum_lag_on_failover` parameter value, optional. The default is
  set by the Spilo docker image.

### Postgres container resources

Those parameters define CPU and memory requests and limits for the postgres
container. They are grouped under the `resources` root key. There are two
subgroups, `requests` and `limits`.

#### Requests

CPU and memory requests for the postgres container.

* **cpu**
  CPU requests for the Postgres container. Optional, overrides the `default_cpu_requests` operator configuration parameter.

* **memory**
  memory requests for the Postgres container. Optional, overrides the `default_memory_request` operator configuration parameter.

#### Limits

CPU and memory limits for the postgres container.

* **cpu**
  CPU limits for the Postgres container. Optional, overrides the `default_cpu_limits` operator configuration parameter.

* **memory**
  memory limits for the Postgres container. Optional, overrides the `default_memory_limits` operator configuration parameter.

### Cluster cloning

Those parameters define the source clone to clone the current one from. They
have no effect once the cluster is already up and running.They are grouped
under the `clone` root key.

* **cluster**
  name of the cluster to clone from. Translated to either the service name or the key inside the S3 bucket containing base backups.

* **uid**
  Kubernetes UID of the cluster to clone from. Since name is not a unique
  identifier of the cluster, we use UID in the S3 bucket name in order to
  guarantee uniqness. Has no effect when cloning from the running cluster
  
* **timestamp**
  timestamp to stop replaying changes of the original cluster for the
  point-in-time recovery. The operator always configures non-inclusive recovery
  target, stopping right before the given timestamp. When the timestamp is
  included the operator will not consider cloning from the live cluster, even
  if it is running, and instead goes to S3. 
