## Command-line options

The following command-line options are supported for the operator:

* **-kubeconfig**
  the path to the kubeconfig file. Usually named config, it contains
  authorization information as well as the URL of the Kubernetes master.

* **-outofcluster**
  run the operator on a client machine, as opposed to a within the cluster.
  When running in this mode, the operator cannot connect to databases inside
  the cluster, as well as call URLs of in-cluster objects (i.e. teams api
  server). Mostly useful for debugging, it also requires setting the
  `OPERATOR_NAMESPACE` environment variable for the operator own namespace.

* **-nodatabaseaccess**
  disable database access from the operator. Equivalent to the
  `enable_database_access` set to off and can be overridden by the
  aforementioned operator configuration option.

* **-noteamsapi**
  disable access to the teams API. Equivalent to the `enable_teams_api` set to
  off can can be overridden by the aforementioned operator configuration
  option.

In addition to that, standard [glog
flags](https://godoc.org/github.com/golang/glog) are also supported. For
instance, one may want to add `-alsologtostderr` and `-v=8` to debug the
operator REST calls.

## Environment variables

The following environment variables are accepted by the operator:

* **CONFIG_MAP_NAME**
  name of the config map where the operator should look for its configuration.
  Must be present.

* **OPERATOR_NAMESPACE**
  name of the namespace the operator runs it. Overrides autodetection by the
  operator itself.

* **WATCHED_NAMESPACE**
  the name of the namespace the operator watches. Special '*' character denotes
  all namespaces. Empty value defaults to the operator namespace. Overrides the
  `watched_namespace` operator parameter.

* **SCALYR_API_KEY**
  the value of the Scalyr API key to supply to the pods. Overrides the
  `scalyr_api_key` operator parameter.

* **CRD_READY_WAIT_TIMEOUT**
  defines the timeout for the complete postgres CRD creation. When not set
  default is 30s.

* **CRD_READY_WAIT_INTERVAL**
  defines the  interval between consecutive attempts waiting for the postgres
  CRD to be created. The default is 5s.
