#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

readonly cluster_name="kind-test-postgres-operator"

# avoid interference with previous test runs
if [[ $(kind get clusters | grep "^${cluster_name}*") != "" ]]
then
  kind delete cluster --name ${cluster_name}
fi

kind create cluster --name ${cluster_name} --config ./e2e/kind-config-multikind.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name=${cluster_name})"
kubectl cluster-info

python3 -m unittest discover --start-directory e2e/tests/ &&
kind delete cluster --name ${cluster_name}