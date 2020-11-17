# Postgres Operator end-to-end tests

End-to-end tests shall ensure that the Postgres Operator does its job when
applying manifests against a Kubernetes (K8s) environment. A test runner
Dockerfile is provided to run e2e tests without the need to install K8s and
its runtime `kubectl` in advance. The test runner uses
[kind](https://kind.sigs.k8s.io/) to create a local K8s cluster which runs on
Docker.

## Prerequisites

Docker
Go

# Notice

The `manifest` folder in e2e tests folder is not commited to git, it comes from `/manifests`

## Build test runner

In the directory of the cloned Postgres Operator repository change to the e2e
folder and run:

```bash
make
```

This will build the `postgres-operator-e2e-tests` image and download the kind
runtime.

## Run tests

In the e2e folder you can invoke tests either with `make test` or with:

```bash
./run.sh main
```

To run both the build and test step you can invoke `make e2e` from the parent
directory.

To run the end 2 end test and keep the kind state execute:
```bash
NOCLEANUP=True ./run.sh main
```

## Run indidual test

After having executed a normal E2E run with `NOCLEANUP=True` Kind still continues to run, allowing you subsequent test runs.

To run an individual test, run the following command in the `e2e` directory

```bash
NOCLEANUP=True ./run.sh main tests.test_e2e.EndToEndTestCase.test_lazy_spilo_upgrade
```

## Inspecting Kind

If you want to inspect Kind/Kubernetes cluster, switch `kubeconfig` file and context
```bash
# save the old config in case you have it
export KUBECONFIG_SAVED=$KUBECONFIG

# use the one created by e2e tests
export KUBECONFIG=/tmp/kind-config-postgres-operator-e2e-tests

# this kubeconfig defines a single context
kubectl config use-context kind-postgres-operator-e2e-tests
```

or use the following script to exec into the K8s setup and then use `kubectl`

```bash
./exec_into_env.sh

# use kubectl
kubectl get pods

# watch relevant objects
./scripts/watch_objects.sh

# get operator logs
./scripts/get_logs.sh
```

If you want to inspect the state of the `kind` cluster manually with a single command, add a `context` flag
```bash
kubectl get pods --context kind-kind
```
or set the context for a few commands at once



## Cleaning up Kind

To cleanup kind and start fresh

```bash
e2e/run.sh cleanup
```

That also helps in case you see the
```
ERROR: no nodes found for cluster "postgres-operator-e2e-tests"
```
that happens when the `kind` cluster was deleted manually but its configuraiton file was not.

## Covered use cases

The current tests are all bundled in [`test_e2e.py`](tests/test_e2e.py):

* support for multiple namespaces
* scale Postgres cluster up and down
* taint-based eviction of Postgres pods
* invoking logical backup cron job
* uniqueness of master pod
* custom service annotations
