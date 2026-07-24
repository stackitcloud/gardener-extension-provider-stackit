// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"fmt"
	"maps"
	"net"
	"slices"

	gardencoreapi "github.com/gardener/gardener/pkg/api"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/apis/core"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/gardener"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

// ValidateCloudProfileConfig validates a CloudProfileConfig object.
func ValidateCloudProfileConfig(cloudProfile *stackitv1alpha1.CloudProfileConfig, machineImages []core.MachineImage, args ...any) field.ErrorList {
	allErrs := field.ErrorList{}
	capabilityDefinitions, fldPath := parseCloudProfileValidationArgs(args...)

	floatingPoolPath := fldPath.Child("constraints", "floatingPools")
	combinationFound := sets.NewString()
	//nolint:staticcheck // SA1019: needed for migration purposes
	for i, pool := range cloudProfile.Constraints.FloatingPools {
		idxPath := floatingPoolPath.Index(i)
		if len(pool.Name) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("name"), "must provide a name"))
		}

		if pool.Region != nil || pool.Domain != nil {
			region := "*"
			domain := "*"
			if pool.Region != nil {
				if len(*pool.Region) == 0 {
					allErrs = append(allErrs, field.Required(idxPath.Child("region"), "must provide a region if key is present"))
				}
				region = *pool.Region
			}
			if pool.Domain != nil {
				if len(*pool.Domain) == 0 {
					allErrs = append(allErrs, field.Required(idxPath.Child("domain"), "must provide a domain if key is present"))
				}
				domain = *pool.Domain
			}
			key := fmt.Sprintf("%s,%s,%s", pool.Name, domain, region)
			if combinationFound.Has(key) {
				// duplicate for given name/domain/region combination
				allErrs = append(allErrs, field.Duplicate(idxPath.Child("name"), pool.Name))
			}
			combinationFound.Insert(key)
		}
	}

	machineImagesPath := fldPath.Child("machineImages")
	if len(cloudProfile.MachineImages) == 0 {
		allErrs = append(allErrs, field.Required(machineImagesPath, "must provide at least one machine image"))
	}
	for i, machineImage := range cloudProfile.MachineImages {
		idxPath := machineImagesPath.Index(i)
		allErrs = append(allErrs, ValidateProviderMachineImage(machineImage, capabilityDefinitions, idxPath)...)
	}
	allErrs = append(allErrs, validateMachineImageMapping(machineImages, cloudProfile, capabilityDefinitions, field.NewPath("spec").Child("machineImages"))...)
	//nolint:staticcheck // SA1019: needed for migration purposes
	if ca := cloudProfile.KeyStoneCACert; ca != nil && len(*ca) > 0 {
		_, err := utils.DecodeCertificate([]byte(*ca))
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("caCert"), *ca, "caCert is not a valid PEM-encoded certificate"))
		}
	}

	regionsFound := sets.NewString()
	//nolint:staticcheck // SA1019: needed for migration purposes
	for i, val := range cloudProfile.KeyStoneURLs {
		idxPath := fldPath.Child("keyStoneURLs").Index(i)

		if len(val.Region) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("region"), "must provide a region"))
		}

		if len(val.URL) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("url"), "must provide an url"))
		}

		if ca := val.CACert; ca != nil && len(*ca) > 0 {
			_, err := utils.DecodeCertificate([]byte(*ca))
			if err != nil {
				allErrs = append(allErrs, field.Invalid(idxPath.Child("caCert"), *ca, "caCert is not a valid PEM-encoded certificate"))
			}
		}

		if regionsFound.Has(val.Region) {
			allErrs = append(allErrs, field.Duplicate(idxPath.Child("region"), val.Region))
		}
		regionsFound.Insert(val.Region)
	}

	for i, ip := range cloudProfile.DNSServers {
		if net.ParseIP(ip) == nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("dnsServers").Index(i), ip, "must provide a valid IP"))
		}
	}

	//nolint:staticcheck // SA1019: needed for migration purposes
	if cloudProfile.DHCPDomain != nil && len(*cloudProfile.DHCPDomain) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("dhcpDomain"), "must provide a dhcp domain when the key is specified"))
	}

	serverGroupPath := fldPath.Child("serverGroupPolicies")
	//nolint:staticcheck // SA1019: needed for migration purposes
	for i, policy := range cloudProfile.ServerGroupPolicies {
		idxPath := serverGroupPath.Index(i)

		if len(policy) == 0 {
			allErrs = append(allErrs, field.Required(idxPath, "policy cannot be empty"))
		}
	}

	return allErrs
}

