# Reference

## Deprecated parameters

Parameters `useLoadBalancer` and `replicaLoadBalancer` in the PostgreSQL
manifest are deprecated. To retain compatibility with the old manifests they
take effect in the absense of new `enableMasterLoadBalancer` and
`enableReplicaLoadBalancer` parameters (that is, if either of the new ones is
present - all deprecated parameters are ignored). The operator configuration
parameter `enable_load_balancer` is ignored in all cases.

## Operator Configuration Parameters

* team_api_role_configuration - a map represented as
  *"key1:value1,key2:value2"* of configuration parameters applied to the roles
  fetched from the API. For instance, `team_api_role_configuration:
  log_statement:all,search_path:'public,"$user"'`. By default is set to
  *"log_statement:all"*. See
  [PostgreSQL documentation on ALTER ROLE .. SET](https://www.postgresql.org/docs/current/static/sql-alterrole.html)
  for to learn about the available options.
* protected_role_names - a list of role names that should be forbidden as the
  manifest, infrastructure and teams API roles. The default value is `admin`.
  Operator will also disallow superuser and replication roles to be redefined.
