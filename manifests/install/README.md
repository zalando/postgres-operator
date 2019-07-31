# manifests/install

## Build and install

```sh
kustomize build . | kubectl apply -f -
```

## Notes

Contents in `converted` are generated with [helm convert plugin][1]

```sh
helm convert --destination converted ../../charts/postgres-operator
```

`patches.yaml` and `kustomizeconfig.yaml` is to handle the hash suffix appended by kustomize during [ConfigMap generation][2]

[1]: https://github.com/ContainerSolutions/helm-convert
[2]: https://github.com/kubernetes-sigs/kustomize/blob/master/examples/configGeneration.md
