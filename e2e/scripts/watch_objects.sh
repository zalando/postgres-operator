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
kubectl get pods -l name=postgres-operator -o jsonpath='{.items..metadata.annotations.step}'
echo
kubectl get pods -l application=spilo -o jsonpath='{.items..spec.containers..image}'
"