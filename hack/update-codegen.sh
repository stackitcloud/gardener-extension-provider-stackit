#!/bin/bash
#
# SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

set -o errexit
set -o nounset
set -o pipefail

CODE_GEN_DIR=$(go list -m -f '{{.Dir}}' k8s.io/code-generator)
source "${CODE_GEN_DIR}/kube_codegen.sh"

GOPATH="${GOPATH:-$(go env GOPATH)}"
rm -f ${GOPATH}/bin/*-gen

CURRENT_DIR=$(dirname $0)
PROJECT_ROOT="${CURRENT_DIR}"/..

kube::codegen::gen_helpers \
  --boilerplate "${CURRENT_DIR}/boilerplate.go.txt" \
  "${PROJECT_ROOT}/pkg/apis/stackit/v1alpha1"

kube::codegen::gen_helpers \
  --boilerplate "${CURRENT_DIR}/boilerplate.go.txt" \
  "${PROJECT_ROOT}/pkg/apis/config"
