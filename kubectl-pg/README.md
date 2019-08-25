# Kubectl Plugin for Zalando's Postgres Operator

## Google Summer of Code 2019

This plugin is a prototype developed as a part of GSoC 2019 under the organisation
**The Postgres Operator**

### GSoC Proposal

[kubectl pg proposal](https://docs.google.com/document/d/1-WMy9HkfZ1XnnMbzplMe9rCzKrRMGaMz4owLVXXPb7w/edit)

### Weekly Reports

https://github.com/VineethReddy02/GSoC-Kubectl-Plugin-for-Postgres-Operator-tracker

 ### Final Project Report
 
 https://gist.github.com/VineethReddy02/159283bd368a710379eaf0f6bd60a40a
 

### Installtion of kubectl pg plugin

This project uses Go Modules for dependency management to build locally
Install go and enable go modules ```export GO111MODULE=on```
From Go >=1.13 Go modules will be enabled by default
```
# Assumes you have a working KUBECONFIG
$ GO111MODULE="on" 
# As of now go by default doesn't support Go mods. So explicit enabling is required.
$ GOPATH/src/github.com/zalando/postgres-operator/kubectl-pg  go mod vendor 
# This generate a vendor directory with all dependencies needed by the plugin.
$ $GOPATH/src/github.com/zalando/postgres-operator/kubectl-pg  go install
# This will place the kubectl-pg binary in your $GOPATH/bin
```

### Before using the kubectl pg plugin make sure to set KUBECONFIG env varibale

Ideally KUBECONFIG is found in $HOME/.kube/config else specify the KUBECONFIG path here.

```export KUBECONFIG=$HOME/.kube/config``` 

### To list all commands available in kubectl pg

```kubectl pg --help``` (or) ```kubectl pg```

### This basically means the operator pod managed to start, so our operator is installed.

```kubectl pg check```

### To create postgresql cluster using manifest file

```kubectl pg create -f acid-minimal-cluster.yaml```

### To update existing cluster using manifest file

```kubectl pg update -f acid-minimal-cluster.yaml```

### To delete existing cluster using manifest file

```kubectl pg delete -f acid-minimal-cluster.yaml```

### To delete existing cluster using cluster name

```kubectl pg delete acid-minimal-cluster```

--namespace or -n flag to specify namespace if cluster is in other namespace.

```kubectl pg delete acid-minimal-cluster -n namespace01```

### To list postgres resources for the current namespace

```kubectl pg list```

### To list postgres resources across namespaces

```kubectl pg list all```

### To add-user and it's roles to an existing pg cluster

```kubectl pg add-user USER01 -p CREATEDB,LOGIN -c acid-minimal-cluster```

Privileges can only be [SUPERUSER, REPLICATION, INHERIT, LOGIN, NOLOGIN, CREATEROLE, CREATEDB, BYPASSURL]

Note: A login user is created by default unless NOLOGIN is specified, in which case the operator creates a role.

### To add-db and it's owner to an existing pg cluster

```kubectl pg add-db DB01 -o OWNER01 -c acid-minimal-cluster```

### To extend volume for an existing pg cluster

```kubectl pg ext-volume 2Gi -c acid-minimal-cluster```

### To find the version of postgres operator and kubectl plugin

```kubectl pg version (optional -n NAMESPACE allows to know specific to a namespace)```

### To connect to the shell of a postgres pod

```kubectl pg connect -c CLUSTER``` #This connects to a random pod
```kubectl pg connect -c CLUSTER -m``` #This connects the master
```kubectl pg connect -c CLUSTER -r 0``` #This connects to the desired replica

### To connect to the psql prompt

```kubectl pg connect -c CLUSTER -p -u username``` #This connects to a random pod. Not master
```kubectl pg connect -c CLUSTER -m -p -u username``` #This connects the master
```kubectl pg connect -c CLUSTER -r 0 -p -u username``` #This connects to the desired replica

Note: -p represents psql prompt

### To get the logs of postgres operator

```kubectl pg logs -o```

### To get the logs of the postgres cluster

```kubectl pg logs -c CLUSTER``` #Fetches the logs of a random pod. Not master
```kubectl pg logs -c CLUSTER -m``` #Fetches the logs of master
```kubectl pg logs -c CLUSTER -r 2``` #Fecthes the logs of specified replica

## Development

- When making changes to plugin make sure to change the major or patch version
of plugin in ```build.sh``` and run ```./build.sh``` 
