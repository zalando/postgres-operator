import unittest
import time
import timeout_decorator
import subprocess
import warnings
import os

from kubernetes import client, config, utils


class SmokeTestCase(unittest.TestCase):
    '''
    Test the most basic functions of the operator.
    '''

    TEST_TIMEOUT_SEC = 600
    RETRY_TIMEOUT_SEC = 5

    @classmethod
    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def setUpClass(cls):
        '''
        Deploy operator to a "kind" cluster created by /e2e/run.sh using examples from /manifests.
        This operator deployment is to be shared among all tests.

        /e2e/run.sh deletes the 'kind' cluster after successful run along with all operator-related entities.
        In the case of test failure the cluster will stay to enable manual examination;
        next invocation of "make e2e" will re-create it.
        '''

        k8s_api = K8sApi()

        # HACK
        # 1. creating RBAC entites with a separate client fails
        #    with "AttributeError: object has no attribute 'select_header_accept'"
        # 2. utils.create_from_yaml cannot create multiple entites from a single file
        subprocess.run(["kubectl", "create", "-f", "manifests/operator-service-account-rbac.yaml"])

        for filename in ["configmap.yaml", "postgres-operator.yaml"]:
            utils.create_from_yaml(k8s_api.k8s_client, "manifests/" + filename)

        # submit the most recent operator image built on the Docker host
        body = {
            "spec": {
                "template": {
                    "spec": {
                        "containers": [
                            {
                              "name": "postgres-operator",
                              "image": os.environ['OPERATOR_IMAGE']
                            }
                        ]
                    }
                 }
            }
        }
        k8s_api.apps_v1.patch_namespaced_deployment("postgres-operator", "default", body)

        # TODO check if CRD is registered instead
        Utils.wait_for_pod_start(k8s_api, 'name=postgres-operator', cls.RETRY_TIMEOUT_SEC)

        # HACK around the lack of Python client for the acid.zalan.do resource
        subprocess.run(["kubectl", "create", "-f", "manifests/minimal-postgres-manifest.yaml"])

        Utils.wait_for_pod_start(k8s_api, 'spilo-role=master', cls.RETRY_TIMEOUT_SEC)

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_master_is_unique(self):
        """
           Check that there is a single pod in the k8s cluster with the label "spilo-role=master".
        """
        k8s = K8sApi()
        labels = 'spilo-role=master,version=acid-minimal-cluster'

        num_of_master_pods = Utils.count_pods_with_label(k8s, labels)
        self.assertEqual(num_of_master_pods, 1, "Expected 1 master pod, found {}".format(num_of_master_pods))

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_scaling(self):
        """
           Scale up from 2 to 3 pods and back to 2 by updating the Postgres manifest at runtime.
        """
        k8s = K8sApi()
        labels = "version=acid-minimal-cluster"

        Utils.wait_for_pg_to_scale(k8s, 3, self.RETRY_TIMEOUT_SEC)
        self.assertEqual(3, Utils.count_pods_with_label(k8s, labels))

        # TODO `kind` pods may stuck in the Terminating state for a few minutes; reproduce/file a bug report
        Utils.wait_for_pg_to_scale(k8s, 2, self.RETRY_TIMEOUT_SEC)
        self.assertEqual(2, Utils.count_pods_with_label(k8s, labels))

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_taint_based_eviction(self):
        """
           Add taint "postgres=:NoExecute" to node with master.
        """
        k8s = K8sApi()
        labels = 'version=acid-minimal-cluster'

        # get nodes of master and replica(s) (expected target of new master)
        current_master_node, failover_targets = Utils.get_spilo_nodes(k8s, labels)
        num_replicas = len(failover_targets)

        # if all pods live on the same node, failover will happen to other worker(s)
        failover_targets = [x for x in failover_targets if x != current_master_node]
        if len(failover_targets) == 0:
            nodes = k8s.core_v1.list_node()
            for n in nodes.items:
                if "node-role.kubernetes.io/master" not in n.metadata.labels and n.metadata.name != current_master_node:
                    failover_targets.append(n.metadata.name)

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

        # patch node and test if master is failing over to one of the expected nodes
        k8s.core_v1.patch_node(current_master_node, body)
        Utils.wait_for_master_failover(k8s, failover_targets, self.RETRY_TIMEOUT_SEC)
        Utils.wait_for_pod_start(k8s, 'spilo-role=replica', self.RETRY_TIMEOUT_SEC)

        new_master_node, new_replica_nodes = Utils.get_spilo_nodes(k8s, labels)
        self.assertTrue(current_master_node != new_master_node,
                        "Master on {} did not fail over to one of {}".format(current_master_node, failover_targets))
        self.assertTrue(num_replicas == len(new_replica_nodes),
                        "Expected {} replicas, found {}".format(num_replicas, len(new_replica_nodes)))

        # undo the tainting
        body = {
            "spec": {
                "taints": []
            }
        }
        k8s.core_v1.patch_node(new_master_node, body)


    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_logical_backup_cron_job(self):
        """
        Ensure we can (a) create the cron job at user request for a specific PG cluster
                      (b) update the cluster-wide image of the logical backup pod
                      (c) delete the job at user request

        Limitations:
        (a) Does not run the actual batch job because there is no S3 mock to upload backups to
        (b) Assumes 'acid-minimal-cluster' exists as defined in setUp
        """

        k8s = K8sApi()

        # create the cron job
        pg_patch_enable_backup = {
            "spec": {
               "enableLogicalBackup" : True,
               "logicalBackupSchedule" :  "7 7 7 7 *"
            }
        }
        k8s.custom_objects_api.patch_namespaced_custom_object("acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_enable_backup)
        Utils.wait_for_logical_backup_job_creation(k8s, self.RETRY_TIMEOUT_SEC)

        # update the cluster-wide image of the logical backup pod
        config_map_patch = {
            "data": {
               "logical_backup_docker_image" : "test-image-name",
            }
        }
        k8s.core_v1.patch_namespaced_config_map("postgres-operator", "default", config_map_patch)

        operator_pod = k8s.core_v1.list_namespaced_pod('default', label_selector="name=postgres-operator").items[0].metadata.name
        k8s.core_v1.delete_namespaced_pod(operator_pod, "default") # restart reloads the conf
        Utils.wait_for_pod_start(k8s, 'name=postgres-operator', self.RETRY_TIMEOUT_SEC)
        #HACK avoid dropping a delete event when the operator pod has the label but is still starting 
        time.sleep(10)

        # delete the logical backup cron job
        pg_patch_disable_backup = {
            "spec": {
               "enableLogicalBackup" : False,
            }
        }
        k8s.custom_objects_api.patch_namespaced_custom_object("acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_disable_backup)
        Utils.wait_for_logical_backup_job_deletion(k8s, self.RETRY_TIMEOUT_SEC)


class K8sApi:

    def __init__(self):

        # https://github.com/kubernetes-client/python/issues/309
        warnings.simplefilter("ignore", ResourceWarning)

        self.config = config.load_kube_config()
        self.k8s_client = client.ApiClient()
        self.core_v1 = client.CoreV1Api()
        self.crd = client.CustomObjectsApi()
        self.apps_v1 = client.AppsV1Api()
        self.custom_objects_api = client.CustomObjectsApi()
        self.batch_v1_beta1 = client.BatchV1beta1Api()


class Utils:

    @staticmethod
    def get_spilo_nodes(k8s_api, pod_labels):
        master_pod_node = ''
        replica_pod_nodes = []
        podsList = k8s_api.core_v1.list_namespaced_pod('default', label_selector=pod_labels)
        for pod in podsList.items:
            if ('spilo-role', 'master') in pod.metadata.labels.items():
                master_pod_node = pod.spec.node_name
            elif ('spilo-role', 'replica') in pod.metadata.labels.items():
                replica_pod_nodes.append(pod.spec.node_name)

        return master_pod_node, replica_pod_nodes

    @staticmethod
    def wait_for_pod_start(k8s_api, pod_labels, retry_timeout_sec):
        pod_phase = 'No pod running'
        while pod_phase != 'Running':
            pods = k8s_api.core_v1.list_namespaced_pod('default', label_selector=pod_labels).items
            if pods:
                pod_phase = pods[0].status.phase
        time.sleep(retry_timeout_sec)

    @staticmethod
    def wait_for_pg_to_scale(k8s_api, number_of_instances, retry_timeout_sec):

        body = {
            "spec": {
                "numberOfInstances": number_of_instances
            }
        }
        _ = k8s_api.crd.patch_namespaced_custom_object("acid.zalan.do",
                                                       "v1", "default", "postgresqls", "acid-minimal-cluster", body)

        labels = 'version=acid-minimal-cluster'
        while Utils.count_pods_with_label(k8s_api, labels) != number_of_instances:
            time.sleep(retry_timeout_sec)

    @staticmethod
    def count_pods_with_label(k8s_api, labels):
        return len(k8s_api.core_v1.list_namespaced_pod('default', label_selector=labels).items)

    @staticmethod
    def wait_for_master_failover(k8s_api, expected_master_nodes, retry_timeout_sec):
        pod_phase = 'Failing over'
        new_master_node = ''
        labels = 'spilo-role=master,version=acid-minimal-cluster'

        while (pod_phase != 'Running') or (new_master_node not in expected_master_nodes):
            pods = k8s_api.core_v1.list_namespaced_pod('default', label_selector=labels).items

            if pods:
                new_master_node = pods[0].spec.node_name
                pod_phase = pods[0].status.phase
        time.sleep(retry_timeout_sec)

    @staticmethod
    def wait_for_logical_backup_job(k8s_api, retry_timeout_sec, expected_num_of_jobs):
        while (len(k8s_api.batch_v1_beta1.list_namespaced_cron_job("default", label_selector="application=spilo").items) != expected_num_of_jobs):
            time.sleep(retry_timeout_sec)

    @staticmethod
    def wait_for_logical_backup_job_deletion(k8s_api, retry_timeout_sec):
        Utils.wait_for_logical_backup_job(k8s_api,  retry_timeout_sec, expected_num_of_jobs = 0)

    @staticmethod
    def wait_for_logical_backup_job_creation(k8s_api, retry_timeout_sec):
        Utils.wait_for_logical_backup_job(k8s_api, retry_timeout_sec, expected_num_of_jobs = 1)

if __name__ == '__main__':
    unittest.main()
