// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"slices"

	featurevalidation "github.com/gardener/gardener/pkg/utils/validation/features"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
)

var (
	validControllers = []stackitv1alpha1.ControllerName{stackitv1alpha1.STACKIT, stackitv1alpha1.OPENSTACK}
)

// ValidateControlPlaneConfig validates a ControlPlaneConfig object.
func ValidateControlPlaneConfig(controlPlaneConfig *stackitv1alpha1.ControlPlaneConfig, infraConfig *stackitv1alpha1.InfrastructureConfig, version string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if controlPlaneConfig.CloudControllerManager != nil {
		allErrs = append(allErrs, featurevalidation.ValidateFeatureGates(controlPlaneConfig.CloudControllerManager.FeatureGates, version, fldPath.Child("cloudControllerManager", "featureGates"))...)
		allErrs = append(allErrs, validateCloudController(controlPlaneConfig.CloudControllerManager, fldPath.Child("cloudControllerManager"))...)
	}

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

func validateCloudController(cloudcontroller *stackitv1alpha1.CloudControllerManagerConfig, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if cloudcontroller == nil {
		return allErrs
	}
	if cloudcontroller.Name != "" && !slices.Contains(validControllers, stackitv1alpha1.ControllerName(cloudcontroller.Name)) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), cloudcontroller.Name, "not supported ccm driver"))
	}
	return allErrs
}

func validateStorage(storage *stackitv1alpha1.Storage, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if storage == nil {
		return allErrs
	}
	if storage.CSI != nil && !slices.Contains(validControllers, stackitv1alpha1.ControllerName(storage.CSI.Name)) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("csi", "name"), storage.CSI.Name, "not supported csi driver"))
	}
	return allErrs
}
