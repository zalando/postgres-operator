#!/bin/bash

watch -c "
kubectl get postgresql --all-namespaces
echo
echo -n 'Rolling upgrade pending: '
kubectl get statefulset -o jsonpath='{.items..metadata.annotations.zalando-postgres-operator-rolling-update-required}'
echo
echo
echo 'Pods'
kubectl get pods -l application=spilo -l name=postgres-operator -l application=db-connection-pooler -o wide --all-namespaces
echo
echo 'Statefulsets'
kubectl get statefulsets --all-namespaces
echo
echo 'Deployments'
kubectl get deployments --all-namespaces -l application=db-connection-pooler -l name=postgres-operator
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