func parseCloudProfileValidationArgs(args ...any) ([]gardencorev1beta1.CapabilityDefinition, *field.Path) {
	switch len(args) {
	case 1:
		return nil, args[0].(*field.Path)
	case 2:
		return args[0].([]gardencorev1beta1.CapabilityDefinition), args[1].(*field.Path)
	default:
		panic("ValidateCloudProfileConfig expects fldPath or capabilityDefinitions and fldPath")
	}
}

// ValidateProviderMachineImage validates a CloudProfileConfig MachineImages entry.
func ValidateProviderMachineImage(providerImage stackitv1alpha1.MachineImages, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, validationPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(providerImage.Name) == 0 {
		allErrs = append(allErrs, field.Required(validationPath.Child("name"), "must provide a name"))
	}

	if len(providerImage.Versions) == 0 {
		allErrs = append(allErrs, field.Required(validationPath.Child("versions"), fmt.Sprintf("must provide at least one version for machine image %q", providerImage.Name)))
	}
	for j, version := range providerImage.Versions {
		jdxPath := validationPath.Child("versions").Index(j)
		allErrs = append(allErrs, validateMachineImageVersion(version, capabilityDefinitions, jdxPath)...)
	}

	return allErrs
}

func validateMachineImageVersion(version stackitv1alpha1.MachineImageVersion, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if len(version.Version) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("version"), "must provide a version"))
	}

	if len(capabilityDefinitions) > 0 {
		if len(version.Regions) > 0 {
			allErrs = append(allErrs, field.Forbidden(fldPath.Child("regions"), "must not be set as CloudProfile defines capabilities. Use capabilityFlavors.regions instead."))
		}
		for k, capabilityFlavor := range version.CapabilityFlavors {
			kdxPath := fldPath.Child("capabilityFlavors").Index(k)
			allErrs = append(allErrs, gardener.ValidateCapabilities(capabilityFlavor.Capabilities, capabilityDefinitions, kdxPath.Child("capabilities"))...)
			allErrs = append(allErrs, validateRegions(capabilityFlavor.Regions, capabilityDefinitions, kdxPath)...)
		}
		return allErrs
	}

	allErrs = append(allErrs, validateRegions(version.Regions, capabilityDefinitions, fldPath)...)
	if len(version.CapabilityFlavors) > 0 {
		allErrs = append(allErrs, field.Forbidden(fldPath.Child("capabilityFlavors"), "must not be set as CloudProfile does not define capabilities. Use regions instead."))
	}
	return allErrs
}

func validateRegions(regions []stackitv1alpha1.RegionIDMapping, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	for k, region := range regions {
		kdxPath := fldPath.Child("regions").Index(k)
		architecture := ptr.Deref(region.Architecture, v1beta1constants.ArchitectureAMD64)

		if len(region.Name) == 0 {
			allErrs = append(allErrs, field.Required(kdxPath.Child("name"), "must provide a name"))
		}
		if len(region.ID) == 0 {
			allErrs = append(allErrs, field.Required(kdxPath.Child("id"), "must provide an image ID"))
		}
		if len(capabilityDefinitions) == 0 && !slices.Contains(v1beta1constants.ValidArchitectures, architecture) {
			allErrs = append(allErrs, field.NotSupported(kdxPath.Child("architecture"), architecture, v1beta1constants.ValidArchitectures))
		}
		if len(capabilityDefinitions) > 0 && region.Architecture != nil {
			allErrs = append(allErrs, field.Forbidden(kdxPath.Child("architecture"), "must be defined in .capabilities.architecture"))
		}
	}

	return allErrs
}

// NewProviderImagesContext creates a new ImagesContext for provider images.
func NewProviderImagesContext(providerImages []stackitv1alpha1.MachineImages) *gardener.ImagesContext[stackitv1alpha1.MachineImages, stackitv1alpha1.MachineImageVersion] {
	return gardener.NewImagesContext(
		utils.CreateMapFromSlice(providerImages, func(mi stackitv1alpha1.MachineImages) string { return mi.Name }),
		func(mi stackitv1alpha1.MachineImages) map[string]stackitv1alpha1.MachineImageVersion {
			return utils.CreateMapFromSlice(mi.Versions, func(v stackitv1alpha1.MachineImageVersion) string { return v.Version })
		},
	)
}

