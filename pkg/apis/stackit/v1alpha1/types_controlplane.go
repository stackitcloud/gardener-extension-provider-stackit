// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControlPlaneConfig contains configuration settings for the control plane.
type ControlPlaneConfig struct {
	metav1.TypeMeta `json:",inline"`

	// CloudControllerManager contains configuration settings for the cloud-controller-manager.
	// +optional
	CloudControllerManager *CloudControllerManagerConfig `json:"cloudControllerManager,omitempty"`
	// Zone is the OpenStack zone.
	//
	// Deprecated: Don't use anymore. Will be removed in a future version.
	//
	// +optional
	Zone *string `json:"zone,omitempty"`
	// Storage contains configuration for storage in the cluster.
	// +optional
	Storage *Storage `json:"storage,omitempty"`

	// ApplicationLoadBalancer holds the configuration for the ApplicationLoadBalancer controller
	// +optional
	ApplicationLoadBalancer *ApplicationLoadBalancerConfig `json:"applicationLoadBalancer,omitempty"`
}

type ApplicationLoadBalancerConfig struct {
	Enabled bool `json:"enabled"`
}

// CloudControllerManagerConfig contains configuration settings for the cloud-controller-manager.
type CloudControllerManagerConfig struct {
	// FeatureGates contains information about enabled feature gates.
	// +optional
	FeatureGates map[string]bool `json:"featureGates,omitempty"`
	// Name contains the information of which ccm to deploy
	// +optional
	Name string `json:"name,omitempty"`
}

// Storage contains configuration for storage in the cluster.
type Storage struct {
	// CSIManila contains configuration for CSI Manila driver (support for NFS volumes)
	// +optional
	CSIManila *CSIManila `json:"csiManila,omitempty"`
	// CSI holds the name of the CSI to use (either stackit or openstack)
	// +optional
	CSI *CSI `json:"csi,omitempty"`
}

type CSI struct {
	Name string `json:"name"`
}

// CSIManila contains configuration for CSI Manila driver (support for NFS volumes)
type CSIManila struct {
	// Enabled is the switch to enable the CSI Manila driver support
	Enabled bool `json:"enabled"`
}
