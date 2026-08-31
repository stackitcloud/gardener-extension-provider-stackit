# Using the STACKIT provider extension with Gardener as operator

The [`core.gardener.cloud/v1beta1.CloudProfile` resource](https://github.com/gardener/gardener/blob/master/example/30-cloudprofile.yaml) declares a `providerConfig` field that is meant to contain provider-specific configuration.

In this document we describe how this configuration looks like for STACKIT and provide an example `CloudProfile` manifest with minimal configuration that you can use to allow creating STACKIT shoot clusters.

## `CloudProfileConfig`

The cloud profile configuration contains information about the real machine image IDs in the STACKIT environment (image names/IDs).
You have to map every version that you specify in `.spec.machineImages[].versions` here such that the STACKIT extension knows the image ID for every version you want to offer.

It also contains optional default values for DNS servers that shall be used for shoots.

### `machineImages`

For each machine image version, region-specific image IDs are mapped using the `regions` field. An optional `architecture` field can be specified per region entry, which specifies the CPU architecture of the machine on which the given machine image can be used. It defaults to `amd64`.

```yaml
machineImages:
  - name: ubuntu
    versions:
      - version: "22.04"
        regions:
          - name: eu01
            id: <image-id>
            architecture: amd64
```

An optional `image` field at the version level can be used as a fallback (image name) if no region mapping is found. This fallback only works for the `amd64` architecture and is strongly discouraged; prefer explicit image IDs.

### `dnsServers`

In the `dnsServers[]` list you can specify IP addresses that are used as DNS configuration for created shoot networks.

### `rescanBlockStorageOnResize`

The `rescanBlockStorageOnResize` field specifies whether the storage plugin scans and checks the new block device size before it resizes the filesystem.

### `storageClasses`

The `storageClasses` field enables the creation of Kubernetes `StorageClass`es for shoots. Each entry can define a `name`, whether it is `default`, `parameters`, `annotations`, `labels`, `reclaimPolicy`, and `volumeBindingMode`. The provisioner is set automatically by the extension to `block-storage.csi.stackit.cloud` (the STACKIT CSI driver).

```yaml
storageClasses:
  - name: default
    default: true
    parameters:
      type: "storage_premium_perf4"
```

### `apiEndpoints`

The `apiEndpoints` field contains API endpoints for the various STACKIT services used by the extension:

- `dns` – endpoint of the DNS API.
- `loadBalancer` – endpoint of the LoadBalancer API.
- `iaas` – endpoint of the IaaS API.
- `applicationLoadBalancer` – endpoint of the Application LoadBalancer API.
- `applicationLoadBalancerCertificate` – endpoint of the Application LoadBalancerCertificate API.
- `tokenEndpoint` – the token endpoint URL.

### `bastion`

The `bastion.rootDiskSize` field allows adjusting the root disk size of the bastion server. It defaults to `25`.

### `resolvConfOptions`

The `resolvConfOptions` field specifies resolver options (e.g. `rotate`, `timeout:1`) that are added as an `options` line to the `resolv.conf` used by the kubelet on workers.

## Example `CloudProfile` manifest

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: CloudProfile
metadata:
  name: stackit
spec:
  type: stackit
  kubernetes:
    versions:
      - version: 1.33.0
  machineImages:
    - name: ubuntu
      versions:
        - version: "22.04"
  machineTypes:
    - name: <machine-type>
      cpu: "4"
      gpu: "0"
      memory: 8Gi
      storage:
        type: default
        size: 40Gi
  regions:
    - name: eu01
      zones:
        - name: eu01-1
  providerConfig:
    apiVersion: stackit.provider.extensions.gardener.cloud/v1alpha1
    kind: CloudProfileConfig
    machineImages:
      - name: ubuntu
        versions:
          - version: "22.04"
            regions:
              - name: eu01
                id: <image-id>
                architecture: amd64
    dnsServers:
      - 1.1.1.1
    rescanBlockStorageOnResize: true
    storageClasses:
      - name: default
        default: true
        parameters:
          type: "storage_premium_perf4"
    apiEndpoints:
      loadBalancer: https://<load-balancer-api-endpoint>
      iaas: https://<iaas-api-endpoint>
      tokenEndpoint: https://<token-endpoint>
    bastion:
      rootDiskSize: 25
```
