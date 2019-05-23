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

    # `kind` pods may stuck in the `Terminating` phase for a few minutes; hence high test timeout
    TEST_TIMEOUT_SEC = 600
    # labels may be assigned before a pod becomes fully operational; so wait a few seconds more
    OPERATOR_POD_START_PERIOD_SEC = 5

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

        # set a single k8s wrapper for all tests
        k8s = cls.k8s = K8s()

        # k8s python client fails with multiple resources in a single file; we resort to kubectl
        subprocess.run(["kubectl", "create", "-f", "manifests/operator-service-account-rbac.yaml"])

        for filename in ["configmap.yaml", "postgres-operator.yaml"]:
            utils.create_from_yaml(k8s.api.k8s_client, "manifests/" + filename)

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
        k8s.api.apps_v1.patch_namespaced_deployment("postgres-operator", "default", body)

        k8s.wait_for_pod_start('name=postgres-operator')
        # reason: CRD may take time to register
        time.sleep(cls.OPERATOR_POD_START_PERIOD_SEC) 

        actual_operator_image = k8s.api.core_v1.list_namespaced_pod('default', label_selector='name=postgres-operator').items[0].spec.containers[0].image
        print("Tested operator image: {}".format(actual_operator_image)) # shows up after tests finish

        subprocess.run(["kubectl", "create", "-f", "manifests/minimal-postgres-manifest.yaml"])
        k8s.wait_for_pod_start('spilo-role=master')


    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def master_is_unique(self):
        """
           Check that there is a single pod in the k8s cluster with the label "spilo-role=master".
        """

        k8s = self.k8s
        labels = 'spilo-role=master,version=acid-minimal-cluster'

        num_of_master_pods = k8s.count_pods_with_label(labels)
        self.assertEqual(num_of_master_pods, 1, "Expected 1 master pod, found {}".format(num_of_master_pods))

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def scaling(self):
        """
           Scale up from 2 to 3 pods and back to 2 by updating the Postgres manifest at runtime.
        """

        k8s = self.k8s
        labels = "version=acid-minimal-cluster"

        k8s.wait_for_pg_to_scale(3)
        self.assertEqual(3, k8s.count_pods_with_label(labels))

        k8s.wait_for_pg_to_scale(2)
        self.assertEqual(2, k8s.count_pods_with_label(labels))

    @timeout_decorator.timeout(TEST_TIMEOUT_SEC)
    def test_taint_based_eviction(self):
        """
           Add taint "postgres=:NoExecute" to node with master.
        """
        k8s = self.k8s
        labels = 'version=acid-minimal-cluster'

        # get nodes of master and replica(s) (expected target of new master)
        current_master_node, failover_targets = k8s.get_spilo_nodes(labels)
        num_replicas = len(failover_targets)

        # if all pods live on the same node, failover will happen to other worker(s)
        failover_targets = [x for x in failover_targets if x != current_master_node]
        if len(failover_targets) == 0:
            nodes = k8s.api.core_v1.list_node()
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
        k8s.api.core_v1.patch_node(current_master_node, body)
        k8s.wait_for_master_failover(failover_targets)
        k8s.wait_for_pod_start('spilo-role=replica')

        new_master_node, new_replica_nodes = k8s.get_spilo_nodes(labels)
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
        k8s.api.core_v1.patch_node(new_master_node, body)


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

        k8s = self.k8s

        # create the cron job
        schedule = "7 7 7 7 *"
        pg_patch_enable_backup = {
            "spec": {
               "enableLogicalBackup" : True,
               "logicalBackupSchedule" : schedule
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object("acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_enable_backup)
        k8s.wait_for_logical_backup_job_creation()
        
        jobs = k8s.get_logical_backup_job().items
        self.assertTrue(1 == len(jobs),
                       "Expected 1 logical backup job, found {}".format(len(jobs)))

        job = jobs[0]
        self.assertTrue(job.metadata.name == "logical-backup-acid-minimal-cluster",
        "Expected job name {}, found {}".format("logical-backup-acid-minimal-cluster", job.metadata.name))
        self.assertTrue(job.spec.schedule == schedule,
        "Expected {} schedule, found {}".format(schedule, job.spec.schedule))

        # update the cluster-wide image of the logical backup pod
        image = "test-image-name"
        config_map_patch = {
            "data": {
               "logical_backup_docker_image" : image,
            }
        }
        k8s.api.core_v1.patch_namespaced_config_map("postgres-operator", "default", config_map_patch)

        operator_pod = k8s.api.core_v1.list_namespaced_pod('default', label_selector="name=postgres-operator").items[0].metadata.name
        k8s.api.core_v1.delete_namespaced_pod(operator_pod, "default") # restart reloads the conf
        k8s.wait_for_pod_start('name=postgres-operator')
        # reason: patch below is otherwise dropped during pod restart
        time.sleep(self.OPERATOR_POD_START_PERIOD_SEC) 

        jobs = k8s.get_logical_backup_job().items
        actual_image = jobs[0].spec.job_template.spec.template.spec.containers[0].image
        self.assertTrue(actual_image == image,
        "Expected job image {}, found {}".format(image, actual_image))

        # delete the logical backup cron job
        pg_patch_disable_backup = {
            "spec": {
               "enableLogicalBackup" : False,
            }
        }
        k8s.api.custom_objects_api.patch_namespaced_custom_object("acid.zalan.do", "v1", "default", "postgresqls", "acid-minimal-cluster", pg_patch_disable_backup)
        k8s.wait_for_logical_backup_job_deletion()
        jobs = k8s.get_logical_backup_job().items
        self.assertTrue(0 == len(jobs),
                       "Expected 0 logical backup jobs, found {}".format(len(jobs)))


class K8sApi:

    def __init__(self):

        # https://github.com/kubernetes-client/python/issues/309
        warnings.simplefilter("ignore", ResourceWarning)

        self.config = config.load_kube_config()
        self.k8s_client = client.ApiClient()

        self.crd = client.CustomObjectsApi()
        self.core_v1 = client.CoreV1Api()
        self.apps_v1 = client.AppsV1Api()
        self.batch_v1_beta1 = client.BatchV1beta1Api()
        self.custom_objects_api = client.CustomObjectsApi()



class K8s:

    RETRY_TIMEOUT_SEC = 5

    def __init__(self):
        self.api = K8sApi()

    def get_spilo_nodes(self, pod_labels):
        master_pod_node = ''
        replica_pod_nodes = []
        podsList = self.api.core_v1.list_namespaced_pod('default', label_selector=pod_labels)
        for pod in podsList.items:
            if ('spilo-role', 'master') in pod.metadata.labels.items():
                master_pod_node = pod.spec.node_name
            elif ('spilo-role', 'replica') in pod.metadata.labels.items():
                replica_pod_nodes.append(pod.spec.node_name)

        return master_pod_node, replica_pod_nodes

    def wait_for_pod_start(self, pod_labels):
        pod_phase = 'No pod running'
        while pod_phase != 'Running':
            pods = self.api.core_v1.list_namespaced_pod('default', label_selector=pod_labels).items
            if pods:
                pod_phase = pods[0].status.phase
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_pg_to_scale(self, number_of_instances):

        body = {
            "spec": {
                "numberOfInstances": number_of_instances
            }
        }
        _ = self.api.crd.patch_namespaced_custom_object("acid.zalan.do",
                                                       "v1", "default", "postgresqls", "acid-minimal-cluster", body)

        labels = 'version=acid-minimal-cluster'
        while self.count_pods_with_label(labels) != number_of_instances:
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def count_pods_with_label(self, labels):
        return len(self.api.core_v1.list_namespaced_pod('default', label_selector=labels).items)

    def wait_for_master_failover(self, expected_master_nodes):
        pod_phase = 'Failing over'
        new_master_node = ''
        labels = 'spilo-role=master,version=acid-minimal-cluster'

        while (pod_phase != 'Running') or (new_master_node not in expected_master_nodes):
            pods = self.api.core_v1.list_namespaced_pod('default', label_selector=labels).items

            if pods:
                new_master_node = pods[0].spec.node_name
                pod_phase = pods[0].status.phase
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def get_logical_backup_job(self):
        return self.api.batch_v1_beta1.list_namespaced_cron_job("default", label_selector="application=spilo")

    def wait_for_logical_backup_job(self, expected_num_of_jobs):
        while (len(self.api.get_logical_backup_job().items) != expected_num_of_jobs):
            time.sleep(self.RETRY_TIMEOUT_SEC)

    def wait_for_logical_backup_job_deletion(self):
        Utils.wait_for_logical_backup_job(expected_num_of_jobs = 0)

    def wait_for_logical_backup_job_creation(self):
        Utils.wait_for_logical_backup_job(expected_num_of_jobs = 1)
    
    def create_with_kubectl(self, path):
        subprocess.run(["kubectl", "create", "-f", path])

if __name__ == '__main__':
    unittest.main()
