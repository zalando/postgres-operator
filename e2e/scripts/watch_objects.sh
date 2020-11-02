#!/bin/bash

watch -c "
kubectl get postgresql
echo
echo -n 'Rolling upgrade pending: '
kubectl get statefulset -o jsonpath='{.items..metadata.annotations.zalando-postgres-operator-rolling-update-required}'
echo
echo
kubectl get pods -o wide
echo
kubectl get statefulsets
echo
kubectl get deployments
echo
echo
echo 'Step from operator deployment'
kubectl get pods -l name=postgres-operator -o jsonpath='{.items..metadata.annotations.step}'
echo
echo
echo 'Spilo Image in statefulset'
kubectl get pods -l application=spilo -o jsonpath='{.items..spec.containers..image}'
echo
echo
echo 'Queue Status'
kubectl exec -it \$(kubectl get pods -l name=postgres-operator -o jsonpath='{.items..metadata.name}') -- curl localhost:8080/workers/all/status/
echo"