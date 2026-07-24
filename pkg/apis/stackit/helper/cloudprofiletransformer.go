// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"slices"

	gardencoreapi "github.com/gardener/gardener/pkg/api"
	"github.com/gardener/gardener/pkg/apis/core"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

// SimulateTransformToParentFormat simulates the transformation of the given NamespacedCloudProfile and providerConfig
// to the parent CloudProfile format.
func SimulateTransformToParentFormat(cloudProfileConfig *stackitv1alpha1.CloudProfileConfig, cloudProfile *core.NamespacedCloudProfile, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) error {
	namespacedCloudProfileSpecV1beta1 := gardencorev1beta1.NamespacedCloudProfileSpec{}
	if err := gardencoreapi.Scheme.Convert(&cloudProfile.Spec, &namespacedCloudProfileSpecV1beta1, nil); err != nil {
		return field.InternalError(field.NewPath("spec"), err)
	}

	*cloudProfileConfig = *TransformProviderConfigToParentFormat(cloudProfileConfig, capabilityDefinitions)
	transformedSpec := gutil.TransformSpecToParentFormat(namespacedCloudProfileSpecV1beta1, capabilityDefinitions)

	if err := gardencoreapi.Scheme.Convert(&transformedSpec, &cloudProfile.Spec, nil); err != nil {
		return field.InternalError(field.NewPath("spec"), err)
	}
	return nil
}

// TransformProviderConfigToParentFormat supports migration between deprecated architecture fields and architecture capabilities.
func TransformProviderConfigToParentFormat(config *stackitv1alpha1.CloudProfileConfig, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) *stackitv1alpha1.CloudProfileConfig {
	if config == nil {
		return &stackitv1alpha1.CloudProfileConfig{}
	}

	transformedConfig := config.DeepCopy()
	transformedConfig.MachineImages = transformMachineImages(config.MachineImages, capabilityDefinitions)
	return transformedConfig
}

func transformMachineImages(images []stackitv1alpha1.MachineImages, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) []stackitv1alpha1.MachineImages {
	result := make([]stackitv1alpha1.MachineImages, 0, len(images))

	for _, img := range images {
		result = append(result, stackitv1alpha1.MachineImages{
			Name:     img.Name,
			Versions: transformImageVersions(img.Versions, capabilityDefinitions),
		})
	}

	return result
}

func transformImageVersions(versions []stackitv1alpha1.MachineImageVersion, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) []stackitv1alpha1.MachineImageVersion {
	result := make([]stackitv1alpha1.MachineImageVersion, 0, len(versions))

	for _, version := range versions {
		transformed := stackitv1alpha1.MachineImageVersion{
			Version: version.Version,
			Image:   version.Image,
		}
		if len(capabilityDefinitions) != 0 {
			transformed.CapabilityFlavors = transformToCapabilityFormat(version, capabilityDefinitions)
		} else {
			transformed.Regions = transformToLegacyFormat(version)
		}
		result = append(result, transformed)
	}

	return result
}

func transformToCapabilityFormat(version stackitv1alpha1.MachineImageVersion, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) []stackitv1alpha1.MachineImageFlavor {
	if len(version.CapabilityFlavors) > 0 {
		return version.CapabilityFlavors
	}
	if len(version.Regions) == 0 {
		return nil
	}

	architectureGroups := make(map[string][]stackitv1alpha1.RegionIDMapping)
	for _, region := range version.Regions {
		arch := ptr.Deref(region.Architecture, v1beta1constants.ArchitectureAMD64)
		architectureGroups[arch] = append(architectureGroups[arch], stackitv1alpha1.RegionIDMapping{
			Name: region.Name,
			ID:   region.ID,
		})
	}

	imageFlavors := make([]stackitv1alpha1.MachineImageFlavor, 0, len(architectureGroups))
	for arch, regions := range architectureGroups {
		sortRegions(regions)
		imageFlavors = append(imageFlavors, stackitv1alpha1.MachineImageFlavor{
			Regions: regions,
			Capabilities: gardencorev1beta1.Capabilities{
				v1beta1constants.ArchitectureName: []string{arch},
			},
		})
	}

	slices.SortFunc(imageFlavors, func(a, b stackitv1alpha1.MachineImageFlavor) int {
		return cmpStrings(getFirstArchitectureValue(a.Capabilities, capabilityDefinitions), getFirstArchitectureValue(b.Capabilities, capabilityDefinitions))
	})

	return imageFlavors
}

func transformToLegacyFormat(version stackitv1alpha1.MachineImageVersion) []stackitv1alpha1.RegionIDMapping {
	if len(version.Regions) > 0 {
		return version.Regions
	}
	if len(version.CapabilityFlavors) == 0 {
		return nil
	}

	var regions []stackitv1alpha1.RegionIDMapping
	for _, flavor := range version.CapabilityFlavors {
		arch := getFirstArchitectureValue(flavor.Capabilities, nil)
		for _, region := range flavor.Regions {
			regions = append(regions, stackitv1alpha1.RegionIDMapping{
				Name:         region.Name,
				ID:           region.ID,
				Architecture: &arch,
			})
		}
	}
	sortRegions(regions)
	return regions
}

func sortRegions(regions []stackitv1alpha1.RegionIDMapping) {
	slices.SortFunc(regions, func(a, b stackitv1alpha1.RegionIDMapping) int {
		return cmpStrings(a.Name, b.Name)
	})
}

func getFirstArchitectureValue(capabilities gardencorev1beta1.Capabilities, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) string {
	defaultedCapabilities := capabilities
	if len(capabilityDefinitions) > 0 {
		defaultedCapabilities = gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(capabilities, capabilityDefinitions)
	}
	if defaultedCapabilities == nil {
		return v1beta1constants.ArchitectureAMD64
	}
	architectures := defaultedCapabilities[v1beta1constants.ArchitectureName]
	if len(architectures) == 0 {
		return v1beta1constants.ArchitectureAMD64
	}
	return architectures[0]
}

func cmpStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
