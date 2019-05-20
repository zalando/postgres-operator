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
        master_pod_node = ''
        new_master_pod_node = ''

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
        podsList = k8s.core_v1.list_namespaced_pod("default", label_selector=labels)
        for pod in podsList.items:
            if ('spilo-role', 'master') in pod.metadata.labels.items():
                master_pod_node = pod.spec.node_name
            elif ('spilo-role', 'replica') in pod.metadata.labels.items():
                new_master_pod_node = pod.spec.node_name

        k8s.core_v1.patch_node(master_pod_node, body)
        Utils.wait_for_master_failover(k8s, new_master_pod_node, self.RETRY_TIMEOUT_SEC)

        self.assertTrue(master_pod_node != new_master_pod_node, "Master on {} did not fail over to {}".format(master_pod_node, new_master_pod_node))

        # undo the tainting
        body = {
            "spec": {
                "taints": []
            }
        }
        k8s.core_v1.patch_node(new_master_pod_node, body)


class K8sApi:

    def __init__(self):

        # https://github.com/kubernetes-client/python/issues/309
        warnings.simplefilter("ignore", ResourceWarning)

        self.config = config.load_kube_config()
        self.k8s_client = client.ApiClient()
        self.core_v1 = client.CoreV1Api()
        self.crd = client.CustomObjectsApi()
        self.apps_v1 = client.AppsV1Api()


class Utils:

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
    def wait_for_master_failover(k8s_api, expected_master_pod_node, retry_timeout_sec):
        pod_phase = 'Failing over'
        new_master_pod_node = ''
        while (pod_phase != 'Running') & (new_master_pod_node != expected_master_pod_node):
            pods = k8s_api.core_v1.list_namespaced_pod('default', label_selector='spilo-role=master,version=acid-minimal-cluster').items
            if pods:
                new_master_pod_node = pods[0].spec.node_name
                pod_phase = pods[0].status.phase
        time.sleep(retry_timeout_sec)


if __name__ == '__main__':
    unittest.main()
