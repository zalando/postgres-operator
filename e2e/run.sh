#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

readonly cluster_name="postgres-operator-e2e-tests"
readonly operator_image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator" --format "{{.Repository}}:{{.Tag}}"  | head -1)
readonly e2e_test_image=${cluster_name}
readonly kubeconfig_path="/tmp/kind-config-${cluster_name}"


function start_kind(){

  # avoid interference with previous test runs
  if [[ $(kind-linux-amd64 get clusters | grep "^${cluster_name}*") != "" ]]
  then
    kind-linux-amd64 delete cluster --name ${cluster_name}
  fi

  kind-linux-amd64 create cluster --name ${cluster_name} --config ./e2e/kind-cluster-postgres-operator-e2e-tests.yaml
  kind-linux-amd64 load docker-image "${operator_image}" --name ${cluster_name}
  KUBECONFIG="$(kind-linux-amd64 get kubeconfig-path --name=${cluster_name})"
  export KUBECONFIG
}

function set_kind_api_server_ip(){
  # use the actual kubeconfig to connect to the 'kind' API server
  # but update the IP address of the API server to the one from the Docker 'bridge' network
  cp "${KUBECONFIG}" /tmp
  readonly local kind_api_server_port=6443 # well-known in the 'kind' codebase
  readonly local kind_api_server=$(docker inspect --format "{{ .NetworkSettings.IPAddress }}:${kind_api_server_port}" "${cluster_name}"-control-plane)
  sed -i "s/server.*$/server: https:\/\/$kind_api_server/g" "${kubeconfig_path}"
}

function run_tests(){
  docker run --rm --mount type=bind,source="$(readlink -f ${kubeconfig_path})",target=/root/.kube/config -e OPERATOR_IMAGE="${operator_image}" "${e2e_test_image}"
}

function clean_up(){
  unset KUBECONFIG 
  kind-linux-amd64 delete cluster --name ${cluster_name}
  rm -rf ${kubeconfig_path}
}

function main(){

  trap "clean_up" QUIT TERM EXIT

  start_kind
  set_kind_api_server_ip
  run_tests
  exit 0
}

main "$@"
