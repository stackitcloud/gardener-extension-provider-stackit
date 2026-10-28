#!/usr/bin/env bash
set -euo pipefail

manifests_dir="artifacts/manifests"
manifests_repo="${REPOSITORY}/manifests"

mkdir -p "$manifests_dir"
cp -rf deploy/operator/. "$manifests_dir"

yq -i "\
  ( .spec.deployment.extension.helm.ociRepository.ref = \"${REPOSITORY}/charts/gardener-extension-provider-stackit:${TAG}\" ) | \
  ( .spec.deployment.admission.runtimeCluster.helm.ociRepository.ref = \"${REPOSITORY}/charts/admission-stackit-runtime:${TAG}\" ) | \
  ( .spec.deployment.admission.virtualCluster.helm.ociRepository.ref = \"${REPOSITORY}/charts/admission-stackit-virtual-garden:${TAG}\" )\
" "$manifests_dir/extension.yaml"

if [ "${PUSH:-false}" != "true" ]; then
  echo "Skip pushing Flux artifact because PUSH is not set to 'true'"
  exit 0
fi

git_sha=$(git rev-parse HEAD)
git_branch=$(git rev-parse --abbrev-ref HEAD)

flux push artifact "oci://${manifests_repo}:${TAG}" \
  --path="$manifests_dir" \
  --source="https://github.com/stackitcloud/gardener-extension-provider-stackit" \
  --revision="${git_branch}@sha1:${git_sha}"

flux tag artifact "oci://${manifests_repo}:${TAG}" \
  --tag="${git_branch}"
