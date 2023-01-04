#!/usr/bin/env bash
kubectl exec -i $1 -- sh -c "$2"
