# kubectl-pg

### Installtion of kubectl pg plugin

Clone the kubectl pg plugin and build from the source using ```go install``` this will generate kubectl pg plugin's executable in ```go/bin/kubectl-pg.exe```

### To list all commands avaialble in kubectl pg

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