// validateMachineImageMapping validates that for each machine image there is a corresponding cpConfig image.
func validateMachineImageMapping(machineImages []core.MachineImage, cpConfig *stackitv1alpha1.CloudProfileConfig, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	providerImages := NewProviderImagesContext(cpConfig.MachineImages)

	// validate machine images
	for idxImage, machineImage := range machineImages {
		if len(machineImage.Versions) == 0 {
			continue
		}
		machineImagePath := fldPath.Index(idxImage)
		// validate that for each machine image there is a corresponding cpConfig image
		if _, existsInConfig := providerImages.GetImage(machineImage.Name); !existsInConfig {
			allErrs = append(allErrs, field.Required(machineImagePath,
				fmt.Sprintf("must provide an image mapping for image %q in providerConfig", machineImage.Name)))
			continue
		}
		// validate that for each machine image version entry a mapped entry in cpConfig exists
		for idxVersion, version := range machineImage.Versions {
			machineImageVersionPath := machineImagePath.Child("versions").Index(idxVersion)
			imageVersion, exists := providerImages.GetImageVersion(machineImage.Name, version.Version)
			if !exists {
				allErrs = append(allErrs, field.Required(machineImageVersionPath,
					fmt.Sprintf("machine image version %s@%s is not defined in the providerConfig",
						machineImage.Name, version.Version),
				))
				continue
			}

			if len(capabilityDefinitions) > 0 {
				allErrs = append(allErrs, validateImageFlavorMapping(machineImage, version, machineImageVersionPath, capabilityDefinitions, imageVersion)...)
				continue
			}

			for _, expectedArchitecture := range version.Architectures {
				if !slices.Contains(v1beta1constants.ValidArchitectures, expectedArchitecture) {
					allErrs = append(allErrs, field.NotSupported(machineImageVersionPath.Child("architectures"), expectedArchitecture, v1beta1constants.ValidArchitectures))
				}

				// Regions is an optional field
				if len(imageVersion.Regions) > 0 {
					architecturesMap := utils.CreateMapFromSlice(imageVersion.Regions, func(re stackitv1alpha1.RegionIDMapping) string {
						return ptr.Deref(re.Architecture, v1beta1constants.ArchitectureAMD64)
					})
					architectures := slices.Collect(maps.Keys(architecturesMap))
					if !slices.Contains(architectures, expectedArchitecture) {
						allErrs = append(allErrs, field.Required(machineImageVersionPath,
							fmt.Sprintf("missing providerConfig mapping for machine image version %s@%s and architecture: %s",
								machineImage.Name, version.Version, expectedArchitecture),
						))
					}
				}
			}
		}
	}

	return allErrs
}

func validateImageFlavorMapping(machineImage core.MachineImage, version core.MachineImageVersion, machineImageVersionPath *field.Path, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, imageVersion stackitv1alpha1.MachineImageVersion) field.ErrorList {
	allErrs := field.ErrorList{}

	var v1beta1Version gardencorev1beta1.MachineImageVersion
	if err := gardencoreapi.Scheme.Convert(&version, &v1beta1Version, nil); err != nil {
		return append(allErrs, field.InternalError(machineImageVersionPath, err))
	}

	defaultedCapabilityFlavors := gardencorev1beta1helper.GetImageFlavorsWithAppliedDefaults(v1beta1Version.CapabilityFlavors, capabilityDefinitions)
	for idxCapability, defaultedCapabilitySet := range defaultedCapabilityFlavors {
		isFound := false
		for _, providerCapabilitySet := range imageVersion.CapabilityFlavors {
			providerDefaultedCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(providerCapabilitySet.Capabilities, capabilityDefinitions)
			if gardencorev1beta1helper.AreCapabilitiesEqual(defaultedCapabilitySet.Capabilities, providerDefaultedCapabilities) {
				isFound = true
				break
			}
		}
		if !isFound {
			allErrs = append(allErrs, field.Required(machineImageVersionPath.Child("capabilityFlavors").Index(idxCapability),
				fmt.Sprintf("missing providerConfig mapping for machine image version %s@%s and capabilitySet %v", machineImage.Name, version.Version, defaultedCapabilitySet.Capabilities)))
		}
	}
	return allErrs
}
