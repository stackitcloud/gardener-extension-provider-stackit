#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

source "$(dirname "$0")/prepare-envtest.sh"

echo "> Integration Tests"

function runIntegrationInProw() {
  go run "$(dirname "$0")/../test/project-wrapper" "${GINKGO}" run "${test_flags[@]}" "$@"
}

function runIntegrationLocally() {
  ${GINKGO} run "${test_flags[@]}" "$@"
}

test_flags=( )
# If running in Prow, we want to generate a machine-readable output file under the location specified via $ARTIFACTS.
# This will add a JUnit view above the build log that shows an overview over successful and failed test cases.
if [ -n "${CI:-}" -a -n "${ARTIFACTS:-}" ] ; then
  mkdir -p "$ARTIFACTS"
  trap "report-collector \"$ARTIFACTS/junit.xml\"" EXIT
  test_flags+=("--junit-report=junit.xml")
  runIntegrationInProw "$@"
else
  runIntegrationLocally "$@"
fi
