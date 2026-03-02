// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	componentbaseconfig "k8s.io/component-base/config/v1alpha1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ControllerConfiguration defines the configuration for the STACKIT provider.
type ControllerConfiguration struct {
	metav1.TypeMeta

	// ClientConnection specifies the kubeconfig file and client connection
	// settings for the proxy server to use when communicating with the apiserver.
	ClientConnection *componentbaseconfig.ClientConnectionConfiguration
	// ETCD is the etcd configuration.
	ETCD ETCD
	// HealthCheckConfig is the config for the health check controller
	HealthCheckConfig *healthcheckconfig.HealthCheckConfig

	// RegistryCaches optionally configures a container registry cache(s) that will be
	// configured on every shoot machine at boot time (and reconciled while its running).
	//
	// Deprecated: will be removed in a future version
	RegistryCaches []RegistryCacheConfiguration

	// DeployALBIngressController
	DeployALBIngressController bool

	// CustomLabelDomain is the domain prefix for custom labels applied to STACKIT infrastructure resources.
	// For example, cluster labels will use "<domain>/cluster" (default: "kubernetes.io").
	// NOTE: Only change this if you know what you are doing!!
	// Changing without a migration plan could lead to orphaned STACKIT resources.
	CustomLabelDomain string
}

// ETCD is an etcd configuration.
type ETCD struct {
	// ETCDStorage is the etcd storage configuration.
	Storage ETCDStorage
	// ETCDBackup is the etcd backup configuration.
	Backup ETCDBackup
}

// ETCDStorage is an etcd storage configuration.
type ETCDStorage struct {
	// ClassName is the name of the storage class used in etcd-main volume claims.
	ClassName *string
	// Capacity is the storage capacity used in etcd-main volume claims.
	Capacity *resource.Quantity
}

// ETCDBackup is an etcd backup configuration.
type ETCDBackup struct {
	// Schedule is the etcd backup schedule.
	Schedule *string
}

// RegistryCacheConfiguration configures a single registry cache.
type RegistryCacheConfiguration struct {
	// Server is the URL of the upstream registry.
	Server string
	// Cache is the URL of the cache registry.
	Cache string
	// CABundle optionally specifies a CA Bundle to trust when connecting to the cache registry.
	CABundle []byte
	// Capabilities optionally specifies what operations the cache registry is capable of.
	Capabilities []string
}
