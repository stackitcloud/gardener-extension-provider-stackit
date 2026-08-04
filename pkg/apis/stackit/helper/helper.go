// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/utils"
)

// FindSubnetByPurpose takes a list of subnets and tries to find the first entry
// whose purpose matches with the given purpose. If no such entry is found then an error will be
// returned.
func FindSubnetByPurpose(subnets []stackitv1alpha1.Subnet, purpose stackitv1alpha1.Purpose) (*stackitv1alpha1.Subnet, error) {
	for _, subnet := range subnets {
		if subnet.Purpose == purpose {
			return &subnet, nil
		}
	}
	return nil, fmt.Errorf("cannot find subnet with purpose %q", purpose)
}

// FindSecurityGroupByPurpose takes a list of security groups and tries to find the first entry
// whose purpose matches with the given purpose. If no such entry is found then an error will be
// returned.
func FindSecurityGroupByPurpose(securityGroups []stackitv1alpha1.SecurityGroup, purpose stackitv1alpha1.Purpose) (*stackitv1alpha1.SecurityGroup, error) {
	for _, securityGroup := range securityGroups {
		if securityGroup.Purpose == purpose {
			return &securityGroup, nil
		}
	}
	return nil, fmt.Errorf("cannot find security group with purpose %q", purpose)
}

// FindImageInCloudProfile takes a list of machine images and tries to find the first entry whose name, version and capabilities
// matches with the machineTypeCapabilities. If no such entry is found then an error will be returned.
// Note: capabilityDefinitions and machineTypeCapabilities are expected to be normalized
// by the caller using NormalizeCapabilityDefinitions() and NormalizeMachineTypeCapabilities()
func FindImageInCloudProfile(
	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig,
	name, version, region string,
	machineCapabilities gardencorev1beta1.Capabilities,
	capabilityDefinitions []gardencorev1beta1.CapabilityDefinition,
) (*stackitv1alpha1.MachineImageFlavor, error) {
	if cloudProfileConfig == nil {
		return nil, fmt.Errorf("cloud profile config is nil")
	}
	if len(capabilityDefinitions) == 0 {
		return nil, fmt.Errorf("capabilityDefinitions must not be empty, use NormalizeCapabilityDefinitions() to ensure defaults")
	}
	machineImages := cloudProfileConfig.MachineImages

	for _, machineImage := range machineImages {
		if machineImage.Name != name {
			continue
		}

		// Collect all versions with matching version string (mixed format support)
		var matchingVersions []stackitv1alpha1.MachineImageVersion
		for _, v := range machineImage.Versions {
			if version == v.Version {
				matchingVersions = append(matchingVersions, v)
			}
		}

		if len(matchingVersions) == 0 {
			continue
		}

		// Convert old format (regions with architecture) versions to capability flavors if required
		// as there may be multiple version entries for the same version with different architectures
		// the normalization for capability flavors is done here instead of the caller to keep the caller code simpler
		capabilityFlavors := convertLegacyVersionsToCapabilityFlavors(matchingVersions)

		// Filter capability flavors by region
		filteredCapabilityFlavors := filterCapabilityFlavorsByRegion(capabilityFlavors, region)

		if len(filteredCapabilityFlavors) > 0 {
			bestMatch, err := worker.FindBestImageFlavor(filteredCapabilityFlavors, machineCapabilities, capabilityDefinitions)
			if err != nil {
				return nil, fmt.Errorf("could not determine best flavor: %w", err)
			}
			return bestMatch, nil
		}
	}
	return nil, fmt.Errorf("could not find an image for region %q, image %q, version %q that supports %v", region, name, version, machineCapabilities)
}

