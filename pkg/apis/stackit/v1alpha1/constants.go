// Package v1alpha1 contains constants and types for the STACKIT provider extension.
// This file defines default names and controller names used throughout the STACKIT provider implementation.
package v1alpha1

const (
	// DefaultCSIName defines the default CSI (Container Storage Interface) name for STACKIT
	DefaultCSIName = "stackit"
	// DefaultCCMName defines the default CCM (Cloud Controller Manager) controller to use
	DefaultCCMName = "stackit"
)

type ControllerName string

const (
	STACKIT   ControllerName = "stackit"
	OPENSTACK ControllerName = "openstack"
)
