#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

kind create cluster --name kind-m --config ./e2e/kind-config-multikind.yaml --loglevel debug
export KUBECONFIG="$(kind get kubeconfig-path --name="kind-m")"
kubectl cluster-info
