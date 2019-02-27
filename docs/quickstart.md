## Prerequisites:

In order to run the Postgres operator locally in minikube you need to install the following tools:

* [minikube](https://github.com/kubernetes/minikube/releases)
* [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/#install-kubectl-binary-via-curl)

Note that you can also use built-in Kubernetes support in the Docker Desktop
for Mac to follow the steps of this tutorial. You would have to replace
`minikube start` and `minikube delete` with your launch actionsfor the Docker
built-in Kubernetes support.

## Local execution

```bash
git clone https://github.com/zalando/postgres-operator.git
cd postgres-operator

minikube start

# start the operator; may take a few seconds
kubectl create -f manifests/configmap.yaml  # configuration
kubectl create -f manifests/operator-service-account-rbac.yaml  # identity and permissions
kubectl create -f manifests/postgres-operator.yaml  # deployment

# create a Postgres cluster
kubectl create -f manifests/minimal-postgres-manifest.yaml

# connect to the Postgres master via psql
# operator creates the relevant k8s secret
export HOST_PORT=$(minikube service acid-minimal-cluster --url | sed 's,.*/,,')
export PGHOST=$(echo $HOST_PORT | cut -d: -f 1)
export PGPORT=$(echo $HOST_PORT | cut -d: -f 2)
export PGPASSWORD=$(kubectl get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
psql -U postgres

# tear down cleanly
minikube delete
```

We have automated starting the operator and submitting the `acid-minimal-cluster` for you:
```bash
cd postgres-operator
./run_operator_locally.sh
```

## Running and testing the operator

The best way to test the operator is to run it in [minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).
Minikube is a tool to run Kubernetes cluster locally.

### Configuration Options

The operator can be configured with the provided ConfigMap (`manifests/configmap.yaml`).
