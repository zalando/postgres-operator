# Quickstart

## Prerequisites

The Postgres Operator runs on Kubernetes (K8s) which you have to setup first.
For local tests we recommend to use solutions such as [minikube](https://github.com/kubernetes/minikube/releases)
or [kind](https://kind.sigs.k8s.io/). This quickstart guide uses minikube.
To interact with the K8s infrastructure install it's CLI runtime [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/#install-kubectl-binary-via-curl).

Note that you can also use built-in Kubernetes support in the Docker Desktop
for Mac to follow the steps of this tutorial. You would have to replace
`minikube start` and `minikube delete` with your launch actions for the Docker
built-in Kubernetes support.

Clone the repository and change to the directory. Then start minikube.

```bash
git clone https://github.com/zalando/postgres-operator.git
cd postgres-operator

minikube start
```

## Configuration Options

If you want to configure the Postgres Operator it must happen before deploying a
Postgres cluster. This can happen in two ways: Via a ConfigMap or a custom
`OperatorConfiguration` object. More details on configuration can be found
[here](reference/operator_parameters.md).

## Manual deployment setup

The Postgres Operator can be installed simply by applying yaml manifests.

```bash
kubectl create -f manifests/configmap.yaml  # configuration
kubectl create -f manifests/operator-service-account-rbac.yaml  # identity and permissions
kubectl create -f manifests/postgres-operator.yaml  # deployment
```

## Helm chart

Alternatively, the operator can be installed by using the provided [Helm](https://helm.sh/)
chart which saves you the manual steps. Therefore, you would need to install
the helm CLI on your machine. After initializing helm (and its server
component Tiller) in your local cluster you can install the operator chart.
You can define a release name that is prepended to the operator resource's
names.

Use `--name zalando` to match with the default service account name as older
operator versions do not support custom names for service accounts. When relying
solely on the CRD-based configuration edit the `serviceAccount` section in the
[values yaml file](../charts/values.yaml) by setting the name to `"operator"`.

```bash
# 1) initialize helm
helm init
# 2) install postgres-operator chart
helm install --name zalando ./charts/postgres-operator
```

## Create a Postgres cluster

Starting the operator may take a few seconds. Check if the operator pod is
running before applying a Postgres cluster manifest.

```bash
# if you've created the operator using yaml manifests
kubectl get pod -l name=postgres-operator

# if you've created the operator using helm chart
kubectl get pod -l app.kubernetes.io/name=postgres-operator

# create a Postgres cluster
kubectl create -f manifests/minimal-postgres-manifest.yaml
```

After the cluster manifest is submitted the operator will create Service and
Endpoint resources and a StatefulSet which spins up new Pod(s) given the number
of instances specified in the manifest. All resources are named like the
cluster. The database pods can be identified by their number suffix, starting
from `-0`. They run the [Spilo](https://github.com/zalando/spilo) container
image by Zalando. As for the services and endpoints, there will be one for the
master pod and another one for all the replicas (`-repl` suffix). Check if all
components are coming up. Use the label `application=spilo` to filter and list
the label `spilo-role` to see who is currently the master.

```bash
# check the deployed cluster
kubectl get postgresql

# check created database pods
kubectl get pods -l application=spilo -L spilo-role

# check created service resources
kubectl get svc -l application=spilo -L spilo-role
```

## Connect to the Postgres cluster via psql

You can retrieve the host and port of the Postgres master from minikube.
Retrieve the password from the Kubernetes Secret that is created in your cluster.

```bash
export HOST_PORT=$(minikube service acid-minimal-cluster --url | sed 's,.*/,,')
export PGHOST=$(echo $HOST_PORT | cut -d: -f 1)
export PGPORT=$(echo $HOST_PORT | cut -d: -f 2)
export PGPASSWORD=$(kubectl get secret postgres.acid-minimal-cluster.credentials -o 'jsonpath={.data.password}' | base64 -d)
psql -U postgres
```

## Delete a Postgres cluster

To delete a Postgres cluster simply delete the postgresql custom resource.

```bash
kubectl delete postgresql acid-minimal-cluster

# tear down cleanly
minikube delete
```

## Running and testing the operator

The best way to test the operator is to run it in [minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).
Minikube is a tool to run Kubernetes cluster locally.

For convenience, we have automated starting the operator and submitting the
`acid-minimal-cluster`. From inside the cloned repository execute the
`run_operator_locally` shell script.

```bash
./run_operator_locally.sh
```

Note we provide the `/manifests` directory as an example only; you should
consider adjusting the manifests to your particular setting.
