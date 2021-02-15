# Kubectl Plugin for Zalando's Postgres Operator

This plugin is a prototype developed as a part of **Google Summer of Code 2019** under the [Postgres Operator](https://summerofcode.withgoogle.com/archive/2019/organizations/6187982082539520/) organization.

## Installation of kubectl pg plugin

This project uses Go Modules for dependency management to build locally.
Install go and enable go modules with ```export GO111MODULE=on```.
From Go >=1.13 Go modules will be enabled by default.

```bash
# Assumes you have a working KUBECONFIG
$ GO111MODULE="on"
$ GOPATH/src/github.com/zalando/postgres-operator/kubectl-pg  go mod vendor
# This generate a vendor directory with all dependencies needed by the plugin.
$ $GOPATH/src/github.com/zalando/postgres-operator/kubectl-pg  go install
# This will place the kubectl-pg binary in your $GOPATH/bin
```

## Before using the kubectl pg plugin make sure to set KUBECONFIG env variable

Ideally KUBECONFIG is found in $HOME/.kube/config else specify the KUBECONFIG path here.
```export KUBECONFIG=$HOME/.kube/config```

## List all commands available in kubectl pg

```kubectl pg --help``` (or) ```kubectl pg```

## Check if Postgres Operator is installed and running

```kubectl pg check```

## Create a new cluster using manifest file

```kubectl pg create -f acid-minimal-cluster.yaml```

## List postgres resources

```kubectl pg list```

List clusters across namespaces
```kubectl pg list all```

## Update existing cluster using manifest file

```kubectl pg update -f acid-minimal-cluster.yaml```

## Delete existing cluster

Using the manifest file:
```kubectl pg delete -f acid-minimal-cluster.yaml```

Or by specifying the cluster name:
```kubectl pg delete acid-minimal-cluster```

Use `--namespace` or `-n` flag if your cluster is in a different namespace to where your current context is pointing to:
```kubectl pg delete acid-minimal-cluster -n namespace01```

## Adding manifest roles to an existing cluster

```kubectl pg add-user USER01 -p CREATEDB,LOGIN -c acid-minimal-cluster```

Privileges can only be [SUPERUSER, REPLICATION, INHERIT, LOGIN, NOLOGIN, CREATEROLE, CREATEDB, BYPASSURL]
Note: By default, a LOGIN user is created (unless NOLOGIN is specified).

## Adding databases to an existing cluster

You have to specify an owner of the new database and this role must already exist in the cluster:
```kubectl pg add-db DB01 -o OWNER01 -c acid-minimal-cluster```

## Extend the volume of an existing pg cluster

```kubectl pg ext-volume 2Gi -c acid-minimal-cluster```

## Print the version of Postgres Operator and kubectl pg plugin

```kubectl pg version```

## Connect to the shell of a postgres pod

Connect to the master pod:
```kubectl pg connect -c CLUSTER -m```

Connect to a random replica pod:
```kubectl pg connect -c CLUSTER```

Connect to a certain replica pod:
```kubectl pg connect -c CLUSTER -r 0```

## Connect to a database via psql

Adding the `-p` flag allows you to directly connect to a given database with the psql client.
With `-u` you specify the user. If left out the name of the current OS user is taken.
`-d` lets you specify the database. If no database is specified, it will be the same as the user name.

Connect to `app_db` database on the master with role `app_user`:
```kubectl pg connect -c CLUSTER -m -p -u app_user -d app_db```

Connect to the `postgres` database on a random replica with role `postgres`:
```kubectl pg connect -c CLUSTER -p -u postgres```

Connect to a certain replica assuming name of OS user, database role and name are all the same:
```kubectl pg connect -c CLUSTER -r 0 -p```


## Access Postgres Operator logs

```kubectl pg logs -o```

## Access Patroni logs of different database pods

Fetch logs of master:
```kubectl pg logs -c CLUSTER -m```

Fetch logs of a random replica pod:
```kubectl pg logs -c CLUSTER```

Fetch logs of specified replica
```kubectl pg logs -c CLUSTER -r 2```

## Development

When making changes to the plugin make sure to change the (major/patch) version of plugin in `build.sh` script and run `./build.sh`.

## Google Summer of Code 2019

### GSoC Proposal

[kubectl pg proposal](https://docs.google.com/document/d/1-WMy9HkfZ1XnnMbzplMe9rCzKrRMGaMz4owLVXXPb7w/edit)

### Weekly Reports

https://github.com/VineethReddy02/GSoC-Kubectl-Plugin-for-Postgres-Operator-tracker

### Final Project Report

https://gist.github.com/VineethReddy02/159283bd368a710379eaf0f6bd60a40a
