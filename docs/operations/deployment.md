# Deployment of the STACKIT provider extension

**Disclaimer:** This document is NOT a step by step installation guide for the STACKIT provider extension and only contains some configuration specifics regarding the installation of the different components via the helm charts residing in this repository.

## gardener-extension-admission-stackit

### Authentication against the Garden cluster

By default, the admission component uses in-cluster configuration to talk to the Garden cluster. To use an explicit kubeconfig instead, set `.Values.kubeconfig` in the `runtime` chart. The value is the kubeconfig content as a string; the chart base64-encodes it into a `Secret` that is mounted into the pod (passed via `--kubeconfig=/etc/gardener-extension-admission-stackit/kubeconfig/kubeconfig`). When `kubeconfig` is set, the pod's service account token is not automounted.

Alternatively, use a projected service account token volume by setting `.Values.projectedKubeconfig`:

```yaml
projectedKubeconfig:
  baseMountPath: /var/run/secrets/gardener.cloud
  genericKubeconfigSecretName: generic-token-kubeconfig
  tokenSecretName: access-stackit-admission
```

This mounts a generic kubeconfig and a token from the two referenced secrets into the pod.

### Virtual Garden

When a *Virtual Garden* is used (i.e., the `runtime` Garden cluster is different from the `target` Garden cluster), set `.Values.gardener.virtualCluster.enabled: true` (the default in the `runtime` chart).

This switches the admission webhook configuration from service mode to URL mode (`--webhook-config-mode=url`) and sets the `SOURCE_CLUSTER` environment variable. The `virtual-garden` subchart deploys a `ServiceAccount`, `ClusterRole`, and `ClusterRoleBinding` into the target cluster.

### Enabling Application Load Balancer support

The Application Load Balancer (ALB) controller is disabled by default in the admission webhook. To allow shoots to enable ALB support via `ControlPlaneConfig.applicationLoadBalancer.enabled: true`, the admission webhook must be configured with `allowApplicationLoadBalancerController: true`.

Set this in the `gardener-extension-admission-stackit` chart values:

```yaml
# charts/gardener-extension-admission-stackit/charts/runtime/values.yaml
allowApplicationLoadBalancerController: true
```

When this flag is `false` (the default), any shoot attempting to enable the ALB controller will be rejected by the admission webhook with a validation error.
