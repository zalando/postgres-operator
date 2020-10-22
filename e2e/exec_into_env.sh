#!/bin/bash

export cluster_name="postgres-operator-e2e-tests"
export kubeconfig_path="/tmp/kind-config-${cluster_name}"
export operator_image="registry.opensource.zalan.do/acid/postgres-operator:latest"
export e2e_test_runner_image="registry.opensource.zalan.do/acid/postgres-operator-e2e-tests-runner:0.3"

docker run -it --entrypoint /bin/bash --network=host -e "TERM=xterm-256color" \
    --mount type=bind,source="$(readlink -f ${kubeconfig_path})",target=/root/.kube/config \
    --mount type=bind,source="$(readlink -f manifests)",target=/manifests \
    --mount type=bind,source="$(readlink -f tests)",target=/tests \
    --mount type=bind,source="$(readlink -f exec.sh)",target=/exec.sh \
    --mount type=bind,source="$(readlink -f scripts)",target=/scripts \
    -e OPERATOR_IMAGE="${operator_image}" "${e2e_test_runner_image}"