// convertLegacyVersionsToCapabilityFlavors converts old format (regions with architecture) versions
// to capability flavors for mixed format support.
func convertLegacyVersionsToCapabilityFlavors(versions []stackitv1alpha1.MachineImageVersion) []stackitv1alpha1.MachineImageFlavor {
	var capabilityFlavors []stackitv1alpha1.MachineImageFlavor
	for _, version := range versions {
		switch {
		case len(version.CapabilityFlavors) > 0:
			// New format: use capability flavors directly
			capabilityFlavors = append(capabilityFlavors, version.CapabilityFlavors...)

		case len(version.Regions) > 0:
			// Old format: regions with architecture - convert to capability flavors
			capabilityFlavors = append(capabilityFlavors, convertRegionsToCapabilityFlavors(version.Regions, version.Image)...)

		case version.Image != "":
			// Legacy format: only global image name, no regions - synthesize an amd64 capability flavor
			capabilityFlavors = append(capabilityFlavors, stackitv1alpha1.MachineImageFlavor{
				Capabilities: gardencorev1beta1.Capabilities{
					v1beta1constants.ArchitectureName: []string{v1beta1constants.ArchitectureAMD64},
				},
				Image: version.Image,
			})
		}
	}
	return capabilityFlavors
}

// convertRegionsToCapabilityFlavors converts old format (regions with architecture) to capability flavors.
// Groups regions by architecture, preserves the Image field, and strips Architecture from RegionIDMapping.
func convertRegionsToCapabilityFlavors(regions []stackitv1alpha1.RegionIDMapping, image string) []stackitv1alpha1.MachineImageFlavor {
	// Group regions by architecture
	architectureRegions := make(map[string][]stackitv1alpha1.RegionIDMapping)
	for _, region := range regions {
		arch := ptr.Deref(region.Architecture, v1beta1constants.ArchitectureAMD64)
		// Remove architecture field from region mapping when converting to capability flavors
		// as architecture is now expressed through the Capabilities field
		regionWithoutArch := stackitv1alpha1.RegionIDMapping{
			Name: region.Name,
			ID:   region.ID,
		}
		architectureRegions[arch] = append(architectureRegions[arch], regionWithoutArch)
	}

	// Create a capability flavor for each architecture
	capabilityFlavors := make([]stackitv1alpha1.MachineImageFlavor, 0, len(architectureRegions))
	for arch, regionMappings := range architectureRegions {
		capabilityFlavors = append(capabilityFlavors, stackitv1alpha1.MachineImageFlavor{
			Capabilities: gardencorev1beta1.Capabilities{
				v1beta1constants.ArchitectureName: []string{arch},
			},
			Regions: regionMappings,
			Image:   image,
		})
	}

	return capabilityFlavors
}

// FindImageInWorkerStatus takes a list of machine images from the worker status and tries to find the first entry whose name, version, architecture
// capabilities and zone matches with the machineTypeCapabilities. If no such entry is found then an error will be returned.
// The worker status is an external source that may contain images in either legacy format (Architecture field)
// or capability format (Capabilities field). The capabilityDefinitions parameter should be the original
// (non-normalized) spec.MachineCapabilities from the CloudProfile to distinguish the two cases.
// An empty architecture is treated as the default architecture (amd64).
func FindImageInWorkerStatus(machineImages []stackitv1alpha1.MachineImage, name string, version string, architecture string, machineCapabilities gardencorev1beta1.Capabilities, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) (*stackitv1alpha1.MachineImage, error) {
	if architecture == "" {
		architecture = v1beta1constants.ArchitectureAMD64
	}
	if len(capabilityDefinitions) == 0 {
		for _, statusMachineImage := range machineImages {
			if statusMachineImage.Name == name && statusMachineImage.Version == version && architecture == ptr.Deref(statusMachineImage.Architecture, v1beta1constants.ArchitectureAMD64) {
				return &statusMachineImage, nil
			}
		}
		return nil, fmt.Errorf("no machine image found for image %q with version %q and architecture %q", name, version, architecture)
	}

	// Capability format: find the best matching capability set.
	for _, statusMachineImage := range machineImages {
		if statusMachineImage.Name == name && statusMachineImage.Version == version && gardencorev1beta1helper.AreCapabilitiesCompatible(statusMachineImage.Capabilities, machineCapabilities, capabilityDefinitions) {
			return &statusMachineImage, nil
		}
	}
	return nil, fmt.Errorf("no machine image found for image %q with version %q and capabilities %v", name, version, machineCapabilities)
}

