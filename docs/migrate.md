<h1>Migrate from v1 to v2</h1>

Version 2.0 changes some default settings and removes deprecated fields. Please read the following sections before upgrading the Postgres Operator deployment.

## K8s Endpoints are deprecated

If your current operator v1.x deployment is relying on K8s endpoints (the default setup) for Patroni to manage the HA state you have to start planning to switch to configmaps, because endpoints are deprecated from K8s 1.33 onwards. The default of the corresponding parameter `kubernetes_use_configmaps` is changing to `true` with v2.0 of the operator. This means you have to explicity set it to `false` in your configuration before you start the upgrade.

We explicitly warn you to go straight to configmap-based HA management with database clusters that use replicas, because there's is a danger to run into split-brain scenarios during the rolling update of pods when there exists a leader endpoint and leader config map at the same time. To play it safe, here is what you should do - before or after the Postgres Operator upgrade:

1. Scale-in all your database clusters to only one primary instance. This can be done by changing the global config options `max_instances` and `min_instances` to `1`. If you have allowed users to ignore globally defined instance limits by configuring an `ignore_instance_limits_annotation_key`, remove it for now.

2. Wait for all clusters to be healthy and change the `kubernetes_use_configmaps` setting to `true`. This will trigger the replacement of the primary pod of all clusters and cause downtime for as long as the pods are rescheduled and start up.

3. Check again that all clusters are healthy with configmaps created. There should be three for each cluster called like cluster name with suffixes `-config`, `-failover` and `-leader`. Now, revert the changes from step 1 and scale-out the to number of instances set in the manifests.

4. The orphaned endpoints, which use the same names like the new configmaps, have to be deleted by you or your K8s garbage collection.


## Dropped manifest fields

We removed some deprecated fields from the Postgresql CRD. Please, make sure that you do not specify them in any of your cluster manifests.  If you do, switch to the listed alternative:

| Removed field in v2 | Alternative |
| --- | --- |
| init_containers | initContainers |
| pod_priority_class_name | podPriorityClassName |
| replicaLoadBalancer | enableReplicaLoadBalancer|
| useLoadBalancer | enableMasterLoadBalancer |
