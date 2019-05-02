
import unittest
import time
import timeout_decorator
import subprocess
import git

from kubernetes import client, config, utils


class K8sApi:

    def __init__(self):
        self.config = config.load_kube_config()
        self.k8s_client = client.ApiClient()
        self.core_v1 = client.CoreV1Api()
        self.crd = client.CustomObjectsApi()
        self.apps_v1 = client.AppsV1Api()


class SmokeTestCase(unittest.TestCase):
    '''
    Test the most basic e2e functionality of the operator.
    '''

    @classmethod
    def setUpClass(cls):
        '''
        Deploy operator to a "kind" cluster created by /e2e/run.sh using examples from /manifests.
        This operator deployment is to be shared among all tests.

        /e2e/run.sh deletes the 'kind' cluster after successful run along with all operator-related entities.
        In the case of test failure the cluster will stay to enable manual examination;
        next invocation of "make e2e" will re-create it.
        '''

        # HACK
        # 1. creating RBAC entites with a separate client fails
        #    with "AttributeError: object has no attribute 'select_header_accept'"
        # 2. utils.create_from_yaml cannot create multiple entites from a single file
        subprocess.run(["kubectl", "create", "-f", "manifests/operator-service-account-rbac.yaml"])

        k8s_api = K8sApi()

        for filename in ["configmap.yaml", "postgres-operator.yaml"]:
            path = "manifests/" + filename
            utils.create_from_yaml(k8s_api.k8s_client, path)

        # submit most recent operator image built locally; see VERSION in Makefile
        repo = git.Repo(".")
        version = repo.git.describe("--tags", "--always", "--dirty")

        body = {
            "spec": {
                "template": {
                    "spec": {
                        "containers": [
                            {
                              "name": "postgres-operator",
                              "image": "registry.opensource.zalan.do/acid/postgres-operator:" + version
                            }
                        ]
                    }
                 }
            }
        }
        k8s_api.apps_v1.patch_namespaced_deployment("postgres-operator", "default", body)

        pod_phase = None
        while pod_phase != 'Running':
            pods = k8s_api.core_v1.list_namespaced_pod('default', label_selector='name=postgres-operator').items
            if pods:
                operator_pod = pods[0]
                pod_phase = operator_pod.status.phase
            print("Waiting for the operator pod to start. Current phase of pod lifecycle: " + str(pod_phase))
            time.sleep(5)

        # HACK around the lack of Python client for the acid.zalan.do resource
        subprocess.run(["kubectl", "create", "-f", "manifests/minimal-postgres-manifest.yaml"])

        pod_phase = 'None'
        while pod_phase != 'Running':
            pods = k8s_api.core_v1.list_namespaced_pod('default', label_selector='spilo-role=master').items
            if pods:
                operator_pod = pods[0]
                pod_phase = operator_pod.status.phase
            print("Waiting for the Spilo master pod to start. Current phase: " + str(pod_phase))
            time.sleep(5)

    @timeout_decorator.timeout(240)
    def test_master_is_unique(self):
        """
           Check that there is a single pod in the k8s cluster with the label "spilo-role=master".
        """
        k8s = K8sApi()
        labels = 'spilo-role=master,version=acid-minimal-cluster'
        master_pods = k8s.core_v1.list_namespaced_pod('default', label_selector=labels).items
        self.assertEqual(len(master_pods), 1, "Expected 1 master pod, found " + str(len(master_pods)))

    @timeout_decorator.timeout(240)
    def test_scaling(self):
        """
           Scale up from 2 to 3 pods and back to 2 by updating the Postgres manifest at runtime.
        """
        k8s = K8sApi()

        body = {
            "spec": {
                "numberOfInstances": 3
            }
        }
        _ = k8s.crd.patch_namespaced_custom_object("acid.zalan.do",
                                                   "v1", "default", "postgresqls", "acid-minimal-cluster", body)

        labels = 'version=acid-minimal-cluster'
        while len(k8s.core_v1.list_namespaced_pod('default', label_selector=labels).items) != 3:
            print("Waiting for the cluster to scale up to 3 pods.")
            time.sleep(5)
        self.assertEqual(3, len(k8s.core_v1.list_namespaced_pod('default', label_selector=labels).items))

        body = {
            "spec": {
                "numberOfInstances": 2
            }
        }

        _ = k8s.crd.patch_namespaced_custom_object("acid.zalan.do",
                                                   "v1", "default", "postgresqls", "acid-minimal-cluster", body)

        while len(k8s.core_v1.list_namespaced_pod('default', label_selector=labels).items) != 2:
            print("Waiting for the cluster to scale down to 2 pods.")
            time.sleep(5)
        self.assertEqual(2, len(k8s.core_v1.list_namespaced_pod('default', label_selector=labels).items))


if __name__ == '__main__':
    unittest.main()
