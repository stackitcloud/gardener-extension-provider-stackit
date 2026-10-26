#!/usr/bin/env bash
set -euo pipefail

if [ "${PUSH:-false}" != "true" ]; then
  echo "Skip pushing Flux artifact because PUSH is not set to 'true'"
  exit 0
fi

git_sha=$(git rev-parse --short HEAD)
git_branch=$(git rev-parse --abbrev-ref HEAD)
manifests_repo="${REPOSITORY}/manifests"

flux push artifact "oci://${manifests_repo}:${TAG}" \
  --path="artifacts/manifests" \
  --source="https://github.com/stackitcloud/gardener-extension-provider-stackit" \
  --revision="${git_branch}@sha1:${git_sha}"

if [ "$git_branch" = "main" ] && [ "$TAG" != "main" ]; then
  flux push artifact "oci://${manifests_repo}:main" \
    --path="artifacts/manifests" \
    --source="https://github.com/stackitcloud/gardener-extension-provider-stackit" \
    --revision="main@sha1:${git_sha}"
fi
