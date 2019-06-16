# kubectl-pg

### Installtion of kubectl pg plugin

Clone the kubectl pg plugin and build from the source using ```go install``` this will generate kubectl pg plugin's executable in ```go/bin/kubectl-pg.exe```

### To list all commands avaialble in kubectl pg

```kubectl pg --help``` (or) ```kubectl pg```

### To validate postgresql CRD creation

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

### To add-user and roles to existing cluster

```kubectl pg add-user USER01 -p CREATE,UPDATE,DELETE -c acid-minimal-cluster```

### To add-db and assigning owner to existing cluster

```kubectl pg add-db DB01 -o OWNER01 -c acid-minimal-cluster```

### To extend volume for an existing cluster

```kubectl pg ext-volume 2Gi -c acid-minimal-cluster```