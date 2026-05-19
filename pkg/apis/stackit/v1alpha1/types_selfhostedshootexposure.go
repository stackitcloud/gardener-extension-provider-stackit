package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SelfHostedShootExposureConfig contains configuration settings for exposing self-hosted shoots.
type SelfHostedShootExposureConfig struct {
	metav1.TypeMeta `json:",inline"`

	// LoadBalancer contains configuration for the load balancer.
	// +optional
	LoadBalancer *LoadBalancer `json:"loadBalancer,omitempty"`
}

// LoadBalancer contains configuration for the load balancer.
type LoadBalancer struct {
	// PlanID specifies the service plan (size) of the load balancer.
	// Currently supported plans are p10, p50, p250, p750 (compare API docs).
	// See https://docs.stackit.cloud/products/network/load-balancing-and-content-delivery/network-load-balancer/reference/service-plans/
	// Defaults to "p10".
	// +optional
	PlanID *string `json:"planID,omitempty"`
	// AccessControl restricts which source IP ranges may reach the load balancer.
	// +optional
	AccessControl *AccessControl `json:"accessControl,omitempty"`
}

// AccessControl restricts access to the load balancer by source IP range.
type AccessControl struct {
	// AllowedSourceRanges is the list of CIDRs permitted to reach the load balancer.
	// An empty or missing list means no source-IP restriction is applied.
	// +optional
	AllowedSourceRanges []string `json:"allowedSourceRanges,omitempty"`
}
