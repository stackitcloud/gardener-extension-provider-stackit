// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
)

const (
	// Name is the name of the OpenStack provider.
	Name = "provider-openstack"

	// STACKITCloudControllerManagerImageName is the name of the stackit-cloud-controller-manager image.
	STACKITCloudControllerManagerImageName = "stackit-cloud-controller-manager"

	// AuthURL is a constant for the key in a cloud provider secret that holds the OpenStack auth url.
	AuthURL = "authURL"
	// DomainName is a constant for the key in a cloud provider secret that holds the OpenStack domain name.
	DomainName = "domainName"
	// TenantName is a constant for the key in a cloud provider secret that holds the OpenStack tenant name.
	TenantName = "tenantName"
	// UserName is a constant for the key in a cloud provider secret and backup secret that holds the OpenStack username.
	UserName = "username"
	// Password is a constant for the key in a cloud provider secret and backup secret that holds the OpenStack password.
	Password = "password"
	// ApplicationCredentialID is a constant for the key in a cloud provider secret and backup secret that holds the OpenStack application credential id.
	ApplicationCredentialID = "applicationCredentialID"
	// ApplicationCredentialName is a constant for the key in a cloud provider secret and backup secret that holds the OpenStack application credential name.
	ApplicationCredentialName = "applicationCredentialName"
	// ApplicationCredentialSecret is a constant for the key in a cloud provider secret and backup secret that holds the OpenStack application credential secret.
	ApplicationCredentialSecret = "applicationCredentialSecret"
	// Region is a constant for the key in a backup secret that holds the Openstack region.
	Region = "region"
	// Insecure is a constant for the key in a cloud provider secret that configures whether the OpenStack client verifies the server's certificate.
	Insecure = "insecure"
	// CACert is a constant for the key in a cloud provider secret that configures the CA bundle used to verify the server's certificate.
	CACert = "caCert"

	// DNSAuthURL is a constant for the key in a DNS secret that holds the OpenStack auth url.
	DNSAuthURL = "OS_AUTH_URL"
	// DNSDomainName is a constant for the key in a DNS secret that holds the OpenStack domain name.
	DNSDomainName = "OS_DOMAIN_NAME"
	// DNSTenantName is a constant for the key in a DNS secret that holds the OpenStack tenant name.
	DNSTenantName = "OS_PROJECT_NAME"
	// DNSUserName is a constant for the key in a DNS secret that holds the OpenStack username.
	DNSUserName = "OS_USERNAME"
	// DNSPassword is a constant for the key in a DNS secret that holds the OpenStack password.
	DNSPassword = "OS_PASSWORD"
	// DNSApplicationCredentialID is a constant for the key in a DNS secret hat holds the OpenStack application credential id.
	DNSApplicationCredentialID = "OS_APPLICATION_CREDENTIAL_ID"
	// DNSApplicationCredentialName is a constant for the key in a DNS secret  that holds the OpenStack application credential name.
	DNSApplicationCredentialName = "OS_APPLICATION_CREDENTIAL_NAME"
	// DNSApplicationCredentialSecret is a constant for the key in a DNS secret  that holds the OpenStack application credential secret.
	DNSApplicationCredentialSecret = "OS_APPLICATION_CREDENTIAL_SECRET"
	// DNSCABundle is a constant for the key in a DNS secret that holds the Openstack CA Bundle for the KeyStone server.
	DNSCABundle = "OS_CACERT"

	// CloudProviderConfigName is the name of the secret containing the cloud provider config.
	CloudProviderConfigName = "cloud-provider-config"
	// CloudProviderDiskConfigName is the name of the secret containing the cloud provider config for disk/volume handling. It is used by kube-controller-manager.
	CloudProviderDiskConfigName = "cloud-provider-disk-config"
	// CloudProviderCSIDiskConfigName is the name of the secret containing the cloud provider config for disk/volume handling. It is used by csi-driver-controller.
	CloudProviderCSIDiskConfigName = "cloud-provider-disk-config-csi"
	// CloudProviderConfigDataKey is the key storing the cloud provider config as value in the cloud provider secret.
	CloudProviderConfigDataKey = "cloudprovider.conf"
	// CloudProviderConfigKeyStoneCAKey is the key storing the KeyStone CA bundle.
	CloudProviderConfigKeyStoneCAKey = "keystone-ca.crt"
	// CloudControllerManagerName is a constant for the name of the CloudController deployed by the worker controller. (openstack)
	CloudControllerManagerName = "cloud-controller-manager"
	// STACKITCloudControllerManagerName is a constant for the name of the CloudController deployed by the worker controller. (stackit)
	STACKITCloudControllerManagerName = "stackit-cloud-controller-manager"
	// STACKITALBControllerManagerName is a constant for the name of the ALB CloudController. (stackit)
	STACKITALBControllerManagerName = "stackit-alb-controller-manager"
	// CSIDiskDriverTopologyKey is the label on persistent volumes that represents availability by zone.
	// See https://github.com/kubernetes/cloud-provider-openstack/blob/master/examples/cinder-csi-plugin/topology/example.yaml
	// See https://gitlab.cern.ch/cloud/cloud-provider-openstack/-/blob/release-1.19/docs/using-cinder-csi-plugin.md#enable-topology-aware-dynamic-provisioning-for-cinder-volumes
	CSIDiskDriverTopologyKey = "topology.cinder.csi.openstack.org/zone"
	// CSISTACKITDriverTopologyKey is the label on persistent volumes that represents availability by zone.
	CSISTACKITDriverTopologyKey = "topology.block-storage.csi.stackit.cloud/zone"
	// CSIControllerName is a constant for the chart name for a CSI Cinder controller deployment in the seed.
	CSIControllerName = "csi-driver-controller"
	// CSISTACKITControllerName is a constant for the chart name for a CSI STACKIT controller deployment in the seed.
	CSISTACKITControllerName = "stackit-blockstorage-csi-driver"
	// CSINodeName is a constant for the chart name for a CSI Cinder node deployment in the shoot.
	CSINodeName = "csi-driver-node"
	// CSISTACKITNodeName is a constant for the chart name for a CSI STACKIT node deployment in the shoot.
	CSISTACKITNodeName = "stackit-blockstorage-csi-driver"
	// CSIDriverName is a constant for the name of the csi-driver component.
	CSIDriverName = "csi-driver"
	// CSIProvisionerName is a constant for the name of the csi-provisioner component.
	CSIProvisionerName = "csi-provisioner"
	// CSIAttacherName is a constant for the name of the csi-attacher component.
	CSIAttacherName = "csi-attacher"
	// CSISnapshotterName is a constant for the name of the csi-snapshotter component.
	CSISnapshotterName = "csi-snapshotter"
	// CSIResizerName is a constant for the name of the csi-resizer component.
	CSIResizerName = "csi-resizer"
	// CSISnapshotControllerName is a constant for the name of the csi-snapshot-controller component.
	CSISnapshotControllerName = "csi-snapshot-controller"
	// CSISnapshotValidationName is the constant for the name of the csi-snapshot-validation-webhook component.
	// TODO: Remove once all snapshot validation webhook have been cleaned up
	CSISnapshotValidationName = "csi-snapshot-validation"
	// CSIStorageProvisioner is a constant with the storage provisioner name which is used in storageclasses.
	CSIStorageProvisioner = "cinder.csi.openstack.org"
	// CSISTACKITStorageProvisioner is a constant with the storage provisioner name which is used in storageclasses.
	CSISTACKITStorageProvisioner = "block-storage.csi.stackit.cloud"
)

var (
	// UsernamePrefix is a constant for the username prefix of components deployed by OpenStack.
	UsernamePrefix = extensionsv1alpha1.SchemeGroupVersion.Group + ":" + Name + ":"
)