// filterCapabilityFlavorsByRegion returns a new list with capabilityFlavors that only contain RegionIDMappings
// of the region to filter for. Flavors with a global Image name but no matching region mapping are included
// without region details (they fall back to the global image name).
func filterCapabilityFlavorsByRegion(capabilityFlavors []stackitv1alpha1.MachineImageFlavor, regionName string) []*stackitv1alpha1.MachineImageFlavor {
	var compatibleFlavors []*stackitv1alpha1.MachineImageFlavor

	for _, capabilityFlavor := range capabilityFlavors {
		var regionIDMapping *stackitv1alpha1.RegionIDMapping
		for _, region := range capabilityFlavor.Regions {
			if region.Name == regionName {
				regionIDMapping = &region
			}
		}
		if regionIDMapping != nil {
			compatibleFlavors = append(compatibleFlavors, &stackitv1alpha1.MachineImageFlavor{
				Regions:      []stackitv1alpha1.RegionIDMapping{*regionIDMapping},
				Image:        capabilityFlavor.Image,
				Capabilities: capabilityFlavor.Capabilities,
			})
		} else if capabilityFlavor.Image != "" {
			compatibleFlavors = append(compatibleFlavors, &stackitv1alpha1.MachineImageFlavor{
				Image:        capabilityFlavor.Image,
				Capabilities: capabilityFlavor.Capabilities,
			})
		}
	}
	return compatibleFlavors
}

// NormalizeCapabilityDefinitions ensures that capability definitions always include at least
// the architecture capability. This allows all downstream code to assume capabilities are always present,
// eliminating the need for conditional logic based on whether capabilities are defined.
func NormalizeCapabilityDefinitions(capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) []gardencorev1beta1.CapabilityDefinition {
	if len(capabilityDefinitions) > 0 {
		return capabilityDefinitions
	}
	return []gardencorev1beta1.CapabilityDefinition{{
		Name:   v1beta1constants.ArchitectureName,
		Values: []string{v1beta1constants.ArchitectureAMD64, v1beta1constants.ArchitectureARM64},
	}}
}

// NormalizeMachineTypeCapabilities ensures that machine type capabilities include the architecture
// capability. This transforms the legacy architecture-based selection into capability-based selection.
// The architecture is determined in the following priority order:
// 1. If capabilities already has architecture, use it as-is
// 2. If capabilityDefinitions has exactly one architecture value, use that value
// 3. Otherwise, use workerArchitecture (defaulting to amd64)
func NormalizeMachineTypeCapabilities(capabilities gardencorev1beta1.Capabilities, workerArchitecture *string, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) gardencorev1beta1.Capabilities {
	if capabilities == nil {
		capabilities = make(gardencorev1beta1.Capabilities)
	}
	// If architecture capability is already present, return as-is
	if _, hasArch := capabilities[v1beta1constants.ArchitectureName]; hasArch {
		return capabilities
	}

	// Check if capabilityDefinitions has exactly one architecture value
	for _, def := range capabilityDefinitions {
		if def.Name == v1beta1constants.ArchitectureName && len(def.Values) == 1 {
			capabilities[v1beta1constants.ArchitectureName] = []string{def.Values[0]}
			return capabilities
		}
	}

	// Fall back to workerArchitecture or default
	arch := ptr.Deref(workerArchitecture, v1beta1constants.ArchitectureAMD64)
	capabilities[v1beta1constants.ArchitectureName] = []string{arch}
	return capabilities
}

