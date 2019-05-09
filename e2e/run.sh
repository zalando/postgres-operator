#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

readonly cluster_name="kind-smoke-test-postgres-operator"

# avoid interference with previous test runs
if [[ $(kind get clusters | grep "^${cluster_name}*") != "" ]]
then
  kind delete cluster --name ${cluster_name}
fi

kind create cluster --name ${cluster_name} --config ./e2e/kind-config-smoke-tests.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name=${cluster_name})"
kubectl cluster-info

image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator" --format "{{.Repository}}:{{.Tag}}"  | head -1)
kind load docker-image ${image} --name ${cluster_name}

cp -r ./manifests ./e2e/manifests
cp $KUBECONFIG ./e2e
d=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}:6443" ${cluster_name}-control-plane)
sed -i "s/server.*$/server: https:\/\/$d/g" e2e/kind-config-${cluster_name}

docker build --tag=postgres-operator-e2e-tests -f e2e/Dockerfile . && docker run postgres-operator-e2e-tests
#python3 -m unittest discover --start-directory e2e/tests/ -v &&

kind delete cluster --name ${cluster_name}
rm -rf ./e2e/manifests ./e2e/kind-smoke-test-postgres-operator.yaml
