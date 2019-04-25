#!/usr/bin/env python3

from kubernetes import client, config

def main():

    config.load_kube_config()
    v1 = client.CoreV1Api()

    body = {
        "metadata": {
            "labels": {
                "lifecycle-status": "ready",
             }
        }
    }

    nodes = ["kind-test-postgres-operator-worker", "kind-test-postgres-operator-worker2", "kind-test-postgres-operator-worker3"]
    for node in nodes:
        _ = v1.patch_node(node, body)

if __name__ == '__main__':
    main()