// FindKeyStoneURL takes a list of keystone URLs and tries to find the first entry
// whose region matches with the given region. If no such entry is found then it tries to use the non-regional
// keystone URL. If this is not specified then an error will be returned.
func FindKeyStoneURL(keyStoneURLs []stackitv1alpha1.KeyStoneURL, keystoneURL, region string) (string, error) {
	for _, keyStoneURL := range keyStoneURLs {
		if keyStoneURL.Region == region {
			return keyStoneURL.URL, nil
		}
	}

	if len(keystoneURL) > 0 {
		return keystoneURL, nil
	}

	return "", fmt.Errorf("cannot find keystone URL for region %q", region)
}

// FindKeyStoneCACert takes a list of keystone URLs and tries to find the first entry
// whose region matches with the given region and returns the CA cert for this region. If no such entry is found then it
// tries to use the non-regional value.
func FindKeyStoneCACert(keyStoneURLs []stackitv1alpha1.KeyStoneURL, keystoneCABundle *string, region string) *string {
	for _, keyStoneURL := range keyStoneURLs {
		if keyStoneURL.Region == region && keyStoneURL.CACert != nil && len(*keyStoneURL.CACert) > 0 {
			return keyStoneURL.CACert
		}
	}

	return keystoneCABundle
}

// FindFloatingPool receives a list of floating pools and tries to find the best
// match for a given `floatingPoolNamePattern` considering constraints like
// `region` and `domain`. If no matching floating pool was found then an error will be returned.
func FindFloatingPool(floatingPools []stackitv1alpha1.FloatingPool, floatingPoolNamePattern, region string, domain *string) (*stackitv1alpha1.FloatingPool, error) {
	var (
		floatingPoolCandidate        *stackitv1alpha1.FloatingPool
		maxCandidateScore            int
		nonConstrainingFloatingPools []stackitv1alpha1.FloatingPool
	)

	for _, f := range floatingPools {
		var fip = f

		// Check non constraining floating pools with second priority
		// which means only when no other floating pool is matching.
		if fip.NonConstraining != nil && *fip.NonConstraining {
			nonConstrainingFloatingPools = append(nonConstrainingFloatingPools, fip)
			continue
		}

		if candidate, score := checkFloatingPoolCandidate(&fip, floatingPoolNamePattern, region, domain); candidate != nil && score > maxCandidateScore {
			floatingPoolCandidate = candidate
			maxCandidateScore = score
		}
	}

	if floatingPoolCandidate != nil {
		return floatingPoolCandidate, nil
	}

	// So far no floating pool was matching to the `floatingPoolNamePattern`
	// therefore try now if there is a non contraining floating pool matching.
	for _, f := range nonConstrainingFloatingPools {
		var fip = f
		if candidate, score := checkFloatingPoolCandidate(&fip, floatingPoolNamePattern, region, domain); candidate != nil && score > maxCandidateScore {
			floatingPoolCandidate = candidate
			maxCandidateScore = score
		}
	}

	if floatingPoolCandidate != nil {
		return floatingPoolCandidate, nil
	}

	return nil, fmt.Errorf("cannot find a matching floating pool for pattern %q", floatingPoolNamePattern)
}

func checkFloatingPoolCandidate(floatingPool *stackitv1alpha1.FloatingPool, floatingPoolNamePattern, region string, domain *string) (*stackitv1alpha1.FloatingPool, int) {
	// If the domain should be considered then only floating pools
	// in the same domain will be considered.
	if domain != nil && !utils.IsStringPtrValueEqual(floatingPool.Domain, *domain) {
		return nil, 0
	}

	// Require floating pools are in the same region.
	if !utils.IsStringPtrValueEqual(floatingPool.Region, region) {
		return nil, 0
	}

	// Check that the name of the current floatingPool is matching to the `floatingPoolNamePattern`.
	if isMatching, score := utils.SimpleMatch(floatingPool.Name, floatingPoolNamePattern); isMatching {
		return floatingPool, score
	}

	return nil, 0
}
