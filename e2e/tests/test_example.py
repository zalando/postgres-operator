
import unittest, yaml
from kubernetes import client, config, utils
from pprint import pprint
import subprocess

class SampleTestCase(unittest.TestCase):

    nodes = set(["kind-test-postgres-operator-worker", "kind-test-postgres-operator-worker2", "kind-test-postgres-operator-worker3"])

    @classmethod
    def setUpClass(cls):

        # deploy operator 

        _ = config.load_kube_config()
        k8s_client = client.ApiClient()

        # HACK create_from_yaml fails with multiple object defined within a single file
        # which is exactly the case with RBAC definition
        subprocess.run(["kubectl", "create", "-f", "manifests/operator-service-account-rbac.yaml"])

        for filename in ["configmap.yaml", "postgres-operator.yaml"]:
            path = "manifests/" + filename
            utils.create_from_yaml(k8s_client, path)
        
        #TODO wait until operator pod starts up label ; name=postgres-operator

    @classmethod
    def tearDownClass(cls):
        pass

    def setUp(self):
        self.config = config.load_kube_config()
        self.v1 = client.CoreV1Api()

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
           Each test must restore the original cluster state
           to avoid introducing dependencies between tests
        """
        body = {
            "metadata": {
                "labels": {
                    "lifecycle-status": None # deletes label
                 }
            }
        }
        for node in self.nodes:
            _ = self.v1.patch_node(node, body)

if __name__ == '__main__':
    unittest.main()