// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"slices"

	cidrvalidation "github.com/gardener/gardener/pkg/utils/validation/cidr"
	"github.com/google/uuid"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
)

// ValidateInfrastructureConfig validates a InfrastructureConfig object.
func ValidateInfrastructureConfig(infra *stackitv1alpha1.InfrastructureConfig, nodesCIDR *string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(infra.FloatingPoolName) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("floatingPoolName"), "must provide the name of a floating pool"))
	}

	networkingPath := field.NewPath("networking")
	var nodes cidrvalidation.CIDR
	if nodesCIDR != nil {
		nodes = cidrvalidation.NewCIDR(*nodesCIDR, networkingPath.Child("nodes"))
	}

	networksPath := fldPath.Child("networks")
	if len(infra.Networks.Worker) == 0 && len(infra.Networks.Workers) == 0 {
		allErrs = append(allErrs, field.Required(networksPath.Child("workers"), "must specify the network range for the worker network"))
	}

	var workerCIDR cidrvalidation.CIDR
	if infra.Networks.Worker != "" {
		workerCIDR = cidrvalidation.NewCIDR(infra.Networks.Worker, networksPath.Child("worker"))
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRParse(workerCIDR)...)
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(networksPath.Child("worker"), infra.Networks.Worker)...)
	}
	if infra.Networks.Workers != "" {
		workerCIDR = cidrvalidation.NewCIDR(infra.Networks.Workers, networksPath.Child("workers"))
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRParse(workerCIDR)...)
		allErrs = append(allErrs, cidrvalidation.ValidateCIDRIsCanonical(networksPath.Child("workers"), infra.Networks.Workers)...)
	}

	if nodes != nil {
		allErrs = append(allErrs, nodes.ValidateSubset(workerCIDR)...)
	}

	if infra.Networks.ID != nil {
		if _, err := uuid.Parse(*infra.Networks.ID); err != nil {
			allErrs = append(allErrs, field.Invalid(networksPath.Child("id"), infra.Networks.ID, "if network ID is provided it must be a valid OpenStack UUID"))
		}
	}

	if infra.Networks.SubnetID != nil {
		if infra.Networks.ID == nil {
			allErrs = append(allErrs, field.Invalid(networksPath.Child("subnetId"), infra.Networks.SubnetID, "if subnet ID is provided a networkID must be provided"))
		}
		if _, err := uuid.Parse(*infra.Networks.SubnetID); err != nil {
			allErrs = append(allErrs, field.Invalid(networksPath.Child("subnetId"), infra.Networks.SubnetID, "if subnet ID is provided it must be a valid OpenStack UUID"))
		}
	}

	if infra.Networks.Router != nil && len(infra.Networks.Router.ID) == 0 {
		allErrs = append(allErrs, field.Invalid(networksPath.Child("router", "id"), infra.Networks.Router.ID, "router id must not be empty when router key is provided"))
	}

	if infra.FloatingPoolSubnetName != nil && infra.Networks.Router != nil && len(infra.Networks.Router.ID) > 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("floatingPoolSubnetName"), infra.FloatingPoolSubnetName, "router id must be empty when a floating subnet name is provided"))
	}

	return allErrs
}

// ValidateInfrastructureConfigUpdate validates a InfrastructureConfig object.
func ValidateInfrastructureConfigUpdate(oldConfig, newConfig *stackitv1alpha1.InfrastructureConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if oldConfig == nil {
		return allErrs
	}

	newNetworks := newConfig.DeepCopy().Networks
	oldNetworks := oldConfig.DeepCopy().Networks

	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newNetworks, oldNetworks, fldPath.Child("networks"))...)
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newConfig.FloatingPoolName, oldConfig.FloatingPoolName, fldPath.Child("floatingPoolName"))...)
	allErrs = append(allErrs, apivalidation.ValidateImmutableField(newConfig.FloatingPoolSubnetName, oldConfig.FloatingPoolSubnetName, fldPath.Child("floatingPoolSubnetName"))...)

	return allErrs
}

// ValidateInfrastructureConfigAgainstCloudProfile validates the given InfrastructureConfig against constraints in the given CloudProfile.
func ValidateInfrastructureConfigAgainstCloudProfile(oldInfra, infra *stackitv1alpha1.InfrastructureConfig, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if oldInfra == nil || oldInfra.FloatingPoolName != infra.FloatingPoolName {
		allErrs = append(allErrs, validateFloatingPoolNameConstraints(cloudProfileConfig.Constraints.FloatingPools, infra.FloatingPoolName, fldPath.Child("floatingPoolName")))
	}

	return allErrs
}

func validateFloatingPoolNameConstraints(fps []stackitv1alpha1.FloatingPool, name string, fldPath *field.Path) *field.Error {
	availablePoolNames := make([]string, 0, len(fps))
	for _, fp := range fps {
		availablePoolNames = append(availablePoolNames, fp.Name)
	}
	if !slices.Contains(availablePoolNames, name) {
		return field.NotSupported(fldPath, name, availablePoolNames)
	}
	return nil
}
