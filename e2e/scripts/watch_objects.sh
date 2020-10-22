#!/bin/bash

watch -c "
kubectl get postgresql
echo
kubectl get pods
echo
kubectl get statefulsets
"