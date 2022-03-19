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
    def test_min_resource_limits(self):
        '''
        Lower resource limits below configured minimum and let operator fix it
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # get nodes of master and replica(s) (expected target of new master)
        _, replica_nodes = k8s.get_pg_nodes(cluster_label)
        self.assertNotEqual(replica_nodes, [])

        # configure minimum boundaries for CPU and memory limits
        minCPULimit = '503m'
        minMemoryLimit = '502Mi'

        patch_min_resource_limits = {
            "data": {
                "min_cpu_limit": minCPULimit,
                "min_memory_limit": minMemoryLimit
            }
        }
        k8s.update_config(patch_min_resource_limits, "Minimum resource test")

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
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

        # wait for switched over
        k8s.wait_for_pod_failover(replica_nodes, 'spilo-role=master,' + cluster_label)
        k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

        def verify_pod_limits():
            pods = k8s.api.core_v1.list_namespaced_pod('default', label_selector="cluster-name=acid-minimal-cluster,application=spilo").items
            if len(pods) < 2:
                return False

            r = pods[0].spec.containers[0].resources.limits['memory'] == minMemoryLimit
            r = r and pods[0].spec.containers[0].resources.limits['cpu'] == minCPULimit
            r = r and pods[1].spec.containers[0].resources.limits['memory'] == minMemoryLimit
            r = r and pods[1].spec.containers[0].resources.limits['cpu'] == minCPULimit
            return r

        self.eventuallyTrue(verify_pod_limits, "Pod limits where not adjusted")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_multi_namespace_support(self):
        '''
        Create a customized Postgres cluster in a non-default namespace.
        '''
        k8s = self.k8s

        with open("manifests/complete-postgres-manifest.yaml", 'r+') as f:
            pg_manifest = yaml.safe_load(f)
            pg_manifest["metadata"]["namespace"] = self.test_namespace
            yaml.dump(pg_manifest, f, Dumper=yaml.Dumper)

        try:
            k8s.create_with_kubectl("manifests/complete-postgres-manifest.yaml")
            k8s.wait_for_pod_start("spilo-role=master", self.test_namespace)
            k8s.wait_for_pod_start("spilo-role=replica", self.test_namespace)
            self.assert_master_is_unique(self.test_namespace, "acid-test-cluster")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise
        finally:
            # delete the new cluster so that the k8s_api.get_operator_state works correctly in subsequent tests
            # ideally we should delete the 'test' namespace here but
            # the pods inside the namespace stuck in the Terminating state making the test time out
            k8s.api.custom_objects_api.delete_namespaced_custom_object(
                "acid.zalan.do", "v1", self.test_namespace, "postgresqls", "acid-test-cluster")
            time.sleep(5)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_node_affinity(self):
        '''
           Add label to a node and update postgres cluster spec to deploy only on a node with that label
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # verify we are in good state from potential previous tests
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")

        # get nodes of master and replica(s)
        master_nodes, replica_nodes = k8s.get_cluster_nodes()
        self.assertNotEqual(master_nodes, [])
        self.assertNotEqual(replica_nodes, [])

        # label node with environment=postgres
        node_label_body = {
            "metadata": {
                "labels": {
                    "node-affinity-test": "postgres"
                }
            }
        }

        try:
            # patch master node with the label
            k8s.api.core_v1.patch_node(master_nodes[0], node_label_body)

            # add node affinity to cluster
            patch_node_affinity_config = {
                "spec": {
                    "nodeAffinity" : {
                        "requiredDuringSchedulingIgnoredDuringExecution": {
                            "nodeSelectorTerms": [
                                {
                                    "matchExpressions": [
                                        {
                                            "key": "node-affinity-test",
                                            "operator": "In",
                                            "values": [
                                                "postgres"
                                            ]
                                        }
                                    ]
                                }
                            ]
                        }
                    }
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                group="acid.zalan.do",
                version="v1",
                namespace="default",
                plural="postgresqls",
                name="acid-minimal-cluster",
                body=patch_node_affinity_config)
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                                 "Operator does not get in sync")

            # node affinity change should cause replica to relocate from replica node to master node due to node affinity requirement
            k8s.wait_for_pod_failover(master_nodes, 'spilo-role=replica,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)
            # next master will be switched over and pod needs to be replaced as well to finish the rolling update
            k8s.wait_for_pod_failover(master_nodes, 'spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            podsList = k8s.api.core_v1.list_namespaced_pod('default', label_selector=cluster_label)
            for pod in podsList.items:
                if pod.metadata.labels.get('spilo-role') == 'replica':
                    self.assertEqual(master_nodes[0], pod.spec.node_name,
                         "Sanity check: expected replica to relocate to master node {}, but found on {}".format(master_nodes[0], pod.spec.node_name))

                    # check that pod has correct node affinity
                    key = pod.spec.affinity.node_affinity.required_during_scheduling_ignored_during_execution.node_selector_terms[0].match_expressions[0].key
                    value = pod.spec.affinity.node_affinity.required_during_scheduling_ignored_during_execution.node_selector_terms[0].match_expressions[0].values[0]
                    self.assertEqual("node-affinity-test", key,
                        "Sanity check: expect node selector key to be equal to 'node-affinity-test' but got {}".format(key))
                    self.assertEqual("postgres", value,
                        "Sanity check: expect node selector value to be equal to 'postgres' but got {}".format(value))

            patch_node_remove_affinity_config = {
                "spec": {
                    "nodeAffinity" : None
                }
            }
            k8s.api.custom_objects_api.patch_namespaced_custom_object(
                group="acid.zalan.do",
                version="v1",
                namespace="default",
                plural="postgresqls",
                name="acid-minimal-cluster",
                body=patch_node_remove_affinity_config)
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            # node affinity change should cause another rolling update and relocation of replica
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=master,' + cluster_label)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # toggle pod anti affinity to make sure replica and master run on separate nodes
        self.assert_distributed_pods(master_nodes)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_node_readiness_label(self):
        '''
           Remove node readiness label from master node. This must cause a failover.
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'
        readiness_label = 'lifecycle-status'
        readiness_value = 'ready'

        # verify we are in good state from potential previous tests
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")

        # get nodes of master and replica(s) (expected target of new master)
        master_nodes, replica_nodes = k8s.get_cluster_nodes()
        self.assertNotEqual(master_nodes, [])
        self.assertNotEqual(replica_nodes, [])

        try:
            # add node_readiness_label to potential failover nodes
            patch_readiness_label = {
                "metadata": {
                    "labels": {
                        readiness_label: readiness_value
                    }
                }
            }
            for replica_node in replica_nodes:
                k8s.api.core_v1.patch_node(replica_node, patch_readiness_label)

            # define node_readiness_label in config map which should trigger a rolling update
            patch_readiness_label_config = {
                "data": {
                    "node_readiness_label": readiness_label + ':' + readiness_value,
                    "node_readiness_label_merge": "AND",
                }
            }
            k8s.update_config(patch_readiness_label_config, "setting readiness label")
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            # first replica will be replaced and get the new affinity
            # however, it might not start due to a volume node affinity conflict
            # in this case only if the pvc and pod are deleted it can be scheduled
            replica = k8s.get_cluster_replica_pod()
            if replica.status.phase == 'Pending':
                k8s.api.core_v1.delete_namespaced_persistent_volume_claim('pgdata-' + replica.metadata.name, 'default')
                k8s.api.core_v1.delete_namespaced_pod(replica.metadata.name, 'default')
                k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # next master will be switched over and pod needs to be replaced as well to finish the rolling update
            k8s.wait_for_pod_failover(replica_nodes, 'spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # patch also node where master ran before
            k8s.api.core_v1.patch_node(master_nodes[0], patch_readiness_label)

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # toggle pod anti affinity to move replica away from master node
        self.assert_distributed_pods(master_nodes)


    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_overwrite_pooler_deployment(self):
        pooler_name = 'acid-minimal-cluster-pooler'
        k8s = self.k8s
        k8s.create_with_kubectl("manifests/minimal-fake-pooler-deployment.yaml")
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(name=pooler_name), 1,
                             "Initial broken deployment not rolled out")

        k8s.api.custom_objects_api.patch_namespaced_custom_object(
        'acid.zalan.do', 'v1', 'default',
        'postgresqls', 'acid-minimal-cluster',
        {
            'spec': {
                'enableConnectionPooler': True
            }
        })

        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.get_deployment_replica_count(name=pooler_name), 2,
                             "Operator did not succeed in overwriting labels")

        k8s.api.custom_objects_api.patch_namespaced_custom_object(
        'acid.zalan.do', 'v1', 'default',
        'postgresqls', 'acid-minimal-cluster',
        {
            'spec': {
                'enableConnectionPooler': False
            }
        })

        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyEqual(lambda: k8s.count_running_pods("connection-pooler="+pooler_name),
                             0, "Pooler pods not scaled down")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_password_rotation(self):
        '''
           Test password rotation and removal of users due to retention policy
        '''
        k8s = self.k8s
        leader = k8s.get_cluster_leader_pod()
        today = date.today()

        # enable password rotation for owner of foo database
        pg_patch_inplace_rotation_for_owner = {
            "spec": {
                "usersWithInPlaceSecretRotation": [
                    "zalando"
                ]
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_inplace_rotation_for_owner)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        # check if next rotation date was set in secret
        secret_data = k8s.get_secret_data("zalando")
        next_rotation_timestamp = datetime.strptime(str(base64.b64decode(secret_data["nextRotation"]), 'utf-8'), "%Y-%m-%dT%H:%M:%SZ")
        today90days = today+timedelta(days=90)
        self.assertEqual(today90days, next_rotation_timestamp.date(),
                        "Unexpected rotation date in secret of zalando user: expected {}, got {}".format(today90days, next_rotation_timestamp.date()))

        # create fake rotation users that should be removed by operator
        # but have one that would still fit into the retention period
        create_fake_rotation_user = """
            CREATE ROLE foo_user201031 IN ROLE foo_user;
            CREATE ROLE foo_user211031 IN ROLE foo_user;
            CREATE ROLE foo_user"""+(today-timedelta(days=40)).strftime("%y%m%d")+""" IN ROLE foo_user;
        """
        self.query_database(leader.metadata.name, "postgres", create_fake_rotation_user)

        # patch foo_user secret with outdated rotation date
        fake_rotation_date = today.isoformat() + 'T00:00:00Z'
        fake_rotation_date_encoded = base64.b64encode(fake_rotation_date.encode('utf-8'))
        secret_fake_rotation = {
            "data": {
                "nextRotation": str(fake_rotation_date_encoded, 'utf-8'),
            },
        }
        k8s.api.core_v1.patch_namespaced_secret(
            name="foo-user.acid-minimal-cluster.credentials.postgresql.acid.zalan.do", 
            namespace="default",
            body=secret_fake_rotation)

        # enable password rotation for all other users (foo_user)
        # this will force a sync of secrets for further assertions
        enable_password_rotation = {
            "data": {
                "enable_password_rotation": "true",
                "password_rotation_interval": "30",
                "password_rotation_user_retention": "30",  # should be set to 60 
            },
        }
        k8s.update_config(enable_password_rotation)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

        # check if next rotation date and username have been replaced
        secret_data = k8s.get_secret_data("foo_user")
        secret_username = str(base64.b64decode(secret_data["username"]), 'utf-8')
        next_rotation_timestamp = datetime.strptime(str(base64.b64decode(secret_data["nextRotation"]), 'utf-8'), "%Y-%m-%dT%H:%M:%SZ")
        rotation_user = "foo_user"+today.strftime("%y%m%d")
        today30days = today+timedelta(days=30)

        self.assertEqual(rotation_user, secret_username,
                        "Unexpected username in secret of foo_user: expected {}, got {}".format(rotation_user, secret_username))
        self.assertEqual(today30days, next_rotation_timestamp.date(),
                        "Unexpected rotation date in secret of foo_user: expected {}, got {}".format(today30days, next_rotation_timestamp.date()))

        # check if oldest fake rotation users were deleted
        # there should only be foo_user, foo_user+today and foo_user+today-40days
        user_query = """
            SELECT rolname
              FROM pg_catalog.pg_roles
             WHERE rolname LIKE 'foo_user%';
        """
        self.eventuallyEqual(lambda: len(self.query_database(leader.metadata.name, "postgres", user_query)), 3,
            "Found incorrect number of rotation users", 10, 5)

        # disable password rotation for all other users (foo_user)
        # and pick smaller intervals to see if the third fake rotation user is dropped 
        enable_password_rotation = {
            "data": {
                "enable_password_rotation": "false",
                "password_rotation_interval": "15",
                "password_rotation_user_retention": "30",  # 2 * rotation interval
            },
        }
        k8s.update_config(enable_password_rotation)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                             "Operator does not get in sync")

        # check if username in foo_user secret is reset
        secret_data = k8s.get_secret_data("foo_user")
        secret_username = str(base64.b64decode(secret_data["username"]), 'utf-8')
        next_rotation_timestamp = str(base64.b64decode(secret_data["nextRotation"]), 'utf-8')
        self.assertEqual("foo_user", secret_username,
                        "Unexpected username in secret of foo_user: expected {}, got {}".format("foo_user", secret_username))
        self.assertEqual('', next_rotation_timestamp,
                        "Unexpected rotation date in secret of foo_user: expected empty string, got {}".format(next_rotation_timestamp))

        # check roles again, there should only be foo_user and foo_user+today
        user_query = """
            SELECT rolname
              FROM pg_catalog.pg_roles
             WHERE rolname LIKE 'foo_user%';
        """
        self.eventuallyEqual(lambda: len(self.query_database(leader.metadata.name, "postgres", user_query)), 2,
            "Found incorrect number of rotation users", 10, 5)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_rolling_update_flag(self):
        '''
            Add rolling update flag to only the master and see it failing over
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # verify we are in good state from potential previous tests
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")

        # get node and replica (expected target of new master)
        _, replica_nodes = k8s.get_pg_nodes(cluster_label)

        # rolling update annotation
        flag = {
            "metadata": {
                "annotations": {
                    "zalando-postgres-operator-rolling-update-required": "true",
                }
            }
        }

        try:
            podsList = k8s.api.core_v1.list_namespaced_pod('default', label_selector=cluster_label)
            for pod in podsList.items:
                # add flag only to the master to make it appear to the operator as a leftover from a rolling update
                if pod.metadata.labels.get('spilo-role') == 'master':
                    old_creation_timestamp = pod.metadata.creation_timestamp
                    k8s.patch_pod(flag, pod.metadata.name, pod.metadata.namespace)
                else:
                    # remember replica name to check if operator does a switchover
                    switchover_target = pod.metadata.name

            # do not wait until the next sync
            k8s.delete_operator_pod()

            # operator should now recreate the master pod and do a switchover before
            k8s.wait_for_pod_failover(replica_nodes, 'spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # check if the former replica is now the new master
            leader = k8s.get_cluster_leader_pod()
            self.eventuallyEqual(lambda: leader.metadata.name, switchover_target, "Rolling update flag did not trigger switchover")

            # check that the old master has been recreated
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)
            replica = k8s.get_cluster_replica_pod()
            self.assertTrue(replica.metadata.creation_timestamp > old_creation_timestamp, "Old master pod was not recreated")


        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_rolling_update_label_timeout(self):
        '''
            Simulate case when replica does not receive label in time and rolling update does not finish
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'
        flag = "zalando-postgres-operator-rolling-update-required"

        # verify we are in good state from potential previous tests
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")

        # get node and replica (expected target of new master)
        _, replica_nodes = k8s.get_pg_nodes(cluster_label)

        # rolling update annotation
        rolling_update_patch = {
            "metadata": {
                "annotations": {
                    flag: "true",
                }
            }
        }

        # make pod_label_wait_timeout so short that rolling update fails on first try
        # temporarily lower resync interval to reduce waiting for further tests
        # pods should get healthy in the meantime
        patch_resync_config = {
            "data": {
                "pod_label_wait_timeout": "2s",
                "resync_period": "30s",
                "repair_period": "30s",
            }
        }

        try:
            # patch both pods for rolling update
            podList = k8s.api.core_v1.list_namespaced_pod('default', label_selector=cluster_label)
            for pod in podList.items:
                k8s.patch_pod(rolling_update_patch, pod.metadata.name, pod.metadata.namespace)
                if pod.metadata.labels.get('spilo-role') == 'replica':
                    switchover_target = pod.metadata.name

            # update config and restart operator
            k8s.update_config(patch_resync_config, "update resync interval and pod_label_wait_timeout")

            # operator should now recreate the replica pod first and do a switchover after
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # pod_label_wait_timeout should have been exceeded hence the rolling update is continued on next sync
            # check if the cluster state is "SyncFailed"
            self.eventuallyEqual(lambda: k8s.pg_get_status(), "SyncFailed", "Expected SYNC event to fail")

            # wait for next sync, replica should be running normally by now and be ready for switchover
            k8s.wait_for_pod_failover(replica_nodes, 'spilo-role=master,' + cluster_label)
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # check if the former replica is now the new master
            leader = k8s.get_cluster_leader_pod()
            self.eventuallyEqual(lambda: leader.metadata.name, switchover_target, "Rolling update flag did not trigger switchover")

            # wait for the old master to get restarted
            k8s.wait_for_pod_start('spilo-role=replica,' + cluster_label)

            # status should again be "SyncFailed" but turn into "Running" on the next sync
            time.sleep(30)
            self.eventuallyEqual(lambda: k8s.pg_get_status(), "Running", "Expected running cluster after two syncs")

            # revert config changes
            patch_resync_config = {
                "data": {
                    "pod_label_wait_timeout": "10m",
                    "resync_period": "4m",
                    "repair_period": "2m",
                }
            }
            k8s.update_config(patch_resync_config, "revert resync interval and pod_label_wait_timeout")


        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_scaling(self):
        '''
           Scale up from 2 to 3 and back to 2 pods by updating the Postgres manifest at runtime.
        '''
        k8s = self.k8s
        pod = "acid-minimal-cluster-0"

        k8s.scale_cluster(3)
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 3, "Scale up to 3 failed")
        self.eventuallyEqual(lambda: len(k8s.get_patroni_running_members(pod)), 3, "Not all 3 nodes healthy")

        k8s.scale_cluster(2)
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "Scale down to 2 failed")
        self.eventuallyEqual(lambda: len(k8s.get_patroni_running_members(pod)), 2, "Not all members 2 healthy")

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

        pg_patch_custom_annotations = {
            "spec": {
                "serviceAnnotations": {
                    "annotation.key": "value",
                    "alice": "bob",
                }
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_custom_annotations)

        annotations = {
            "annotation.key": "value",
            "foo": "bar",
            "alice": "bob"
        }

        self.eventuallyTrue(lambda: k8s.check_service_annotations("cluster-name=acid-minimal-cluster,spilo-role=master", annotations), "Wrong annotations")
        self.eventuallyTrue(lambda: k8s.check_service_annotations("cluster-name=acid-minimal-cluster,spilo-role=replica", annotations), "Wrong annotations")

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
                "inherited_annotations": "owned-by",
            }
        }
        k8s.update_config(patch_sset_propagate_annotations)

        pg_crd_annotations = {
            "metadata": {
                "annotations": {
                    "deployment-time": "2020-04-30 12:00:00",
                    "downscaler/downtime_replicas": "0",
                    "owned-by": "acid",
                },
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object(
            "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_crd_annotations)

        annotations = {
            "deployment-time": "2020-04-30 12:00:00",
            "downscaler/downtime_replicas": "0",
            "owned-by": "acid",
        }
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")
        self.eventuallyTrue(lambda: k8s.check_statefulset_annotations(cluster_label, annotations), "Annotations missing")

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_taint_based_eviction(self):
        '''
           Add taint "postgres=:NoExecute" to node with master. This must cause a failover.
        '''
        k8s = self.k8s
        cluster_label = 'application=spilo,cluster-name=acid-minimal-cluster'

        # verify we are in good state from potential previous tests
        self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")
        self.eventuallyEqual(lambda: len(k8s.get_patroni_running_members("acid-minimal-cluster-0")), 2, "Postgres status did not enter running")

        # get nodes of master and replica(s) (expected target of new master)
        master_nodes, replica_nodes = k8s.get_cluster_nodes()
        self.assertNotEqual(master_nodes, [])
        self.assertNotEqual(replica_nodes, [])

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
        k8s.api.core_v1.patch_node(master_nodes[0], body)

        # add toleration to pods
        patch_toleration_config = {
            "data": {
                "toleration": "key:postgres,operator:Exists,effect:NoExecute"
            }
        }

        try:
            k8s.update_config(patch_toleration_config, step="allow tainted nodes")
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"},
                        "Operator does not get in sync")

            self.eventuallyEqual(lambda: k8s.count_running_pods(), 2, "No 2 pods running")
            self.eventuallyEqual(lambda: len(k8s.get_patroni_running_members("acid-minimal-cluster-0")), 2, "Postgres status did not enter running")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # toggle pod anti affinity to move replica away from master node
        self.assert_distributed_pods(master_nodes)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_zz_cluster_deletion(self):
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
        time.sleep(25)
        self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

        try:
            # this delete attempt should be omitted because of missing annotations
            k8s.api.custom_objects_api.delete_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster")
            time.sleep(15)
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            # check that pods and services are still there
            k8s.wait_for_running_pods(cluster_label, 2)
            k8s.wait_for_service(cluster_label)

            # recreate Postgres cluster resource
            k8s.create_with_kubectl("manifests/minimal-postgres-manifest.yaml")

            # wait a little before proceeding
            time.sleep(10)
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

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
            self.eventuallyEqual(lambda: k8s.get_operator_state(), {"0": "idle"}, "Operator does not get in sync")

            # wait a little before proceeding
            time.sleep(20)
            k8s.wait_for_running_pods(cluster_label, 2)
            k8s.wait_for_service(cluster_label)

            # now delete process should be triggered
            k8s.api.custom_objects_api.delete_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster")

            self.eventuallyEqual(lambda: len(k8s.api.custom_objects_api.list_namespaced_custom_object(
                "acid.zalan.do", "v1", "default", "postgresqls", label_selector="cluster-name=acid-minimal-cluster")["items"]), 0, "Manifest not deleted")

            # check if everything has been deleted
            self.eventuallyEqual(lambda: k8s.count_pods_with_label(cluster_label), 0, "Pods not deleted")
            self.eventuallyEqual(lambda: k8s.count_services_with_label(cluster_label), 0, "Service not deleted")
            self.eventuallyEqual(lambda: k8s.count_endpoints_with_label(cluster_label), 0, "Endpoints not deleted")
            self.eventuallyEqual(lambda: k8s.count_statefulsets_with_label(cluster_label), 0, "Statefulset not deleted")
            self.eventuallyEqual(lambda: k8s.count_deployments_with_label(cluster_label), 0, "Deployments not deleted")
            self.eventuallyEqual(lambda: k8s.count_pdbs_with_label(cluster_label), 0, "Pod disruption budget not deleted")
            self.eventuallyEqual(lambda: k8s.count_secrets_with_label(cluster_label), 0, "Secrets not deleted")

        except timeout_decorator.TimeoutError:
            print('Operator log: {}'.format(k8s.get_operator_log()))
            raise

        # reset configmap
        patch_delete_annotations = {
            "data": {
                "delete_annotation_date_key": "",
                "delete_annotation_name_key": ""
            }
        }
        k8s.update_config(patch_delete_annotations)

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
