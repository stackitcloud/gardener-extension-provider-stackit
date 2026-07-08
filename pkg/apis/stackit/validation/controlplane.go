// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"slices"

	featurevalidation "github.com/gardener/gardener/pkg/utils/validation/features"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

var (
	validControllers           = []stackitv1alpha1.ControllerName{stackitv1alpha1.STACKIT, stackitv1alpha1.OPENSTACK}
	validCSICompatibilityModes = []stackitv1alpha1.CSICompatibilityMode{
		stackitv1alpha1.DEFAULT, stackitv1alpha1.COMPAT, stackitv1alpha1.COMPATBLOCK,
	}
)

// ValidateControlPlaneConfig validates a ControlPlaneConfig object.
func ValidateControlPlaneConfig(controlPlaneConfig *stackitv1alpha1.ControlPlaneConfig, version string, allowApplicationLoadBalancerController bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{} // nolint:prealloc // size is not known yet

	allErrs = append(allErrs, validateCloudController(controlPlaneConfig.CloudControllerManager, version, fldPath.Child("cloudControllerManager"))...)

	allErrs = append(allErrs, validateApplicationLoadBalancer(controlPlaneConfig.ApplicationLoadBalancer, allowApplicationLoadBalancerController, fldPath.Child("applicationLoadBalancer"))...)

	allErrs = append(allErrs, validateStorage(controlPlaneConfig.Storage, fldPath.Child("storage"))...)

	return allErrs
}

// ValidateControlPlaneConfigUpdate validates a ControlPlaneConfig object.
func ValidateControlPlaneConfigUpdate(_, _ *stackitv1alpha1.ControlPlaneConfig, _ *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	return allErrs
}

// ValidateControlPlaneConfigAgainstCloudProfile validates the given ControlPlaneConfig against constraints in the given CloudProfile.
func ValidateControlPlaneConfigAgainstCloudProfile(oldCpConfig, cpConfig *stackitv1alpha1.ControlPlaneConfig, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	return allErrs
}

func validateCloudController(cloudcontroller *stackitv1alpha1.CloudControllerManagerConfig, version string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if cloudcontroller == nil {
		return allErrs
	}
	if cloudcontroller.Name != "" && !slices.Contains(validControllers, stackitv1alpha1.ControllerName(cloudcontroller.Name)) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), cloudcontroller.Name, "not supported ccm driver"))
	}
	allErrs = append(allErrs, featurevalidation.ValidateFeatureGates(cloudcontroller.FeatureGates, version, fldPath.Child("featureGates"))...)

	return allErrs
}

func validateApplicationLoadBalancer(applicationLoadBalancerConfig *stackitv1alpha1.ApplicationLoadBalancerConfig, allowApplicationLoadBalancerController bool, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if applicationLoadBalancerConfig == nil {
		return allErrs
	}
	if !applicationLoadBalancerConfig.Enabled {
		return allErrs
	}

	if !allowApplicationLoadBalancerController {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("enabled"), applicationLoadBalancerConfig.Enabled, "application load balancer support is disabled and cannot be enabled on a shoot"))
	}

	var controllerEnabled bool

	if applicationLoadBalancerConfig.Ingress != nil && applicationLoadBalancerConfig.Ingress.Enabled {
		controllerEnabled = true
	}

	if !controllerEnabled {
		allErrs = append(allErrs, field.Invalid(fldPath.Root(), applicationLoadBalancerConfig, "at least one controller has to be enabled is required"))
	}

	return allErrs
}

func validateStorage(storage *stackitv1alpha1.Storage, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if storage == nil || storage.CSI == nil {
		return allErrs
	}
	if !slices.Contains(validControllers, stackitv1alpha1.ControllerName(storage.CSI.Name)) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("csi", "name"), storage.CSI.Name, "not supported csi driver"))
	}
	// CompatibilityMode is optional; empty is accepted (defaulting sets it to "default").
	if storage.CSI.CompatibilityMode != "" {
		if !slices.Contains(validCSICompatibilityModes, stackitv1alpha1.CSICompatibilityMode(storage.CSI.CompatibilityMode)) {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child("csi", "compatibilityMode"),
				storage.CSI.CompatibilityMode,
				"not supported CSI compatibility mode",
			))
		}
		if stackitv1alpha1.CSICompatibilityMode(storage.CSI.CompatibilityMode) != stackitv1alpha1.DEFAULT &&
			stackitv1alpha1.ControllerName(storage.CSI.Name) != stackitv1alpha1.STACKIT {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child("csi", "compatibilityMode"),
				storage.CSI.CompatibilityMode,
				"can only be set when CSI driver stackit is in use",
			))
		}
	}
	return allErrs
}
