#!/usr/bin/env bash

set -eou pipefail

action="$1"

ns=$(kubectl get namespace -l controllerregistration.core.gardener.cloud/name=provider-stackit -o name | cut -d/ -f2)
mr=$(kubectl get mr -n garden -o name | grep '/provider-stackit' | cut -d/ -f2)

# revert any changes in-cluster after this script is done
cleanup_helper() {
  if [[ "${SKIP_CLEANUP:-}" == "true" ]]; then
    return 0
  fi
  kubectl annotate mr "$mr" -n garden resources.gardener.cloud/ignore-
}
trap cleanup_helper EXIT

# scale down deployment to 1, so that every request ends up in the pod that is intercepted by mirrord.
kubectl annotate mr "$mr" -n garden resources.gardener.cloud/ignore=true
kubectl label deploy -n "$ns" gardener-extension-provider-stackit high-availability-config.resources.gardener.cloud/type-
kubectl scale deploy -n "$ns" gardener-extension-provider-stackit --replicas 1

# get all args currently used in-cluster
mapfile -t args < <(
  kubectl get deploy -n "$ns" gardener-extension-provider-stackit -o json  | jq -r '.spec.template.spec.containers[0].args[]'
)
# if cleanup was disabled, the last element is the --disable-controllers flag
if [[ "${args[-1]}" == --disable-controllers* ]]; then
  unset 'args[-1]'
fi
args+=(--leader-election=false)

# disable all controllers in the pod running in-cluster
read -ra controllers_arr < <(
  go run ./cmd/gardener-extension-provider-stackit --help | grep '\--disable-controllers' | awk -F'[][]' '{print $2}'
)
controllers="$(IFS=, ; echo "${controllers_arr[*]}")"
cat <<EOF | kubectl patch deploy -n "$ns" gardener-extension-provider-stackit --type=json -p "$(cat)"
[{
  "op": "add",
  "path": "/spec/template/spec/containers/0/args/-",
  "value": "--disable-controllers=${controllers[*]}"
}]
EOF
sleep 1
kubectl wait -n "$ns" deploy/gardener-extension-provider-stackit --for='jsonpath={status.readyReplicas}=1'

export MIRRORD_TARGET_NAMESPACE="$ns"

case "$action" in
  debug)
    mirrord exec -f .mirrord/mirrord.json -- \
      dlv debug --listen=:2345 --headless --api-version=2 \
        ./cmd/gardener-extension-provider-stackit -- "${args[@]}"
    ;;
  run)
    mkdir -p out
    go build -o ./out ./cmd/gardener-extension-provider-stackit
    mirrord exec -f .mirrord/mirrord.json -- \
      ./out/gardener-extension-provider-stackit "${args[@]}"
    ;;
  *)
    echo "invalid argument"
esac
