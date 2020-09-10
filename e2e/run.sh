#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

readonly cluster_name="postgres-operator-e2e-tests"
readonly kubeconfig_path="/tmp/kind-config-${cluster_name}"
readonly spilo_image="registry.opensource.zalan.do/acid/spilo-12:1.6-p5"

echo "Clustername: ${cluster_name}"
echo "Kubeconfig path: ${kubeconfig_path}"

function pull_images(){
  operator_tag=$(git describe --tags --always --dirty)
  if [[ -z $(docker images -q registry.opensource.zalan.do/acid/postgres-operator:${operator_tag}) ]]
  then
    docker pull registry.opensource.zalan.do/acid/postgres-operator:latest
  fi

  operator_image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator" --format "{{.Repository}}:{{.Tag}}" | head -1)

  # this image does not contain the tests; a container mounts them from a local "./tests" dir at start time
  e2e_test_runner_image="registry.opensource.zalan.do/acid/postgres-operator-e2e-tests-runner:latest"
  docker pull ${e2e_test_runner_image}
}

function start_kind(){
  echo "Starting kind for e2e tests"
  # avoid interference with previous test runs
  if [[ $(kind get clusters | grep "^${cluster_name}*") != "" ]]
  then
    kind delete cluster --name ${cluster_name}
  fi

  export KUBECONFIG="${kubeconfig_path}"
  kind create cluster --name ${cluster_name} --config kind-cluster-postgres-operator-e2e-tests.yaml
  kind load docker-image "${operator_image}" --name ${cluster_name}
  docker pull "${spilo_image}"
  kind load docker-image "${spilo_image}" --name ${cluster_name}
}

function set_kind_api_server_ip(){
  echo "Setting up kind API server ip"
  # use the actual kubeconfig to connect to the 'kind' API server
  # but update the IP address of the API server to the one from the Docker 'bridge' network
  readonly local kind_api_server_port=6443 # well-known in the 'kind' codebase
  readonly local kind_api_server=$(docker inspect --format "{{ .NetworkSettings.Networks.kind.IPAddress }}:${kind_api_server_port}" "${cluster_name}"-control-plane)
  sed -i "s/server.*$/server: https:\/\/$kind_api_server/g" "${kubeconfig_path}"
}

function run_tests(){
  echo "Running tests..."

  # tests modify files in ./manifests, so we mount a copy of this directory done by the e2e Makefile

  docker run --rm --network=host -e "TERM=xterm-256color" \
  --mount type=bind,source="$(readlink -f ${kubeconfig_path})",target=/root/.kube/config \
  --mount type=bind,source="$(readlink -f manifests)",target=/manifests \
  --mount type=bind,source="$(readlink -f tests)",target=/tests \
  --mount type=bind,source="$(readlink -f exec.sh)",target=/exec.sh \
  -e OPERATOR_IMAGE="${operator_image}" "${e2e_test_runner_image}"
  
}

function clean_up(){
  echo "Executing cleanup"
  unset KUBECONFIG
  kind delete cluster --name ${cluster_name}
  rm -rf ${kubeconfig_path}
}

function main(){

  trap "clean_up" QUIT TERM EXIT

  time pull_images
  time start_kind
  time set_kind_api_server_ip
  run_tests
  exit 0
}

"$@"
