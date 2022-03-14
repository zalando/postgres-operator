import json
import unittest
import time
import timeout_decorator
import os
import yaml
import base64

from datetime import datetime, date, timedelta
from kubernetes import client

from tests.k8s_api import K8s
from kubernetes.client.rest import ApiException

SPILO_CURRENT = "registry.opensource.zalan.do/acid/spilo-14-e2e:0.1"
SPILO_LAZY = "registry.opensource.zalan.do/acid/spilo-14-e2e:0.2"


def to_selector(labels):
    return ",".join(["=".join(lbl) for lbl in labels.items()])


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

    def eventuallyEqual(self, f, x, m, retries=60, interval=2):
        while True:
            try:
                y = f()
                self.assertEqual(y, x, m.format(y))
                return True
            except AssertionError:
                retries = retries - 1
                if not retries > 0:
                    raise
                time.sleep(interval)

    def eventuallyNotEqual(self, f, x, m, retries=60, interval=2):
        while True:
            try:
                y = f()
                self.assertNotEqual(y, x, m.format(y))
                return True
            except AssertionError:
                retries = retries - 1
                if not retries > 0:
                    raise
                time.sleep(interval)

    def eventuallyTrue(self, f, m, retries=60, interval=2):
        while True:
            try:
                self.assertTrue(f(), m)
                return True
            except AssertionError:
                retries = retries - 1
                if not retries > 0:
                    raise
                time.sleep(interval)

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
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # remove existing local storage class and create hostpath class
        try:
            k8s.api.storage_v1_api.delete_storage_class("standard")
        except ApiException as e:
            print("Failed to delete the 'standard' storage class: {0}".format(e))

        # operator deploys pod service account there on start up
        # needed for test_multi_namespace_support()
        cls.test_namespace = "test"
        try:
            v1_namespace = client.V1Namespace(metadata=client.V1ObjectMeta(name=cls.test_namespace))
            k8s.api.core_v1.create_namespace(v1_namespace)
        except ApiException as e:
            print("Failed to create the '{0}' namespace: {1}".format(cls.test_namespace, e))

        # submit the most recent operator image built on the Docker host
        with open("manifests/postgres-operator.yaml", 'r+') as f:
            operator_deployment = yaml.safe_load(f)
            operator_deployment["spec"]["template"]["spec"]["containers"][0]["image"] = os.environ['OPERATOR_IMAGE']

        with open("manifests/postgres-operator.yaml", 'w') as f:
            yaml.dump(operator_deployment, f, Dumper=yaml.Dumper)

        with open("manifests/configmap.yaml", 'r+') as f:
            configmap = yaml.safe_load(f)
            configmap["data"]["workers"] = "1"
            configmap["data"]["docker_image"] = SPILO_CURRENT

        with open("manifests/configmap.yaml", 'w') as f:
            yaml.dump(configmap, f, Dumper=yaml.Dumper)

        for filename in ["operator-service-account-rbac.yaml",
                         "postgresql.crd.yaml",
                         "operatorconfiguration.crd.yaml",
                         "postgresteam.crd.yaml",
                         "configmap.yaml",
                         "postgres-operator.yaml",
                         "api-service.yaml",
                         "infrastructure-roles.yaml",
                         "infrastructure-roles-new.yaml",
                         "custom-team-membership.yaml",
                         "e2e-storage-class.yaml"]:
            result = k8s.create_with_kubectl("manifests/" + filename)
            print("stdout: {}, stderr: {}".format(result.stdout, result.stderr))

        k8s.wait_for_operator_pod_start()

        # reset taints and tolerations
        k8s.api.core_v1.patch_node("postgres-operator-e2e-tests-worker", {"spec": {"taints": []}})
        k8s.api.core_v1.patch_node("postgres-operator-e2e-tests-worker2", {"spec": {"taints": []}})

        # make sure we start a new operator on every new run,
        # this tackles the problem when kind is reused
        # and the Docker image is in fact changed (dirty one)

        k8s.update_config({}, step="TestSuite Startup")

        actual_operator_image = k8s.api.core_v1.list_namespaced_pod(
            'default', label_selector='name=postgres-operator').items[0].spec.containers[0].image
        print("Tested operator image: {}".format(actual_operator_image))  # shows up after tests finish

        result = k8s.create_with_kubectl("manifests/minimal-postgres-manifest.yaml")
        print('stdout: {}, stderr: {}'.format(result.stdout, result.stderr))
        try:
            k8s.wait_for_pod_start('spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)
        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_additional_pod_capabilities(self):
        '''
           Extend postgres container capabilities
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'
        capabilities = ["SYS_NICE","CHOWN"]
        patch_capabilities = {
            "data": {
                "additional_pod_capabilities": ','.join(capabilities),
            },
        }

        # get node and replica (expected target of new master)
        _, replica_nodes = k8s.get_pg_nodes(cluster_label)

        try:
            k8s.update_config(patch_capabilities)
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                                "Operator does not get in sync")

            # changed security context of postgres container should trigger a rolling update
            k8s.wait_for_pod_failover(replica_nodes, 'spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            self.eventuallyEqual(lambda: k8s.count_pods_with_container_capabilities(capabilities, cluster_label),
                                2, "Container capabilities not updated")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_additional_teams_and_members(self):
        '''
           Test PostgresTeam CRD with extra teams and members
        '''
        k8s = self.k8s

        # enable PostgresTeam CRD and lower resync
        enable_postgres_team_crd = {
            "data": {
                "enable_postgres_team_crd": "true",
                "enable_team_member_deprecation": "true",
                "role_deletion_suffix": "_delete_me",
                "resync_period": "15s",
                "repair_period": "15s",
            },
        }
        k8s.update_config(enable_postgres_team_crd)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

        k8s.api.custom_objects_api.patch_namespaced_custom_object(
        'acid.zalan.do', 'v1', 'default',
        'postgresteams', 'custom-team-membership',
        {
            'spec': {
                'additionalTeams': {
                    'acid': [
                        'e2e'
                    ]
                },
                'additionalMembers': {
                    'e2e': [
                        'kind'
                    ]
                }
            }
        })

        leader = k8s.get_cluster_leader_pod()
        user_query = """
            SELECT rolname
              FROM pg_catalog.pg_roles
             WHERE rolname IN ('elephant', 'kind');
        """
        self.eventuallyEqual(lambda: len(self.query_database(leader.metadata.name, "postgres", user_query)), 2,
            "Not all additional users found in database", 10, 5)

        # replace additional member and check if the removed member's role is renamed
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
        'acid.zalan.do', 'v1', 'default',
        'postgresteams', 'custom-team-membership',
        {
            'spec': {
                'additionalMembers': {
                    'e2e': [
                        'tester'
                    ]
                },
            }
        })

        user_query = """
            SELECT rolname
              FROM pg_catalog.pg_roles
             WHERE (rolname = 'tester' AND rolcanlogin)
                OR (rolname = 'kind_delete_me' AND NOT rolcanlogin);
        """
        self.eventuallyEqual(lambda: len(self.query_database(leader.metadata.name, "postgres", user_query)), 2,
            "Database role of replaced member in PostgresTeam not renamed", 10, 5)

        # re-add additional member and check if the role is renamed back
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
        'acid.zalan.do', 'v1', 'default',
        'postgresteams', 'custom-team-membership',
        {
            'spec': {
                'additionalMembers': {
                    'e2e': [
                        'kind'
                    ]
                },
            }
        })

        user_query = """
            SELECT rolname
              FROM pg_catalog.pg_roles
             WHERE (rolname = 'kind' AND rolcanlogin)
                OR (rolname = 'tester_delete_me' AND NOT rolcanlogin);
        """
        self.eventuallyEqual(lambda: len(self.query_database(leader.metadata.name, "postgres", user_query)), 2,
            "Database role of recreated member in PostgresTeam not renamed back to original name", 10, 5)

        # revert config change
        revert_resync = {
            "data": {
                "resync_period": "4m",
                "repair_period": "1m",
            },
        }
        k8s.update_config(revert_resync)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_config_update(self):
        '''
            Change Postgres config under Spec.Postgresql.Parameters and Spec.Patroni
            and query Patroni config endpoint to check if manifest changes got applied
            via restarting cluster through Patroni's rest api
        '''
        k8s = self.k8s
        leader = k8s.get_cluster_leader_pod()
        replica = k8s.get_cluster_replica_pod()
        masterCreationTimestamp = leader.metadata.creation_timestamp
        replicaCreationTimestamp = replica.metadata.creation_timestamp
        new_max_connections_value = "50"

        # adjust Postgres config
        pg_patch_config = {
            "spec": {
                "postgresql": {
                    "parameters": {
                        "max_connections": new_max_connections_value
                     }
                 },
                 "patroni": {
                    "slots": {
                        "test_slot": {
                            "type": "physical"
                        }
                    },
                    "ttl": 29,
                    "loop_wait": 9,
                    "retry_timeout": 9,
                    "synchronous_mode": True
                 }
            }
        }

        try:
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_config)
            
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            def compare_config():
                effective_config = k8s.patroni_rest(leader.metadata.name, "config")
                desired_config = pg_patch_config["spec"]["patroni"]
                desired_parameters = pg_patch_config["spec"]["postgresql"]["parameters"]
                effective_parameters = effective_config["postgresql"]["parameters"]
                self.assertEqual(desired_parameters["max_connections"], effective_parameters["max_connections"],
                            "max_connections not updated")
                self.assertTrue(effective_config["slots"] is not None, "physical replication slot not added")
                self.assertEqual(desired_config["ttl"], effective_config["ttl"],
                            "ttl not updated")
                self.assertEqual(desired_config["loop_wait"], effective_config["loop_wait"],
                            "loop_wait not updated")
                self.assertEqual(desired_config["retry_timeout"], effective_config["retry_timeout"],
                            "retry_timeout not updated")
                self.assertEqual(desired_config["synchronous_mode"], effective_config["synchronous_mode"],
                            "synchronous_mode not updated")
                return True

            # check if Patroni config has been updated
            self.eventuallyTrue(compare_config, "Postgres config not applied")

            # make sure that pods were not recreated
            leader = k8s.get_cluster_leader_pod()
            replica = k8s.get_cluster_replica_pod()
            self.assertEqual(masterCreationTimestamp, leader.metadata.creation_timestamp,
                            "Master pod creation timestamp is updated")
            self.assertEqual(replicaCreationTimestamp, replica.metadata.creation_timestamp,
                            "Master pod creation timestamp is updated")

            # query max_connections setting
            setting_query = """
               SELECT setting
                 FROM pg_settings
                WHERE name = 'max_connections';
            """
            self.eventuallyEqual(lambda: self.query_database(leader.metadata.name, "postgres", setting_query)[0], new_max_connections_value,
                "New max_connections setting not applied on master", 10, 5)
            self.eventuallyNotEqual(lambda: self.query_database(replica.metadata.name, "postgres", setting_query)[0], new_max_connections_value,
                "Expected max_connections not to be updated on replica since Postgres was restarted there first", 10, 5)

            # the next sync should restart the replica because it has pending_restart flag set
            # force next sync by deleting the operator pod
            k8s.delete_operator_pod()
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            self.eventuallyEqual(lambda: self.query_database(replica.metadata.name, "postgres", setting_query)[0], new_max_connections_value,
                "New max_connections setting not applied on replica", 10, 5)

            # decrease max_connections again
            # this time restart will be correct and new value should appear on both instances
            lower_max_connections_value = "30"
            pg_patch_max_connections = {
                "spec": {
                    "postgresql": {
                        "parameters": {
                            "max_connections": lower_max_connections_value
                        }
                    }
                }
            }

            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_max_connections)

            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            # check Patroni config again
            pg_patch_config["spec"]["postgresql"]["parameters"]["max_connections"] = lower_max_connections_value
            self.eventuallyTrue(compare_config, "Postgres config not applied")

            # and query max_connections setting again
            self.eventuallyEqual(lambda: self.query_database(leader.metadata.name, "postgres", setting_query)[0], lower_max_connections_value,
                "Previous max_connections setting not applied on master", 10, 5)
            self.eventuallyEqual(lambda: self.query_database(replica.metadata.name, "postgres", setting_query)[0], lower_max_connections_value,
                "Previous max_connections setting not applied on replica", 10, 5)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # make sure cluster is in a good state for further tests
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2,
                             "No 2 pods running")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_cross_namespace_secrets(self):
        '''
            Test secrets in different namespace
        '''
        k8s = self.k8s

        # enable secret creation in separate namespace
        patch_cross_namespace_secret = {
            "data": {
                "enable_cross_namespace_secret": "true"
            }
        }
        k8s.update_config(patch_cross_namespace_secret,
                          step="cross namespace secrets enabled")
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

        # create secret in test namespace
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'users':{
                        'test.db_user': [],
                    }
                }
            })
        
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.count_secrets_with_label("cluster-name=acid-minimal-cluster,application=spilo", self.test_namespace),
                             1, "Secret not created for user in namespace")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_enable_disable_connection_pooler(self):
        '''
        For a database without connection pooler, then turns it on, scale up,
        turn off and on again. Test with different ways of doing this (via
        enableConnectionPooler or connectionPooler configuration section). At
        the end turn connection pooler off to not interfere with other tests.
        '''
        k8s = self.k8s
        pooler_label = 'application=db-connection-pooler,cluster-name=acid-minimal-cluster'
        master_pooler_label = 'connection-pooler=acid-minimal-cluster-pooler'
        replica_pooler_label = master_pooler_label + '-repl'
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'enableConnectionPooler': True,
                    'enableReplicaConnectionPooler': True,
                }
            })
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(), 2, "Deployment replicas is 2 default")
        self.eventuallyEqual(lambda: k8s.count_running_pods(master_pooler_label), 2, "No pooler pods found")
        self.eventuallyEqual(lambda: k8s.count_running_pods(replica_pooler_label), 2, "No pooler replica pods found")
        self.eventuallyEqual(lambda: k8s.count_services_with_label(pooler_label), 2, "No pooler service found")
        self.eventuallyEqual(lambda: k8s.count_secrets_with_label(pooler_label), 1, "Pooler secret not created")

        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'enableMasterPoolerLoadBalancer': True,
                    'enableReplicaPoolerLoadBalancer': True,
                }
            })
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.get_service_type(master_pooler_label+","+pooler_label),
                             'LoadBalancer',
                             "Expected LoadBalancer service type for master pooler pod, found {}")
        self.eventuallyEqual(lambda: k8s.get_service_type(replica_pooler_label+","+pooler_label),
                             'LoadBalancer',
                             "Expected LoadBalancer service type for replica pooler pod, found {}")

        # Turn off only master connection pooler
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'enableConnectionPooler': False,
                    'enableReplicaConnectionPooler': True,
                }
            })

        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(name="acid-minimal-cluster-pooler-repl"), 2,
                             "Deployment replicas is 2 default")
        self.eventuallyEqual(lambda: k8s.count_running_pods(master_pooler_label),
                             0, "Master pooler pods not deleted")
        self.eventuallyEqual(lambda: k8s.count_running_pods(replica_pooler_label),
                             2, "Pooler replica pods not found")
        self.eventuallyEqual(lambda: k8s.count_services_with_label(pooler_label),
                             1, "No pooler service found")
        self.eventuallyEqual(lambda: k8s.count_secrets_with_label(pooler_label),
                             1, "Secret not created")

        # Turn off only replica connection pooler
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'enableConnectionPooler': True,
                    'enableReplicaConnectionPooler': False,
                    'enableMasterPoolerLoadBalancer': False,
                }
            })

        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(), 2,
                             "Deployment replicas is 2 default")
        self.eventuallyEqual(lambda: k8s.count_running_pods(master_pooler_label),
                             2, "Master pooler pods not found")
        self.eventuallyEqual(lambda: k8s.count_running_pods(replica_pooler_label),
                             0, "Pooler replica pods not deleted")
        self.eventuallyEqual(lambda: k8s.count_services_with_label(pooler_label),
                             1, "No pooler service found")
        self.eventuallyEqual(lambda: k8s.get_service_type(master_pooler_label+","+pooler_label),
                             'ClusterIP',
                             "Expected LoadBalancer service type for master, found {}")
        self.eventuallyEqual(lambda: k8s.count_secrets_with_label(pooler_label),
                             1, "Secret not created")

        # scale up connection pooler deployment
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'connectionPooler': {
                        'numberOfInstances': 3,
                    },
                }
            })

        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(), 3,
                             "Deployment replicas is scaled to 3")
        self.eventuallyEqual(lambda: k8s.count_running_pods(master_pooler_label),
                             3, "Scale up of pooler pods does not work")

        # turn it off, keeping config should be overwritten by false
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'enableConnectionPooler': False,
                    'enableReplicaConnectionPooler': False,
                    'enableReplicaPoolerLoadBalancer': False,
                }
            })

        self.eventuallyEqual(lambda: k8s.count_running_pods(master_pooler_label),
                             0, "Pooler pods not scaled down")
        self.eventuallyEqual(lambda: k8s.count_services_with_label(pooler_label),
                             0, "Pooler service not removed")
        self.eventuallyEqual(lambda: k8s.count_secrets_with_label('application=spilo,cluster-name=acid-minimal-cluster'),
                             4, "Secrets not deleted")

        # Verify that all the databases have pooler schema installed.
        # Do this via psql, since otherwise we need to deal with
        # credentials.
        db_list = []

        leader = k8s.get_cluster_leader_pod()
        schemas_query = """
            SELECT schema_name
              FROM information_schema.schemata
             WHERE schema_name = 'pooler'
        """

        db_list = self.list_databases(leader.metadata.name)
        for db in db_list:
            self.eventuallyNotEqual(lambda: len(self.query_database(leader.metadata.name, db, schemas_query)), 0,
                "Pooler schema not found in database {}".format(db))

        # remove config section to make test work next time
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            'acid.zalan.do', 'v1', 'default',
            'postgresqls', 'acid-minimal-cluster',
            {
                'spec': {
                    'connectionPooler': None,
                    'EnableReplicaConnectionPooler': False,
                }
            })

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_enable_load_balancer(self):
        '''
        Test if services are updated when enabling/disabling load balancers in Postgres manifest
        '''

        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster,spilo-role={}'

        self.eventuallyEqual(lambda: k8s.get_service_type(cluster_label.format("master")),
                             'ClusterIP',
                             "Expected ClusterIP type initially, found {}")

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

            self.eventuallyEqual(lambda: k8s.get_service_type(cluster_label.format("master")),
                                 'LoadBalancer',
                                 "Expected LoadBalancer service type for master, found {}")

            self.eventuallyEqual(lambda: k8s.get_service_type(cluster_label.format("replica")),
                                 'LoadBalancer',
                                 "Expected LoadBalancer service type for master, found {}")

            # disable load balancer services again
            pg_patch_disable_lbs = {
                "spec": {
                    "enableMasterLoadBalancer": False,
                    "enableReplicaLoadBalancer": False
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_disable_lbs)

            self.eventuallyEqual(lambda: k8s.get_service_type(cluster_label.format("master")),
                                 'ClusterIP',
                                 "Expected LoadBalancer service type for master, found {}")

            self.eventuallyEqual(lambda: k8s.get_service_type(cluster_label.format("replica")),
                                 'ClusterIP',
                                 "Expected LoadBalancer service type for master, found {}")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    def assert_master_is_unique(self, namespace='default', clusterName="acid-minimal-cluster"):
        '''
           Check that there is a single pod in the k8s cluster with the label "spilo-role=master"
           To be called manually after operations that affect pods
        '''
        k8s = self.k8s
        labels = 'spilo-role=master,cluster-name=' + clusterName

        num_of_master_pods = k8s.count_pods_with_label(labels, namespace)
        self.assertEqual(num_of_master_pods, 1, "Expected 1 master pod, found {}".format(num_of_master_pods))

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def assert_distributed_pods(self, target_nodes, cluster_labels='cluster-name=acid-minimal-cluster'):
        '''
           Other tests can lead to the situation that master and replica are on the same node.
           Toggle pod anti affinty to distribute pods accross nodes (replica in particular).
        '''
        k8s = self.k8s
        cluster_labels = 'application=spilo,cluster-name=acid-minimal-cluster'

        # get nodes of master and replica(s)
        master_nodes, replica_nodes = k8s.get_cluster_nodes()
        self.assertNotEqual(master_nodes, [])
        self.assertNotEqual(replica_nodes, [])

        # if nodes are different we can quit here
        if master_nodes[0] not in replica_nodes:
            return True             

        # enable pod anti affintiy in config map which should trigger movement of replica
        patch_enable_antiaffinity = {
            "data": {
                "enable_pod_antiaffinity": "true"
            }
        }

        try:
            k8s.update_config(patch_enable_antiaffinity, "enable antiaffinity")
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_labels)
            k8s.wait_for_running_pods(cluster_labels, 2)

            # now disable pod anti affintiy again which will cause yet another failover
            patch_disable_antiaffinity = {
                "data": {
                    "enable_pod_antiaffinity": "false"
                }
            }
            k8s.update_config(patch_disable_antiaffinity, "disable antiaffinity")
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
            
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_labels)
            k8s.wait_for_running_pods(cluster_labels, 2)

            master_nodes, replica_nodes = k8s.get_cluster_nodes()
            self.assertNotEqual(master_nodes, [])
            self.assertNotEqual(replica_nodes, [])

            # if nodes are different we can quit here
            for target_node in target_nodes:
                if (target_node not in master_nodes or target_node not in replica_nodes) and master_nodes[0] in replica_nodes:
                    print('Pods run on the same node') 
                    return False

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        return True

    def list_databases(self, pod_name):
        '''
           Get list of databases we might want to iterate over
        '''
        k8s = self.k8s
        result_set = []
        db_list = []
        db_list_query = "SELECT datname FROM pg_database"
        exec_query = r"psql -tAq -c \"{}\" -d {}"

        try:
            q = exec_query.format(db_list_query, "postgres")
            q = "su postgres -c \"{}\"".format(q)
            result = k8s.exec_with_kubectl(pod_name, q)
            db_list = clean_list(result.stdout.split(b'\n'))
        except Exception as ex:
            print('Could not get databases: {}'.format(ex))
            print('Stdout: {}'.format(result.stdout))
            print('Stderr: {}'.format(result.stderr))

        for db in db_list:
            if db in ('template0', 'template1'):
                continue
            result_set.append(db)

        return result_set

    def query_database(self, pod_name, db_name, query):
        '''
           Query database and return result as a list
        '''
        k8s = self.k8s
        result_set = []
        exec_query = r"psql -tAq -c \"{}\" -d {}"

        try:
            q = exec_query.format(query, db_name)
            q = "su postgres -c \"{}\"".format(q)
            result = k8s.exec_with_kubectl(pod_name, q)
            result_set = clean_list(result.stdout.split(b'\n'))
        except Exception as ex:
            print('Error on query execution: {}'.format(ex))
            print('Stdout: {}'.format(result.stdout))
            print('Stderr: {}'.format(result.stderr))

        return result_set

if __name__ == '__main__':
    unittest.main()
