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
	LoadBalancer *LoadBalancerConfig `json:"loadBalancer,omitempty"`
}

// LoadBalancerConfig contains configuration for the load balancer.
type LoadBalancerConfig struct {
	// PlanId specifies the service plan (size) of the load balancer.
	// Currently supported plans are p10, p50, p250, p750 (compare API docs).
	// +optional
	PlanId *string `json:"planId,omitempty"`
}
