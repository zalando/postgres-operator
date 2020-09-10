#!/usr/bin/env bash
kubectl exec -it $1 -- sh -c "$2"
