# Using the STACKIT provider extension with Gardener as operator

The [`core.gardener.cloud/v1beta1.CloudProfile` resource](https://github.com/gardener/gardener/blob/master/example/30-cloudprofile.yaml) declares a `providerConfig` field that is meant to contain provider-specific configuration.

In this document we are describing how this configuration looks like for STACKIT and provide an example `CloudProfile` manifest with minimal configuration that you can use to allow creating STACKIT shoot clusters.


## `CloudProfileConfig`

The cloud profile configuration contains information about the real machine image IDs in the STACKIT environment (image names).
You have to map every version that you specify in `.spec.machineImages[].versions` here such that the STACKIT extension knows the image ID for every version you want to offer.

TODO: ask about storageclass

It also contains optional default values for DNS servers that shall be used for shoots.
In the `dnsServers[]` list you can specify IP addresses that are used as DNS configuration for created shoot subnets.

Some hypervisors (especially those which are VMware-based) don't automatically send a new volume size to a Linux kernel when a volume is resized and in-use.
For those hypervisors you can enable the storage plugin interacting with Cinder to telling the SCSI block device to refresh its information to provide information about it's updated size to the kernel. You might need to enable this behavior depending on the underlying hypervisor of your STACKIT installation. The `rescanBlockStorageOnResize` field controls this. Please note that it only applies for Kubernetes versions where CSI is used.

You can specify API endpoints for various STACKIT services(IaaS, LoadBalancer), via `APIEndpoints`.

## Example `CloudProfile` manifest

