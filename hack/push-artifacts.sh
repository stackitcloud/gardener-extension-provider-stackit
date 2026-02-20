#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

images=$(cat "$1")
chart_names="gardener-extension-provider-stackit gardener-extension-admission-stackit"
helm_artifacts=artifacts/charts

rm -rf "$helm_artifacts"
mkdir -p "$helm_artifacts"

function oci_repo() {
  local chart_name="$1"
  local image_full_tag=$(echo "$images" | jq -r ".images.\"${chart_name}\"")
  echo "$image_full_tag" | rev | cut -d'/' -f2- | rev
}

function image_repo() {
  local chart_name="$1"
  local image_full_tag=$(echo "$images" | jq -r ".images.\"${chart_name}\"")
  echo "$image_full_tag" | cut -d ':' -f 1
}

function image_tag() {
  local chart_name="$1"
  local image_full_tag=$(echo "$images" | jq -r ".images.\"${chart_name}\"")
  echo "$(image_tag_with_digest $chart_name)" | cut -d'@' -f1 | sed 's/-dirty//'
}

function image_tag_with_digest(){
  local chart_name="$1"
  local image_full_tag=$(echo "$images" | jq -r ".images.\"${chart_name}\"")
  echo "$image_full_tag" | cut -d ':' -f 2-
}

function package_and_push_chart() {
    # Always delete the previous, since in the case of subcharts the build_dir is the same!
    rm -rf "${helm_artifacts}/${chart_name}"
    chart_build_dir="${helm_artifacts}/${chart_name}"
    mkdir -p "$chart_build_dir"

    local chart_source_dir="$1"
    cp -r "${chart_source_dir}/." "$chart_build_dir"

    yq -i "\
      ( .image.repository = \"$(image_repo ${chart_name})\" ) | \
      ( .image.tag = \"$(image_tag_with_digest ${chart_name})\" )\
    " "$chart_build_dir/values.yaml"

    helm_package_raw_output=$(helm package "$chart_build_dir" --version "$(image_tag ${chart_name})" -d "$helm_artifacts" 2>&1)

    if [ $? -ne 0 ]; then
      echo "Error: helm package failed for chart ${chart_name}."
      echo "$helm_package_raw_output"
      exit 1
    fi

    packaged_chart_file=$(echo "$helm_package_raw_output" | tail -n 1 | sed -n 's/Successfully packaged chart \(into: \|and saved it to: \)\(.*\)/\2/p')

    if [ -z "$packaged_chart_file" ] || [ ! -f "$packaged_chart_file" ]; then
      echo "Error: 'helm package' claimed success but did not output a valid packaged chart path or the file does not exist for ${chart_name}."
      echo "$helm_package_raw_output"
      exit 1
    fi

    # TODO: If this error (i.e. Unauthorized) then nothing is printed
    push_output=$(helm push "$packaged_chart_file" "oci://$(oci_repo ${chart_name})/charts" 2>&1)

    pushed_line=$(echo "$push_output" | grep "Pushed:")
    digest_line=$(echo "$push_output" | grep "Digest:")

    if [ -n "$pushed_line" ] && [ -n "$digest_line" ]; then
      echo "$pushed_line"
      echo "$digest_line"
    else
      echo "Error: Failed to push chart ${chart_name}."
      echo "$push_output"
      exit 1
    fi
}

if [ "$PUSH" != "true" ] ; then
  echo "Skip pushing artifacts because PUSH is not set to 'true'"
  exit 0
fi

for chart_name in $chart_names; do
  chart_source_dir="charts/${chart_name}"

  if [ -d "${chart_source_dir}/charts" ]; then
    echo "$chart_name has multiple charts"
    for subchart in $(ls "${chart_source_dir}/charts"); do
      echo "${chart_source_dir}/charts/${subchart}"
      package_and_push_chart "${chart_source_dir}/charts/${subchart}"
    done
  else
    package_and_push_chart "${chart_source_dir}"
  fi


done
