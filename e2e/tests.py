#!/usr/bin/env python3

import unittest
from kubernetes import client, config
from pprint import pprint

class SampleTestCase(unittest.TestCase):

    nodes = set(["kind-test-postgres-operator-worker", "kind-test-postgres-operator-worker2", "kind-test-postgres-operator-worker3"])

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

        assert self.nodes == labelled_nodes, "nodes incorrectly labelled"

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