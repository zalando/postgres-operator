#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

GENERATED_PACKAGE_ROOT="src/github.com"
OPERATOR_PACKAGE_ROOT="${GENERATED_PACKAGE_ROOT}/zalando/postgres-operator"
SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
TARGET_CODE_DIR=${1-${SCRIPT_ROOT}/pkg}
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

source "${CODEGEN_PKG}/kube_codegen.sh"

cleanup() {
    rm -rf "${GENERATED_PACKAGE_ROOT}"
}
trap "cleanup" EXIT SIGINT

kube::codegen::gen_helpers \
    --input-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt"

kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --input-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
    --output-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/generated/client" \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt"

#bash "${CODEGEN_PKG}/kube_codegen.sh" client,deepcopy,informer,lister \
#  "${OPERATOR_PACKAGE_ROOT}/pkg/generated" "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
#  "acid.zalan.do:v1 zalando.org:v1" \
#  --go-header-file "${SCRIPT_ROOT}"/hack/custom-boilerplate.go.txt \
#  -o ./

#cp -r "${OPERATOR_PACKAGE_ROOT}"/pkg/* "${TARGET_CODE_DIR}"

cleanup
