#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

OPERATOR_PACKAGE_ROOT="zalando/postgres-operator"
# TARGET_CODE_DIR=${1-${SCRIPT_ROOT}/pkg}

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

source "${CODEGEN_PKG}/kube_codegen.sh"

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.

#cleanup() {
#    rm -rf "${OPERATOR_PACKAGE_ROOT}"
#}
#trap "cleanup" EXIT SIGINT

kube::codegen::gen_helpers \
    --input-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt"

if [[ -n "${API_KNOWN_VIOLATIONS_DIR:-}" ]]; then
    report_filename="${API_KNOWN_VIOLATIONS_DIR}/sample_apiserver_violation_exceptions.list"
    if [[ "${UPDATE_API_KNOWN_VIOLATIONS:-}" == "true" ]]; then
        update_report="--update-report"
    fi
fi

kube::codegen::gen_openapi \
    --input-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
    --output-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/generated" \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../.." \
    --report-filename "${report_filename:-"/dev/null"}" \
    ${update_report:+"${update_report}"} \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

kube::codegen::gen_client \
    --with-watch \
    --with-applyconfig \
    --input-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
    --output-pkg-root "${OPERATOR_PACKAGE_ROOT}/pkg/generated" \
    --output-base "$(dirname "${BASH_SOURCE[0]}")/../../.." \
    --boilerplate "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt"

#bash "${CODEGEN_PKG}/kube_codegen.sh" client,deepcopy,informer,lister \
#  "${OPERATOR_PACKAGE_ROOT}/pkg/generated" "${OPERATOR_PACKAGE_ROOT}/pkg/apis" \
#  "acid.zalan.do:v1 zalando.org:v1" \
#  --go-header-file "${SCRIPT_ROOT}"/hack/custom-boilerplate.go.txt \
#  -o ./

#cp -r "${OPERATOR_PACKAGE_ROOT}"/pkg/* "${TARGET_CODE_DIR}"

#cleanup
