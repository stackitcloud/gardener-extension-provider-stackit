# Using the STACKIT provider extension with Gardener as end-user

The [`core.gardener.cloud/v1beta1.Shoot` resource](https://github.com/gardener/gardener/blob/master/example/90-shoot.yaml) declares a few fields that are meant to contain provider-specific configuration.

In this document we describe how this configuration looks for STACKIT and provide an example `Shoot` manifest with minimal configuration that you can use to create a STACKIT cluster, except for the landscape-specific information such as cloud profile names and credentials binding names.

## Provider Secret Data

Every shoot cluster references a `CredentialsBinding` via `credentialsBindingName` which itself references a `Secret` by `credentialsRef`. This `Secret` contains the provider credentials of your STACKIT project.
This `Secret` must look as follows:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: core-stackit
  namespace: garden-dev
type: Opaque
data:
  project-id: base64(project-id)
  serviceaccount.json: base64(service-account-key-json)
```

The two required fields are:

| Field                 | Description                                     |
| --------------------- | ----------------------------------------------- |
| `project-id`          | The STACKIT project identifier.                 |
| `serviceaccount.json` | The STACKIT service account key in JSON format. |

The service account must be granted the STACKIT permissions required by the deployed components. The roles referenced in this repository are:

| Permission                    | Purpose                                                                                 |
| ----------------------------- | --------------------------------------------------------------------------------------- |
| `nlb.admin`                   | CCM service-controller, network load balancer and self-hosted shoot exposure controller |
| `alb.admin`                   | application load balancer controller                                                    |
| `blockstorage.admin`          | CSI driver                                                                              |
| `compute.admin`               | CCM node-controller and MCM                                                             |
| `iaas.network.admin`          | bastion and infrastructure controller                                                   |
| `iaas.isolated-network.admin` | infrastructure controller                                                               |

## `InfrastructureConfig`

The infrastructure configuration mainly describes how the network layout looks like in order to create the shoot worker nodes in a later step.

An example `InfrastructureConfig` for the STACKIT extension looks as follows:

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: johndoe-stackit
  namespace: garden-dev
spec:
  provider:
    infrastructureConfig:
      apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
      kind: InfrastructureConfig
      floatingPoolName: MY-FLOATING-POOL
      networks:
        workers: 10.250.0.0/19
```

The `floatingPoolName` is the name of the floating pool (external network) you want to use for your shoot. It is a required field. If you don't know which floating pools are available, look them up in the respective `CloudProfile`.

The `networks.workers` section describes the CIDR for the (isolated) network that is used for all shoot worker nodes, i.e., VMs which later run your applications. You can freely choose this CIDR and it is your responsibility to properly design the network layout to suit your needs.

Instead of creating a new network, you can reuse an existing network by specifying its ID via `networks.id`:

```yaml
provider:
  infrastructureConfig:
    apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
    kind: InfrastructureConfig
    floatingPoolName: MY-FLOATING-POOL
    networks:
      id: 12345678-abcd-efef-08af-0123456789ab
```

When `networks.id` is set, the `networks.workers` CIDR must not be set. The `networks.id` value must be a valid STACKIT network ID (UUID).

> [!NOTE]
> `networks.worker` is a deprecated alias for `networks.workers`. If both are set, `networks.workers` takes precedence.

The optional `networks.dnsServers` field overrides the DNS servers configured in the `CloudProfile` (`CloudProfileConfig.dnsServers`) and is used when the worker network is created:

```yaml
provider:
  infrastructureConfig:
    apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
    kind: InfrastructureConfig
    floatingPoolName: MY-FLOATING-POOL
    networks:
      workers: 10.250.0.0/19
      dnsServers:
        - 1.1.1.1
```

The `floatingPoolName` and the whole `networks` section are immutable after cluster creation.

## `ControlPlaneConfig`

The control plane configuration mainly contains values for the STACKIT-specific control plane components (`cloud-controller-manager` and CSI driver), as well as the optional Application Load Balancer controller.

An example `ControlPlaneConfig` for the STACKIT extension looks as follows:

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: johndoe-stackit
  namespace: garden-dev
spec:
  provider:
    controlPlaneConfig:
      apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
      kind: ControlPlaneConfig
      cloudControllerManager:
        name: stackit
        # featureGates:
        #   SomeKubernetesFeature: true
      storage:
        csi:
          name: stackit
      applicationLoadBalancer:
        enabled: true
        ingress:
          enabled: true
```

### `cloudControllerManager`

The optional `cloudControllerManager.name` field selects which cloud-controller-manager is deployed:

- `stackit` (default) – the STACKIT cloud-controller-manager.

The `cloudControllerManager.featureGates` field contains a map of explicitly enabled or disabled feature gates. For production usage it's not recommended to use this field at all, as you can enable alpha features or disable beta/stable features, potentially impacting cluster stability. If you don't want to configure anything, simply omit the key in the YAML specification.

### `storage.csi`

The optional `storage.csi.name` field selects the CSI driver for block storage:

- `stackit` (default) – the STACKIT CSI driver.

### `applicationLoadBalancer`

The optional `applicationLoadBalancer` section enables the STACKIT Application Load Balancer (ALB) controller:

- `applicationLoadBalancer.enabled` activates the ALB integration.
- `applicationLoadBalancer.ingress.enabled` activates the Ingress controller for the ALB.

When the ALB is enabled, at least one controller source (currently only `ingress`) must be enabled.

> **Note:** ALB support must be enabled in the admission webhook configuration before it can be used in a shoot. See [Deployment](../operations/deployment.md) for details.

### Deprecated fields

The `zone` field is deprecated and will be removed in a future version. Don't use it anymore.

## `WorkerConfig`

Each worker group in a shoot may contain provider-specific configurations and options. These are contained in the `providerConfig` section of a worker group and can be configured using a `WorkerConfig` object. An example of a `WorkerConfig` within a shoot looks as follows:

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: johndoe-stackit
  namespace: garden-dev
spec:
  provider:
    workers:
      - name: worker-xoluy
        providerConfig:
          apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
          kind: WorkerConfig
          # nodeTemplate: # (to be specified only if the node capacity would be different from cloudprofile info during runtime)
          #   capacity:
          #     cpu: 2
          #     nvidia.com/gpu: 0
          #     memory: 50Gi
```

### Node Templates

The `nodeTemplate` section allows overriding the capacity of the nodes as defined by the server flavor specified in the `CloudProfile`'s `machineTypes`. This is useful for dynamic scenarios as it allows customizing cluster-autoscaler's behavior for this worker group with the provided values (e.g., scaling a node group from zero).

## `SelfHostedShootExposureConfig`

For [self-hosted shoots](https://gardener.cloud/docs/gardener/extensions/resources/selfhostedshootexposure/), the `SelfHostedShootExposure` resource's `providerConfig` section can be used to configure the STACKIT load balancer that exposes the control plane. An example looks as follows:

```yaml
apiVersion: extensions.gardener.cloud/v1alpha1
kind: SelfHostedShootExposure
metadata:
  name: self-hosted-exposure
  namespace: kube-system
spec:
  type: stackit
  providerConfig:
    apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
    kind: SelfHostedShootExposureConfig
    loadBalancer:
      planID: p10
      # accessControl:
      #   allowedSourceRanges:
      #   - 203.0.113.0/24
```

- `loadBalancer.planID` specifies the service plan (size) of the load balancer. Currently supported plans are `p10`, `p50`, `p250`, `p750`. It defaults to `p10`.
- `loadBalancer.accessControl.allowedSourceRanges` restricts which source IP ranges (CIDRs) may reach the load balancer. An empty or missing list means no source-IP restriction is applied.

## Example `Shoot` manifest

Please find below an example `Shoot` manifest:

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: johndoe-stackit
  namespace: garden-dev
spec:
  cloudProfile:
    name: stackit
  region: eu01
  credentialsBindingName: core-stackit
  provider:
    type: stackit
    infrastructureConfig:
      apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
      kind: InfrastructureConfig
      floatingPoolName: MY-FLOATING-POOL
      networks:
        workers: 10.250.0.0/19
    controlPlaneConfig:
      apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
      kind: ControlPlaneConfig
      cloudControllerManager:
        name: stackit
      storage:
        csi:
          name: stackit
    workers:
      - name: worker-xoluy
        machine:
          type: MY-MACHINE-TYPE
        minimum: 2
        maximum: 2
        zones:
          - eu01-1
  networking:
    nodes: 10.250.0.0/16
    type: calico
  kubernetes:
    version: 1.33.0
  maintenance:
    autoUpdate:
      kubernetesVersion: true
      machineImageVersion: true
  addons:
    kubernetesDashboard:
      enabled: true
    nginxIngress:
      enabled: true
```

## CSI volume provisioners

By default, every STACKIT shoot cluster is deployed with the STACKIT CSI driver, which uses the `block-storage.csi.stackit.cloud` provisioner.

## DNS records

The extension supports the `DNSRecord` resource of type `stackit`. An example looks as follows:

```yaml
apiVersion: extensions.gardener.cloud/v1alpha1
kind: DNSRecord
metadata:
  name: dnsrecord-external
  namespace: shoot--foobar--stackit
spec:
  type: stackit
  secretRef:
    name: dnsrecord-external
    namespace: shoot--foobar--stackit
  name: api.example.foobar.shoot.example.com
  recordType: A # Use A, CNAME, or TXT
  values: # list of IP addresses for A records, a single hostname for CNAME records, or a list of texts for TXT records.
    - 1.2.3.4
  # zone: some-zone-uuid
  # ttl: 120
```

The referenced `Secret` contains the same `project-id` and `serviceaccount.json` fields as the [provider secret](#provider-secret-data). If `zone` is not set, the extension looks up the matching hosted zone by listing the zones of the project and matching against the record name; the resolved zone ID is persisted in the `DNSRecord` status. The STACKIT DNS API is global, so the region is not used to select an endpoint. The `ttl` field defaults to `120` seconds and must be within the STACKIT-allowed range of `60` to `99999999` seconds.
