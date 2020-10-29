import json
import unittest
import time
import timeout_decorator
import subprocess
import warnings
import os
import yaml

from datetime import datetime
from kubernetes import client, config


def to_selector(labels):
    return ",".join(["=".join(l) for l in labels.items()])


def clean_list(values):
    # value is not stripped bytes, strip and convert to a string
    clean = lambda v: v.strip().decode()
    notNone = lambda v: v

    return list(filter(notNone, map(clean, values)))


class EndToEndTestCase(unittest.TestCase):
    '''
    Test interaction of the operator with multiple K8s components.
    '''

    # `kind` pods may stuck in the `Terminating` phase for a few minutes; hence high test timeout
    TEST_TIMEOUT_SEC = 600

    @classmethod
    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def setUpClass(cls):
        '''
        Deploy operator to a "kind" cluster created by run.sh using examples from /manifests.
        This operator deployment is to be shared among all tests.

        run.sh deletes the 'kind' cluster after successful run along with all operator-related entities.
        In the case of test failure the cluster will stay to enable manual examination;
        next invocation of "make test" will re-create it.
        '''
        print("Test Setup being executed")

        # set a single K8s wrapper for all tests
        k8s = cls.k8s = K8s()

        # remove existing local storage class and create hostpath class
        k8s.api.storage_v1_api.delete_storage_class("standard")

        # operator deploys pod service account there on start up
        # needed for test_multi_namespace_support()
        cls.namespace = "test"
        v1_namespace = client.V1Namespace(metadata=client.V1ObjectMeta(name=cls.namespace))
        k8s.api.core_v1.create_namespace(v1_namespace)

        # submit the most recent operator image built on the Docker host
        with open("manifests/postgres-operator.yaml", 'r+') as f:
            operator_deployment = yaml.safe_load(f)
            operator_deployment["spec"]["template"]["spec"]["containers"][0]["image"] = os.environ['OPERATOR_IMAGE']
            yaml.dump(operator_deployment, f, Dumper=yaml.Dumper)

        for filename in ["operator-service-account-rbac.yaml",
                         "configmap.yaml",
                         "postgres-operator.yaml",
                         "infrastructure-roles.yaml",
                         "infrastructure-roles-new.yaml",
                         "e2e-storage-class.yaml"]:
            result = k8s.create_with_kubectl("manifests/" + filename)
            print("stdout: {}, stderr: {}".format(result.stdout, result.stderr))

        k8s.wait_for_operator_pod_start()

        actual_operator_image = k8s.api.core_v1.list_namespaced_pod(
            'default', label_selector='name=postgres-operator').items[0].spec.containers[0].image
        print("Tested operator image: {}".format(actual_operator_image))  # shows up after tests finish

        result = k8s.create_with_kubectl("manifests/minimal-postgres-manifest.yaml")
        print('stdout: {}, stderr: {}'.format(result.stdout, result.stderr))
        try:
            k8s.wait_for_pod_start('spilo-role=master')
            k8s.wait_for_pod_start('spilo-role=replica')
        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_enable_disable_connection_pooler(self):
        '''
        For a database without connection pooler, then turns it on, scale up,
        turn off and on again. Test with different ways of doing this (via
        enableConnectionPooler or connectionPooler configuration section). At
        the end turn connection pooler off to not interfere with other tests.
        '''
        k8s = self.k8s
        service_labels = {
            'cluster-name': 'acid-minimal-cluster',
        }
        pod_labels = dict({
            'connection-pooler': 'acid-minimal-cluster-pooler',
        })

        pod_selector = to_selector(pod_labels)
        service_selector = to_selector(service_labels)

        try:
            # enable connection pooler
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                'acid.zalan.do', 'v1', 'default',
                'postgresqls', 'acid-minimal-cluster',
                {
                    'spec': {
                        'enableConnectionPooler': True,
                    }
                })
            k8s.wait_for_pod_start(pod_selector)

            pods = k8s.api.core_v1.list_namespaced_pod(
                'default', label_selector=pod_selector
            ).items

            self.assertTrue(pods, 'No connection pooler pods')

            k8s.wait_for_service(service_selector)
            services = k8s.api.core_v1.list_namespaced_service(
                'default', label_selector=service_selector
            ).items
            services = [
                s for s in services
                if s.metadata.name.endswith('pooler')
            ]

            self.assertTrue(services, 'No connection pooler service')

            # scale up connection pooler deployment
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                'acid.zalan.do', 'v1', 'default',
                'postgresqls', 'acid-minimal-cluster',
                {
                    'spec': {
                        'connectionPooler': {
                            'numberOfInstances': 2,
                        },
                    }
                })

            k8s.wait_for_running_pods(pod_selector, 2)

            # Verify that all the databases have pooler schema installed.
            # Do this via psql, since otherwise we need to deal with
            # credentials.
            dbList = []

            leader = k8s.get_cluster_leader_pod('acid-minimal-cluster')
            dbListQuery = "select datname from pg_database"
            schemasQuery = """
                select schema_name
                from information_schema.schemata
                where schema_name = 'pooler'
            """
            exec_query = r"psql -tAq -c \"{}\" -d {}"

            if leader:
                try:
                    q = exec_query.format(dbListQuery, "postgres")
                    q = "su postgres -c \"{}\"".format(q)
                    print('Get databases: {}'.format(q))
                    result = k8s.exec_with_kubectl(leader.metadata.name, q)
                    dbList = clean_list(result.stdout.split(b'\n'))
                    print('dbList: {}, stdout: {}, stderr {}'.format(
                        dbList, result.stdout, result.stderr
                    ))
                except Exception as ex:
                    print('Could not get databases: {}'.format(ex))
                    print('Stdout: {}'.format(result.stdout))
                    print('Stderr: {}'.format(result.stderr))

                for db in dbList:
                    if db in ('template0', 'template1'):
                        continue

                    schemas = []
                    try:
                        q = exec_query.format(schemasQuery, db)
                        q = "su postgres -c \"{}\"".format(q)
                        print('Get schemas: {}'.format(q))
                        result = k8s.exec_with_kubectl(leader.metadata.name, q)
                        schemas = clean_list(result.stdout.split(b'\n'))
                        print('schemas: {}, stdout: {}, stderr {}'.format(
                            schemas, result.stdout, result.stderr
                        ))
                    except Exception as ex:
                        print('Could not get databases: {}'.format(ex))
                        print('Stdout: {}'.format(result.stdout))
                        print('Stderr: {}'.format(result.stderr))

                    self.assertNotEqual(len(schemas), 0)
            else:
                print('Could not find leader pod')

            # turn it off, keeping configuration section
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                'acid.zalan.do', 'v1', 'default',
                'postgresqls', 'acid-minimal-cluster',
                {
                    'spec': {
                        'enableConnectionPooler': False,
                    }
                })
            k8s.wait_for_pods_to_stop(pod_selector)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_enable_load_balancer(self):
        '''
        Test if services are updated when enabling/disabling load balancers
        '''

        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        try:
            # enable load balancer services
            pg_patch_enable_lbs = {
                "spec": {
                    "enableMasterLoadBalancer": True,
                    "enableReplicaLoadBalancer": True
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_enable_lbs)
            # wait for service recreation
            time.sleep(60)

            master_svc_type = k8s.get_service_type(cluster_label + ',spilo-role=master')
            self.assertEqual(master_svc_type, 'LoadBalancer',
                             "Expected LoadBalancer service type for master, found {}".format(master_svc_type))

            repl_svc_type = k8s.get_service_type(cluster_label + ',spilo-role=replica')
            self.assertEqual(repl_svc_type, 'LoadBalancer',
                             "Expected LoadBalancer service type for replica, found {}".format(repl_svc_type))

            # disable load balancer services again
            pg_patch_disable_lbs = {
                "spec": {
                    "enableMasterLoadBalancer": False,
                    "enableReplicaLoadBalancer": False
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_disable_lbs)
            # wait for service recreation
            time.sleep(60)

            master_svc_type = k8s.get_service_type(cluster_label + ',spilo-role=master')
            self.assertEqual(master_svc_type, 'ClusterIP',
                             "Expected ClusterIP service type for master, found {}".format(master_svc_type))

            repl_svc_type = k8s.get_service_type(cluster_label + ',spilo-role=replica')
            self.assertEqual(repl_svc_type, 'ClusterIP',
                             "Expected ClusterIP service type for replica, found {}".format(repl_svc_type))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_infrastructure_roles(self):
        '''
            Test using external secrets for infrastructure roles
        '''
        k8s = self.k8s
        # update infrastructure roles description
        secret_name = "postgresql-infrastructure-roles"
        roles = "secretname: postgresql-infrastructure-roles-new, \
            userkey: user, rolekey: memberof, passwordkey: password, defaultrolevalue: robot_zmon"
        patch_infrastructure_roles = {
            "data": {
                "infrastructure_roles_secret_name": secret_name,
                "infrastructure_roles_secrets": roles,
            },
        }
        k8s.update_config(patch_infrastructure_roles)

        # wait a little before proceeding
        time.sleep(30)

        try:
            # check that new roles are represented in the config by requesting the
            # operator configuration via API
            operator_pod = k8s.get_operator_pod()
            get_config_cmd = "wget --quiet -O - localhost:8080/config"
            result = k8s.exec_with_kubectl(
                operator_pod.metadata.name,
                get_config_cmd,
            )
            roles_dict = (json.loads(result.stdout)
                          .get("controller", {})
                          .get("InfrastructureRoles"))

            self.assertTrue("robot_zmon_acid_monitoring_new" in roles_dict)
            role = roles_dict["robot_zmon_acid_monitoring_new"]
            role.pop("Password", None)
            self.assertDictEqual(role, {
                "Name": "robot_zmon_acid_monitoring_new",
                "Flags": None,
                "MemberOf": ["robot_zmon"],
                "Parameters": None,
                "AdminRole": "",
                "Origin": 2,
            })

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_lazy_spilo_upgrade(self):
        '''
        Test lazy upgrade for the Spilo image: operator changes a stateful set but lets pods run with the old image
        until they are recreated for reasons other than operator's activity. That works because the operator configures
        stateful sets to use "onDelete" pod update policy.

        The test covers:
        1) enabling lazy upgrade in existing operator deployment
        2) forcing the normal rolling upgrade by changing the operator configmap and restarting its pod
        '''

        k8s = self.k8s

        # update docker image in config and enable the lazy upgrade
        conf_image = "registry.opensource.zalan.do/acid/spilo-cdp-12:1.6-p114"
        patch_lazy_spilo_upgrade = {
            "data": {
                "docker_image": conf_image,
                "enable_lazy_spilo_upgrade": "true"
            }
        }
        k8s.update_config(patch_lazy_spilo_upgrade)

        pod0 = 'acid-minimal-cluster-0'
        pod1 = 'acid-minimal-cluster-1'

        try:
            # restart the pod to get a container with the new image
            k8s.api.core_v1.delete_namespaced_pod(pod0, 'default')
            time.sleep(60)

            # lazy update works if the restarted pod and older pods run different Spilo versions
            new_image = k8s.get_effective_pod_image(pod0)
            old_image = k8s.get_effective_pod_image(pod1)
            self.assertNotEqual(new_image, old_image,
                                "Lazy updated failed: pods have the same image {}".format(new_image))

            # sanity check
            assert_msg = "Image {} of a new pod differs from {} in operator conf".format(new_image, conf_image)
            self.assertEqual(new_image, conf_image, assert_msg)

            # clean up
            unpatch_lazy_spilo_upgrade = {
                "data": {
                    "enable_lazy_spilo_upgrade": "false",
                }
            }
            k8s.update_config(unpatch_lazy_spilo_upgrade)

            # at this point operator will complete the normal rolling upgrade
            # so we additonally test if disabling the lazy upgrade - forcing the normal rolling upgrade - works

            # XXX there is no easy way to wait until the end of Sync()
            time.sleep(60)

            image0 = k8s.get_effective_pod_image(pod0)
            image1 = k8s.get_effective_pod_image(pod1)

            assert_msg = "Disabling lazy upgrade failed: pods still have different \
                images {} and {}".format(image0, image1)
            self.assertEqual(image0, image1, assert_msg)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_logical_backup_cron_job(self):
        '''
        Ensure we can (a) create the cron job at user request for a specific PG cluster
                      (b) update the cluster-wide image for the logical backup pod
                      (c) delete the job at user request

        Limitations:
        (a) Does not run the actual batch job because there is no S3 mock to upload backups to
        (b) Assumes 'acid-minimal-cluster' exists as defined in setUp
        '''

        k8s = self.k8s

        # create the cron job
        schedule = "7 7 7 7 *"
        pg_patch_enable_backup = {
            "spec": {
                "enableLogicalBackup": True,
                "logicalBackupSchedule": schedule
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_enable_backup)

        try:
            k8s.wait_for_logical_backup_job_creation()

            jobs = k8s.get_logical_backup_job().items
            self.assertEqual(1, len(jobs), "Expected 1 logical backup job, found {}".format(len(jobs)))

            job = jobs[0]
            self.assertEqual(job.metadata.name, "logical-backup-acid-minimal-cluster",
                             "Expected job name {}, found {}"
                             .format("logical-backup-acid-minimal-cluster", job.metadata.name))
            self.assertEqual(job.spec.schedule, schedule,
                             "Expected {} schedule, found {}"
                             .format(schedule, job.spec.schedule))

            # update the cluster-wide image of the logical backup pod
            image = "test-image-name"
            patch_logical_backup_image = {
                "data": {
                    "logical_backup_docker_image": image,
                }
            }
            k8s.update_config(patch_logical_backup_image)

            jobs = k8s.get_logical_backup_job().items
            actual_image = jobs[0].spec.job_template.spec.template.spec.containers[0].image
            self.assertEqual(actual_image, image,
                             "Expected job image {}, found {}".format(image, actual_image))

            # delete the logical backup cron job
            pg_patch_disable_backup = {
                "spec": {
                    "enableLogicalBackup": False,
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_disable_backup)
            k8s.wait_for_logical_backup_job_deletion()
            jobs = k8s.get_logical_backup_job().items
            self.assertEqual(0, len(jobs),
                             "Expected 0 logical backup jobs, found {}".format(len(jobs)))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_min_resource_limits(self):
        '''
        Lower resource limits below configured minimum and let operator fix it
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'
        labels = 'spilo-role=master,' + cluster_label
        _, failover_targets = k8s.get_pg_nodes(cluster_label)

        # configure minimum boundaries for CPU and memory limits
        minCPULimit = '500m'
        minMemoryLimit = '500Mi'
        patch_min_resource_limits = {
            "data": {
                "min_cpu_limit": minCPULimit,
                "min_memory_limit": minMemoryLimit
            }
        }
        k8s.update_config(patch_min_resource_limits)

        # lower resource limits below minimum
        pg_patch_resources = {
            "spec": {
                "resources": {
                    "requests": {
                        "cpu": "10m",
                        "memory": "50Mi"
                    },
                    "limits": {
                        "cpu": "200m",
                        "memory": "200Mi"
                    }
                }
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_resources)

        try:
            k8s.wait_for_pod_failover(failover_targets, labels)
            k8s.wait_for_pod_start('spilo-role=replica')

            pods = k8s.api.core_v1.list_namespaced_pod(
                'default', label_selector=labels).items
            self.assert_master_is_unique()
            masterPod = pods[0]

            self.assertEqual(masterPod.spec.containers[0].resources.limits['cpu'], minCPULimit,
                             "Expected CPU limit {}, found {}"
                             .format(minCPULimit, masterPod.spec.containers[0].resources.limits['cpu']))
            self.assertEqual(masterPod.spec.containers[0].resources.limits['memory'], minMemoryLimit,
                             "Expected memory limit {}, found {}"
                             .format(minMemoryLimit, masterPod.spec.containers[0].resources.limits['memory']))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_multi_namespace_support(self):
        '''
        Create a customized Postgres cluster in a non-default namespace.
        '''
        k8s = self.k8s

        with open("manifests/complete-postgres-manifest.yaml", 'r+') as f:
            pg_manifest = yaml.safe_load(f)
            pg_manifest["metadata"]["namespace"] = self.namespace
            yaml.dump(pg_manifest, f, Dumper=yaml.Dumper)

        try:
            k8s.create_with_kubectl("manifests/complete-postgres-manifest.yaml")
            k8s.wait_for_pod_start("spilo-role=master", self.namespace)
            self.assert_master_is_unique(self.namespace, "acid-test-cluster")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_node_readiness_label(self):
        '''
           Remove node readiness label from master node. This must cause a failover.
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'
        readiness_label = 'lifecycle-status'
        readiness_value = 'ready'

        try:
            # get nodes of master and replica(s) (expected target of new master)
            current_master_node, current_replica_nodes = k8s.get_pg_nodes(cluster_label)
            num_replicas = len(current_replica_nodes)
            failover_targets = self.get_failover_targets(current_master_node, current_replica_nodes)

            # add node_readiness_label to potential failover nodes
            patch_readiness_label = {
                "metadata": {
                    "labels": {
                        readiness_label: readiness_value
                    }
                }
            }
            for failover_target in failover_targets:
                k8s.api.core_v1.patch_node(failover_target, patch_readiness_label)

            # define node_readiness_label in config map which should trigger a failover of the master
            patch_readiness_label_config = {
                "data": {
                    "node_readiness_label": readiness_label + ':' + readiness_value,
                }
            }
            k8s.update_config(patch_readiness_label_config)
            new_master_node, new_replica_nodes = self.assert_failover(
                current_master_node, num_replicas, failover_targets, cluster_label)

            # patch also node where master ran before
            k8s.api.core_v1.patch_node(current_master_node, patch_readiness_label)

            # wait a little before proceeding with the pod distribution test
            time.sleep(30)

            # toggle pod anti affinity to move replica away from master node
            self.assert_distributed_pods(new_master_node, new_replica_nodes, cluster_label)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_scaling(self):
        '''
           Scale up from 2 to 3 and back to 2 pods by updating the Postgres manifest at runtime.
        '''
        k8s = self.k8s
        labels = "application=spilo,cluster-name=acid-minimal-cluster"

        try:
            k8s.wait_for_pg_to_scale(3)
            self.assertEqual(3, k8s.count_pods_with_label(labels))
            self.assert_master_is_unique()

            k8s.wait_for_pg_to_scale(2)
            self.assertEqual(2, k8s.count_pods_with_label(labels))
            self.assert_master_is_unique()

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_service_annotations(self):
        '''
        Create a Postgres cluster with service annotations and check them.
        '''
        k8s = self.k8s
        patch_custom_service_annotations = {
            "data": {
                "custom_service_annotations": "foo:bar",
            }
        }
        k8s.update_config(patch_custom_service_annotations)

        try:
            pg_patch_custom_annotations = {
                "spec": {
                    "serviceAnnotations": {
                        "annotation.key": "value",
                        "foo": "bar",
                    }
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_custom_annotations)

            # wait a little before proceeding
            time.sleep(30)
            annotations = {
                "annotation.key": "value",
                "foo": "bar",
            }
            self.assertTrue(k8s.check_service_annotations(
                "cluster-name=acid-minimal-cluster,spilo-role=master", annotations))
            self.assertTrue(k8s.check_service_annotations(
                "cluster-name=acid-minimal-cluster,spilo-role=replica", annotations))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # clean up
        unpatch_custom_service_annotations = {
            "data": {
                "custom_service_annotations": "",
            }
        }
        k8s.update_config(unpatch_custom_service_annotations)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_statefulset_annotation_propagation(self):
        '''
           Inject annotation to Postgresql CRD and check it's propagation to stateful set
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        patch_sset_propagate_annotations = {
            "data": {
                "downscaler_annotations": "deployment-time,downscaler/*",
            }
        }
        k8s.update_config(patch_sset_propagate_annotations)

        try:
            pg_crd_annotations = {
                "metadata": {
                    "annotations": {
                        "deployment-time": "2020-04-30 12:00:00",
                        "downscaler/downtime_replicas": "0",
                    },
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_crd_annotations)

            # wait a little before proceeding
            time.sleep(60)
            annotations = {
                "deployment-time": "2020-04-30 12:00:00",
                "downscaler/downtime_replicas": "0",
            }
            self.assertTrue(k8s.check_statefulset_annotations(cluster_label, annotations))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_taint_based_eviction(self):
        '''
           Add taint "postgres=:NoExecute" to node with master. This must cause a failover.
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # get nodes of master and replica(s) (expected target of new master)
        current_master_node, current_replica_nodes = k8s.get_pg_nodes(cluster_label)
        num_replicas = len(current_replica_nodes)
        failover_targets = self.get_failover_targets(current_master_node, current_replica_nodes)

        # taint node with postgres=:NoExecute to force failover
        body = {
            "spec": {
                "taints": [
                    {
                        "effect": "NoExecute",
                        "key": "postgres"
                    }
                ]
            }
        }

        try:
            # patch node and test if master is failing over to one of the expected nodes
            k8s.api.core_v1.patch_node(current_master_node, body)
            new_master_node, new_replica_nodes = self.assert_failover(
                current_master_node, num_replicas, failover_targets, cluster_label)

            # add toleration to pods
            patch_toleration_config = {
                "data": {
                    "toleration": "key:postgres,operator:Exists,effect:NoExecute"
                }
            }
            k8s.update_config(patch_toleration_config)

            # wait a little before proceeding with the pod distribution test
            time.sleep(30)

            # toggle pod anti affinity to move replica away from master node
            self.assert_distributed_pods(new_master_node, new_replica_nodes, cluster_label)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_x_cluster_deletion(self):
        '''
           Test deletion with configured protection
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # configure delete protection
        patch_delete_annotations = {
            "data": {
                "delete_annotation_date_key": "delete-date",
                "delete_annotation_name_key": "delete-clustername"
            }
        }
        k8s.update_config(patch_delete_annotations)

        try:
            # this delete attempt should be omitted because of missing annotations
            k8s.api.custom_objects_api.delete_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster")

            # check that pods and services are still there
            k8s.wait_for_running_pods(cluster_label, 2)
            k8s.wait_for_service(cluster_label)

            # recreate Postgres cluster resource
            k8s.create_with_kubectl("manifests/minimal-postgres-manifest.yaml")

            # wait a little before proceeding
            time.sleep(10)

            # add annotations to manifest
            delete_date = datetime.today().strftime('%Y-%m-%d')
            pg_patch_delete_annotations = {
                "metadata": {
                    "annotations": {
                        "delete-date": delete_date,
                        "delete-clustername": "acid-minimal-cluster",
                    }
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_delete_annotations)

            # wait a little before proceeding
            time.sleep(10)
            k8s.wait_for_running_pods(cluster_label, 2)
            k8s.wait_for_service(cluster_label)

            # now delete process should be triggered
            k8s.api.custom_objects_api.delete_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster")

            # wait until cluster is deleted
            time.sleep(120)

            # check if everything has been deleted
            self.assertEqual(0, k8s.count_pods_with_label(cluster_label))
            self.assertEqual(0, k8s.count_services_with_label(cluster_label))
            self.assertEqual(0, k8s.count_endpoints_with_label(cluster_label))
            self.assertEqual(0, k8s.count_statefulsets_with_label(cluster_label))
            self.assertEqual(0, k8s.count_deployments_with_label(cluster_label))
            self.assertEqual(0, k8s.count_pdbs_with_label(cluster_label))
            self.assertEqual(0, k8s.count_secrets_with_label(cluster_label))

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    def get_failover_targets(self, master_node, replica_nodes):
        '''
           If all pods live on the same node, failover will happen to other worker(s)
        '''
        k8s = self.k8s
        k8s_master_exclusion = 'kubernetes.io/hostname!=postgres-operator-e2e-tests-control-plane'

        failover_targets = [x for x in replica_nodes if x != master_node]
        if len(failover_targets) == 0:
            nodes = k8s.api.core_v1.list_node(label_selector=k8s_master_exclusion)
            for n in nodes.items:
                if n.metadata.name != master_node:
                    failover_targets.append(n.metadata.name)

        return failover_targets

    def assert_failover(self, current_master_node, num_replicas, failover_targets, cluster_label):
        '''
           Check if master is failing over. The replica should move first to be the switchover target
        '''
        k8s = self.k8s
        k8s.wait_for_pod_failover(failover_targets, 'spilo-role=master,' + cluster_label)
        k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

        new_master_node, new_replica_nodes = k8s.get_pg_nodes(cluster_label)
        self.assertNotEqual(current_master_node, new_master_node,
                            "Master on {} did not fail over to one of {}".format(current_master_node, failover_targets))
        self.assertEqual(num_replicas, len(new_replica_nodes),
                         "Expected {} replicas, found {}".format(num_replicas, len(new_replica_nodes)))
        self.assert_master_is_unique()

        return new_master_node, new_replica_nodes

    def assert_master_is_unique(self, namespace='default', clusterName="acid-minimal-cluster"):
        '''
           Check that there is a single pod in the k8s cluster with the label "spilo-role=master"
           To be called manually after operations that affect pods
        '''
        k8s = self.k8s
        labels = 'spilo-role=master,cluster-name=' + clusterName

        num_of_master_pods = k8s.count_pods_with_label(labels, namespace)
        self.assertEqual(num_of_master_pods, 1, "Expected 1 master pod, found {}".format(num_of_master_pods))

    def assert_distributed_pods(self, master_node, replica_nodes, cluster_label):
        '''
           Other tests can lead to the situation that master and replica are on the same node.
           Toggle pod anti affinty to distribute pods accross nodes (replica in particular).
        '''
        k8s = self.k8s
        failover_targets = self.get_failover_targets(master_node, replica_nodes)

        # enable pod anti affintiy in config map which should trigger movement of replica
        patch_enable_antiaffinity = {
            "data": {
                "enable_pod_antiaffinity": "true"
            }
        }
        k8s.update_config(patch_enable_antiaffinity)
        self.assert_failover(master_node, len(replica_nodes), failover_targets, cluster_label)

        # now disable pod anti affintiy again which will cause yet another failover
        patch_disable_antiaffinity = {
            "data": {
                "enable_pod_antiaffinity": "false"
            }
        }
        k8s.update_config(patch_disable_antiaffinity)
        k8s.wait_for_pod_start('spilo-role=master')
        k8s.wait_for_pod_start('spilo-role=replica')


class K8sApi:

    def __init__(self):

        # https://github.com/kubernetes-client/python/issues/309
        warnings.simplefilter("ignore", ResourceWarning)

        self.config = config.load_kube_config()
        self.k8s_client = client.ApiClient()

        self.core_v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()
        self.batch_v1_beta1 = client.BatchV1beta1Api()
        self.custom_objects_api = client.CustomObjectsApi()
        self.policy_v1_beta1 = client.PolicyV1beta1Api()
        self.storage_v1_api = client.StorageV1Api()


class K8s:
    '''
    Wraps around K8s api client and helper methods.
    '''

    RETRY_TIMEOUT_SEC = 10

    def __init__(self):
        self.api = K8sApi()

    def get_pg_nodes(self, pg_cluster_name, namespace='default'):
        master_pod_node = ''
        replica_pod_nodes = []
        podsList = self.api.core_v1.list_namespaced_pod(namespace, label_selector=pg_cluster_name)
        for pod in podsList.items:
            if pod.metadata.labels.get('spilo-role') == 'master':
                master_pod_node = pod.spec.node_name
            elif pod.metadata.labels.get('spilo-role') == 'replica':
                replica_pod_nodes.append(pod.spec.node_name)

        return master_pod_node, replica_pod_nodes

    def get_cluster_leader_pod(self, pg_cluster_name, namespace='default'):
        labels = {
            'application': 'spilo',
            'cluster-name': pg_cluster_name,
            'spilo-role': 'master',
        }

        pods = self.api.core_v1.list_namespaced_pod(
                namespace, label_selector=to_selector(labels)).items

        if pods:
            return pods[0]

    def wait_for_operator_pod_start(self):
        self. wait_for_pod_start("name=postgres-operator")
        # HACK operator must register CRD and/or Sync existing PG clusters after start up
        # for local execution ~ 10 seconds suffices
        time.sleep(60)

    def get_operator_pod(self):
        pods = self.api.core_v1.list_namespaced_pod(
            'default', label_selector='name=postgres-operator'
        ).items

        if pods:
            return pods[0]

        return None

    def get_operator_log(self):
        operator_pod = self.get_operator_pod()
        pod_name = operator_pod.metadata.name
        return self.api.core_v1.read_namespaced_pod_log(
            name=pod_name,
            namespace='default'
        )

    def wait_for_pod_start(self, pod_labels, namespace='default'):
        pod_phase = 'No pod running'
        while pod_phase != 'Running':
            pods = self.api.core_v1.list_namespaced_pod(namespace, label_selector=pod_labels).items
            if pods:
                pod_phase = pods[0].status.phase

            time.sleep(self.RETRY_TIMEOUT_SEC)

    def get_service_type(self, svc_labels, namespace='default'):
        svc_type = ''
        svcs = self.api.core_v1.list_namespaced_service(namespace, label_selector=svc_labels, limit=1).items
        for svc in svcs:
            svc_type = svc.spec.type
        return svc_type

    def check_service_annotations(self, svc_labels, annotations, namespace='default'):
        svcs = self.api.core_v1.list_namespaced_service(namespace, label_selector=svc_labels, limit=1).items
        for svc in svcs:
            for key, value in annotations.items():
                if key not in svc.metadata.annotations or svc.metadata.annotations[key] != value:
                    print("Expected key {} not found in annotations {}".format(key, svc.metadata.annotation))
                    return False
        return True

    def check_statefulset_annotations(self, sset_labels, annotations, namespace='default'):
        ssets = self.api.apps_v1.list_namespaced_stateful_set(namespace, label_selector=sset_labels, limit=1).items
        for sset in ssets:
            for key, value in annotations.items():
                if key not in sset.metadata.annotations or sset.metadata.annotations[key] != value:
                    print("Expected key {} not found in annotations {}".format(key, sset.metadata.annotation))
                    return False
        return True

    def wait_for_pg_to_scale(self, number_of_instances, namespace='default'):

        body = {
            "spec": {
                "numberOfInstances": number_of_instances
            }
        }
        _ = self.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", namespace, "postgresqls", "acid-minimal-cluster", body)

        labels = 'application=spilo,cluster-name=acid-minimal-cluster'
        while self.count_pods_with_label(labels) != number_of_instances:
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_running_pods(self, labels, number, namespace=''):
        while self.count_pods_with_label(labels) != number:
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_pods_to_stop(self, labels, namespace=''):
        while self.count_pods_with_label(labels) != 0:
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_service(self, labels, namespace='default'):
        def get_services():
            return self.api.core_v1.list_namespaced_service(
                namespace, label_selector=labels
            ).items

        while not get_services():
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def count_pods_with_label(self, labels, namespace='default'):
        return len(self.api.core_v1.list_namespaced_pod(namespace, label_selector=labels).items)

    def count_services_with_label(self, labels, namespace='default'):
        return len(self.api.core_v1.list_namespaced_service(namespace, label_selector=labels).items)

    def count_endpoints_with_label(self, labels, namespace='default'):
        return len(self.api.core_v1.list_namespaced_endpoints(namespace, label_selector=labels).items)

    def count_secrets_with_label(self, labels, namespace='default'):
        return len(self.api.core_v1.list_namespaced_secret(namespace, label_selector=labels).items)

    def count_statefulsets_with_label(self, labels, namespace='default'):
        return len(self.api.apps_v1.list_namespaced_stateful_set(namespace, label_selector=labels).items)

    def count_deployments_with_label(self, labels, namespace='default'):
        return len(self.api.apps_v1.list_namespaced_deployment(namespace, label_selector=labels).items)

    def count_pdbs_with_label(self, labels, namespace='default'):
        return len(self.api.policy_v1_beta1.list_namespaced_pod_disruption_budget(
            namespace, label_selector=labels).items)

    def wait_for_pod_failover(self, failover_targets, labels, namespace='default'):
        pod_phase = 'Failing over'
        new_pod_node = ''

        while (pod_phase != 'Running') or (new_pod_node not in failover_targets):
            pods = self.api.core_v1.list_namespaced_pod(namespace, label_selector=labels).items
            if pods:
                new_pod_node = pods[0].spec.node_name
                pod_phase = pods[0].status.phase
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def get_logical_backup_job(self, namespace='default'):
        return self.api.batch_v1_beta1.list_namespaced_cron_job(namespace, label_selector="application=spilo")

    def wait_for_logical_backup_job(self, expected_num_of_jobs):
        while (len(self.get_logical_backup_job().items) != expected_num_of_jobs):
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_logical_backup_job_deletion(self):
        self.wait_for_logical_backup_job(expected_num_of_jobs=0)

    def wait_for_logical_backup_job_creation(self):
        self.wait_for_logical_backup_job(expected_num_of_jobs=1)

    def delete_operator_pod(self):
        operator_pod = self.api.core_v1.list_namespaced_pod(
            'default', label_selector="name=postgres-operator").items[0].metadata.name
        self.api.core_v1.delete_namespaced_pod(operator_pod, "default")  # restart reloads the conf
        self.wait_for_operator_pod_start()

    def update_config(self, config_map_patch):
        self.api.core_v1.patch_namespaced_config_map("postgres-operator", "default", config_map_patch)
        self.delete_operator_pod()

    def create_with_kubectl(self, path):
        return subprocess.run(
            ["kubectl", "create", "-f", path],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE)

    def exec_with_kubectl(self, pod, cmd):
        return subprocess.run(["./exec.sh", pod, cmd],
                              stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE)

    def get_effective_pod_image(self, pod_name, namespace='default'):
        '''
        Get the Spilo image pod currently uses. In case of lazy rolling updates
        it may differ from the one specified in the stateful set.
        '''
        pod = self.api.core_v1.list_namespaced_pod(
            namespace, label_selector="statefulset.kubernetes.io/pod-name=" + pod_name)
        return pod.items[0].spec.containers[0].image


if __name__ == '__main__':
    unittest.main()
