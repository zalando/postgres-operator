#!/bin/bash
kubectl logs $(kubectl get pods -l name=postgres-operator --field-selector status.phase=Running -o jsonpath='{.items..metadata.name}')
