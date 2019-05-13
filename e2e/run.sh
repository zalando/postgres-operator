#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

export PATH=$PATH:/tmp/kind
echo $PATH
ls -al /tmp/kind

readonly cluster_name="postgres-operator-e2e-tests"
readonly operator_image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator" --format "{{.Repository}}:{{.Tag}}"  | head -1)
readonly e2e_test_image=${cluster_name}
readonly kind_api_server_port=6443 # well-known in the 'kind' codebase
readonly kubeconfig_path="./e2e/kind-config-${cluster_name}"

# avoid interference with previous test runs
if [[ $(kind get clusters | grep "^${cluster_name}*") != "" ]]
then
  kind delete cluster --name ${cluster_name}
fi

kind create cluster --name ${cluster_name} --config ./e2e/kind-cluster-postgres-operator-e2e-tests.yaml
export KUBECONFIG="$(kind get kubeconfig-path --name=${cluster_name})"

kind load docker-image ${operator_image} --name ${cluster_name}

# use the actual kubeconfig to connect to the 'kind' API server
# but update the IP address of the API server to the one from the Docker 'bridge' network
cp $KUBECONFIG ./e2e
kind_api_server=$(docker inspect --format "{{ .NetworkSettings.IPAddress }}:${kind_api_server_port}" ${cluster_name}-control-plane)
sed -i "s/server.*$/server: https:\/\/$kind_api_server/g" ${kubeconfig_path}

docker run --rm --mount type=bind,source="$(realpath ${kubeconfig_path})",target=/root/.kube/config -e OPERATOR_IMAGE=${operator_image} ${e2e_test_image}

kind delete cluster --name ${cluster_name}
rm -rf ${kubeconfig_path}
