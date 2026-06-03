#!/usr/bin/env bash

# enable unofficial bash strict mode
set -o errexit
set -o nounset
set -o pipefail
IFS=$'\n\t'

readonly cluster_name="postgres-operator-e2e-tests"
readonly kubeconfig_path="${HOME}/kind-config-${cluster_name}"
readonly spilo_image="ghcr.io/zalando/spilo-18:4.1-p1"
readonly e2e_test_runner_image="ghcr.io/zalando/postgres-operator-e2e-tests-runner:latest"

export GOPATH=${GOPATH-~/go}
export PATH=${GOPATH}/bin:$PATH

# detect system architecture for pulling the correct Spilo image
case "$(uname -m)" in
    x86_64)  readonly PLATFORM="linux/amd64" ;;
    aarch64|arm64) readonly PLATFORM="linux/arm64" ;;
    *) echo "Unsupported architecture: $(uname -m)"; exit 1 ;;
esac

echo "Clustername: ${cluster_name}"
echo "Kubeconfig path: ${kubeconfig_path}"

function pull_images(){
  operator_tag=$(git describe --tags --always --dirty)
  components=("postgres-operator" "pooler")
  image_urls=("ghcr.io/zalando/postgres-operator:${operator_tag}" "ghcr.io/zalando/postgres-operator/pgbouncer:${operator_tag}")

  for i in "${!components[@]}"; do
    component="${components[$i]}"
    image="${image_urls[$i]}"

    if [[ -z $(docker images -q "$image") ]]; then
      echo "Pulling $component image: $image"
      if ! docker pull "$image"; then
        echo "Failed to pull $component image: $image"
        exit 1
      fi
    else
      echo "$component image already exists: $image"
    fi

    # Set variables for later use
    if [[ "$component" == "postgres-operator" ]]; then
      operator_image="$image"
    elif [[ "$component" == "pooler" ]]; then
      pooler_image="$image"
    fi
  done

  echo "Using operator image: $operator_image"
  echo "Using pooler image: $pooler_image"
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
  
  echo "Pulling Spilo image for platform ${PLATFORM}"
  docker pull --platform ${PLATFORM} "${spilo_image}"
  kind load docker-image "${spilo_image}" --name ${cluster_name}
}

function load_operator_images() {
  echo "Loading operator images"
  export KUBECONFIG="${kubeconfig_path}"
  kind load docker-image "${operator_image}" --name ${cluster_name}
  kind load docker-image "${pooler_image}" --name ${cluster_name}
}

function set_kind_api_server_ip(){
  echo "Setting up kind API server ip"
  # use the actual kubeconfig to connect to the 'kind' API server
  # but update the IP address of the API server to the one from the Docker 'bridge' network
  readonly local kind_api_server_port=6443 # well-known in the 'kind' codebase
  readonly local kind_api_server=$(docker inspect --format "{{ .NetworkSettings.Networks.kind.IPAddress }}:${kind_api_server_port}" "${cluster_name}"-control-plane)
  sed "s/server.*$/server: https:\/\/$kind_api_server/g" "${kubeconfig_path}" > "${kubeconfig_path}".tmp && mv "${kubeconfig_path}".tmp "${kubeconfig_path}"
}

function generate_certificate(){
  openssl req -x509 -nodes -newkey rsa:2048 -keyout tls/tls.key -out tls/tls.crt -subj "/CN=acid.zalan.do"
}

function run_tests(){
  echo "Running tests... image: ${e2e_test_runner_image}"
  # tests modify files in ./manifests, so we mount a copy of this directory done by the e2e Makefile

  docker run --rm --network=host -e "TERM=xterm-256color" \
  --mount type=bind,source="$(readlink -f ${kubeconfig_path})",target=/root/.kube/config \
  --mount type=bind,source="$(readlink -f manifests)",target=/manifests \
  --mount type=bind,source="$(readlink -f tls)",target=/tls \
  --mount type=bind,source="$(readlink -f tests)",target=/tests \
  --mount type=bind,source="$(readlink -f exec.sh)",target=/exec.sh \
  --mount type=bind,source="$(readlink -f scripts)",target=/scripts \
  -e OPERATOR_IMAGE="${operator_image}" -e POOLER_IMAGE="${pooler_image}" \
  "${e2e_test_runner_image}" ${E2E_TEST_CASE-} $@
}

function cleanup(){
  echo "Executing cleanup"
  unset KUBECONFIG
  kind delete cluster --name ${cluster_name}
  rm -rf ${kubeconfig_path}
}

function main(){
  echo "Entering main function..."
  [[ -z ${NOCLEANUP-} ]] && trap "cleanup" QUIT TERM EXIT
  pull_images
  [[ ! -f ${kubeconfig_path} ]] && start_kind
  load_operator_images
  set_kind_api_server_ip
  generate_certificate

  shift
  run_tests $@
  exit 0
}

"$1" $@
