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
./run.sh
```

To run both the build and test step you can invoke `make e2e` from the parent
directory.

## Covered use cases

The current tests are all bundled in [`test_e2e.py`](tests/test_e2e.py):

* support for multiple namespaces
* scale Postgres cluster up and down
* taint-based eviction of Postgres pods
* invoking logical backup cron job
* uniqueness of master pod
* custom service annotations
