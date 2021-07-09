#!/usr/bin/env bash
set -eou pipefail

GOPKG="github.com/zalando/postgres-operator"
SCRIPT_ROOT="$(dirname "${BASH_SOURCE[0]}")/.."

rm -rf "${SCRIPT_ROOT}/generated"

go run k8s.io/code-generator/cmd/deepcopy-gen \
 --input-dirs ${GOPKG}/pkg/apis/acid.zalan.do/v1,${GOPKG}/pkg/apis/zalando.org/v1alpha1 \
 -O zz_generated.deepcopy \
 --bounding-dirs ${GOPKG}/pkg/apis \
 --go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
 -o "${SCRIPT_ROOT}/generated"

cp -rv "${SCRIPT_ROOT}/generated/${GOPKG}"/* .

rm -rf "${SCRIPT_ROOT}/generated"