#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

cd $(dirname "$0");

readonly cluster_name="postgres-operator-e2e-tests"
readonly darwin_os_str="Darwin"
readonly uname_str="$(uname)"
readonly kubeconfig_path="/tmp/kind-config-${cluster_name}"

os_type='linux'
readlink_cmd="readlink"

if [[ $uname_str == $darwin_os_str ]]
then
  os_type='darwin'
  readlink_cmd="greadlink"
fi

function pull_images(){

  operator_tag=$(git describe --tags --always --dirty)
  if [[ -z $(docker images -q registry.opensource.zalan.do/acid/postgres-operator:${operator_tag}) ]]
  then
    docker pull registry.opensource.zalan.do/acid/postgres-operator:latest
  fi
  if [[ -z $(docker images -q registry.opensource.zalan.do/acid/postgres-operator-e2e-tests:${operator_tag}) ]]
  then
    docker pull registry.opensource.zalan.do/acid/postgres-operator-e2e-tests:latest
  fi

  operator_image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator" --format "{{.Repository}}:{{.Tag}}" | head -1)
  e2e_test_image=$(docker images --filter=reference="registry.opensource.zalan.do/acid/postgres-operator-e2e-tests" --format "{{.Repository}}:{{.Tag}}" | head -1)
}

function start_kind(){

  # avoid interference with previous test runs
<<<<<<< HEAD
  if [[ $(kind get clusters | grep "^${cluster_name}*") != "" ]]
  then
    kind delete cluster --name ${cluster_name}
  fi

  kind create cluster --name ${cluster_name} --config kind-cluster-postgres-operator-e2e-tests.yaml
  kind load docker-image "${operator_image}" --name ${cluster_name}
  kind load docker-image "${e2e_test_image}" --name ${cluster_name}
  KUBECONFIG="$(kind get kubeconfig-path --name=${cluster_name})"
=======
  if [[ $(kind-${os_type}-amd64 get clusters | grep "^${cluster_name}*") != "" ]]
  then
    kind-${os_type}-amd64 delete cluster --name ${cluster_name}
  fi

  kind-${os_type}-amd64 create cluster --name ${cluster_name} --config kind-cluster-postgres-operator-e2e-tests.yaml
  kind-${os_type}-amd64 load docker-image "${operator_image}" --name ${cluster_name}
  kind-${os_type}-amd64 load docker-image "${e2e_test_image}" --name ${cluster_name}
  KUBECONFIG="$(kind-${os_type}-amd64 get kubeconfig-path --name=${cluster_name})"
>>>>>>> f2d06d8... Made Makefile and run.sh compatible with MacOS
  export KUBECONFIG
}

function set_kind_api_server_ip(){
  # use the actual kubeconfig to connect to the 'kind' API server
  # but update the IP address of the API server to the one from the Docker 'bridge' network
  cp "${KUBECONFIG}" /tmp
  readonly local kind_api_server_port=6443 # well-known in the 'kind' codebase
  readonly local kind_api_server=$(docker inspect --format "{{ .NetworkSettings.IPAddress }}:${kind_api_server_port}" "${cluster_name}"-control-plane)
  
  if [[ $os_type == 'darwin' ]]
  then
    sed -i.bak "s/server.*$/server: https:\/\/$kind_api_server/g" "${kubeconfig_path}"
  else
    sed -i "s/server.*$/server: https:\/\/$kind_api_server/g" "${kubeconfig_path}"
  fi

}

function run_tests(){

  docker run --rm --mount type=bind,source="$(${readlink_cmd} -f ${kubeconfig_path})",target=/root/.kube/config -e OPERATOR_IMAGE="${operator_image}" "${e2e_test_image}"
}

function clean_up(){
  unset KUBECONFIG
<<<<<<< HEAD
  kind delete cluster --name ${cluster_name}
=======
  kind-${os_type}-amd64 delete cluster --name ${cluster_name}
>>>>>>> f2d06d8... Made Makefile and run.sh compatible with MacOS
  rm -rf ${kubeconfig_path}

  if [[ $os_type == 'darwin' ]]
  then
    rm -rf "${kubeconfig_path}.bak"
  fi
}

function main(){

  trap "clean_up" QUIT TERM EXIT

  pull_images
  start_kind
  set_kind_api_server_ip
  run_tests
  exit 0
}

main "$@"
