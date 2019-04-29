
import unittest, yaml, time
from kubernetes import client, config, utils
from pprint import pprint
import subprocess

class SampleTestCase(unittest.TestCase):

    nodes = set(["kind-test-postgres-operator-worker", "kind-test-postgres-operator-worker2", "kind-test-postgres-operator-worker3"])

    @classmethod
    def setUpClass(cls):
        '''
        Deploy operator to a "kind" cluster created by /e2e/run.sh using examples from /manifests.
        This operator deployment is to be shared among all tests of this suit.
        '''

        _ = config.load_kube_config()
        k8s_client = client.ApiClient()

        # HACK 
        # 1. creating RBAC entites with a separate client fails with 
        # "AttributeError: object has no attribute 'select_header_accept'"
        # 2. utils.create_from_yaml cannot create multiple entites from a single file
        subprocess.run(["kubectl", "create", "-f", "manifests/operator-service-account-rbac.yaml"])

        for filename in ["configmap.yaml", "postgres-operator.yaml"]:
            path = "manifests/" + filename
            utils.create_from_yaml(k8s_client, path)

        v1 = client.CoreV1Api()
        pod_phase = None
        while pod_phase != 'Running':
            pods = v1.list_namespaced_pod('default', label_selector='name=postgres-operator').items
            if pods:
               operator_pod = pods[0]
               pod_phase = operator_pod.status.phase
               print("Waiting for the operator pod to start. Current phase: " + pod_phase)
            time.sleep(5)

    @classmethod
    def tearDownClass(cls):
        '''
        /e2e/run.sh deletes the 'kind' cluster after successful run along with all operator-related entities.
        In the case of test failure the cluster will stay to enable manual examination;
        next invocation of "make e2e" will re-create it.
        '''
        pass

    def setUp(self):
        '''
        Deploy a new Postgres DB for each test.
        '''
        self.config = config.load_kube_config()
        self.v1 = client.CoreV1Api()

        k8s_client = client.ApiClient()

        # TODO substitue with utils.create_from_yaml and Python client  for acid.zalan.do
        subprocess.run(["kubectl", "create", "-f", "manifests/minimal-postgres-manifest.yaml"])

        pod_phase = None
        while pod_phase != 'Running':
            pods = self.v1.list_namespaced_pod('default', label_selector='spilo-role=master').items
            if pods:
               operator_pod = pods[0]
               pod_phase = operator_pod.status.phase
               print("Waiting for the Spilo master pod to start. Current phase: " + pod_phase)
               time.sleep(5)

    def test_assign_labels_to_nodes(self):
        """
           Ensure labeling nodes through the externally connected Python client works.
           Sample test case to illustrate potential test structure
        """
        body = {
            "metadata": {
                "labels": {
                    "lifecycle-status": "ready"
                 }
            }
        }
        for node in self.nodes:
            _ = self.v1.patch_node(node, body)

        labelled_nodes = set([])
        for node in self.nodes:
            v1_node_var = self.v1.read_node(node)
            if v1_node_var.metadata.labels['lifecycle-status'] == 'ready':
                labelled_nodes.add(v1_node_var.metadata.name)

        self.assertEqual(self.nodes, labelled_nodes,"nodes incorrectly labelled")

    def tearDown(self):
        """
           Delete the database to avoid introducing dependencies between tests
        """
        # HACK workaround for #551
        time.sleep(60)

        _ = config.load_kube_config()
        crd = client.CustomObjectsApi()
        body = client.V1DeleteOptions()
        _ = crd.delete_namespaced_custom_object("acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", body) 

        # wait for the pods to be deleted
        pods = self.v1.list_namespaced_pod('default', label_selector='spilo-role=master').items
        while pods:
            pods = self.v1.list_namespaced_pod('default', label_selector='spilo-role=master').items
            print("Waiting for the database to be deleted.")
            time.sleep(5)

if __name__ == '__main__':
    unittest.main()