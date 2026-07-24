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

// FindMachineImage takes a list of machine images and tries to find the first entry
// whose name, version, and zone matches with the given name, version, and cloud profile. If no such
// entry is found then an error will be returned.
func FindMachineImage(machineImages []stackitv1alpha1.MachineImage, name, version, architecture string) (*stackitv1alpha1.MachineImage, error) {
	for _, machineImage := range machineImages {
		// If the architecture field is not present, ignore it for backwards-compatibility.
		if machineImage.Name == name && machineImage.Version == version &&
			(machineImage.Architecture == nil || *machineImage.Architecture == architecture) {
			return &machineImage, nil
		}
	}
	return nil, fmt.Errorf("no machine image with name %q, version %q found", name, version)
}

// FindImageFromCloudProfile takes a list of machine images, and the desired image name and version. It tries
// to find the image with the given name and version in the desired cloud profile. If it cannot be found then an error
// is returned.
func FindImageFromCloudProfile(cloudProfileConfig *stackitv1alpha1.CloudProfileConfig, imageName, imageVersion, regionName, architecture string) (*stackitv1alpha1.MachineImage, error) {
	if cloudProfileConfig != nil {
		for _, machineImage := range cloudProfileConfig.MachineImages {
			if machineImage.Name != imageName {
				continue
			}
			for _, version := range machineImage.Versions {
				if imageVersion != version.Version {
					continue
				}
				for _, region := range version.Regions {
					if regionName == region.Name && architecture == ptr.Deref(region.Architecture, v1beta1constants.ArchitectureAMD64) {
						return &stackitv1alpha1.MachineImage{
							Name:         imageName,
							Version:      imageVersion,
							Architecture: &architecture,
							ID:           region.ID,
						}, nil
					}
				}

				// if we haven't found a region mapping, fallback to the image name
				if version.Image != "" && architecture == v1beta1constants.ArchitectureAMD64 {
					// The fallback image name doesn't specify an architecture, but we assume it is amd64 as arm was not supported
					// previously.
					// Referencing images by name is error-prone and is highly discouraged anyways.
					// If people want to use arm images in their CloudProfile, they need to specify a region mapping and can't
					// use the fallback MachineImage by name.
					return &stackitv1alpha1.MachineImage{
						Name:         imageName,
						Version:      imageVersion,
						Architecture: new(v1beta1constants.ArchitectureAMD64),
						Image:        version.Image,
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("could not find an image for name %q in version %q for region %q", imageName, imageVersion, regionName)
}

// FindImageInCloudProfile finds the best matching machine image flavor for the given image, region, architecture, and capabilities.
func FindImageInCloudProfile(
	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig,
	name, version, region string,
	arch *string,
	machineCapabilities gardencorev1beta1.Capabilities,
	capabilityDefinitions []gardencorev1beta1.CapabilityDefinition,
) (*stackitv1alpha1.MachineImageFlavor, error) {
	if cloudProfileConfig == nil {
		return nil, fmt.Errorf("cloud profile config is nil")
	}

	capabilitySet, err := findMachineImageFlavor(cloudProfileConfig.MachineImages, name, version, region, arch, machineCapabilities, capabilityDefinitions)
	if err != nil {
		return nil, fmt.Errorf("could not find an image for region %q, image %q, version %q that supports %v: %w", region, name, version, machineCapabilities, err)
	}
	if capabilitySet != nil && len(capabilitySet.Regions) > 0 && (capabilitySet.Regions[0].ID != "" || capabilitySet.Image != "") {
		return capabilitySet, nil
	}
	return nil, fmt.Errorf("could not find an image for region %q, image %q, version %q that supports %v", region, name, version, machineCapabilities)
}

// FindImageInWorkerStatus finds a previously selected machine image in the Worker provider status.
func FindImageInWorkerStatus(machineImages []stackitv1alpha1.MachineImage, name, version string, architecture *string, machineCapabilities gardencorev1beta1.Capabilities, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) (*stackitv1alpha1.MachineImage, error) {
	if len(capabilityDefinitions) == 0 {
		for _, statusMachineImage := range machineImages {
			if statusMachineImage.Architecture == nil {
				statusMachineImage.Architecture = ptr.To(v1beta1constants.ArchitectureAMD64)
			}
			if statusMachineImage.Name == name && statusMachineImage.Version == version && ptr.Equal(architecture, statusMachineImage.Architecture) {
				return &statusMachineImage, nil
			}
		}
		return nil, fmt.Errorf("no machine image found for image %q with version %q and architecture %q", name, version, ptr.Deref(architecture, ""))
	}

	for _, statusMachineImage := range machineImages {
		if statusMachineImage.Name == name && statusMachineImage.Version == version && gardencorev1beta1helper.AreCapabilitiesCompatible(statusMachineImage.Capabilities, machineCapabilities, capabilityDefinitions) {
			return &statusMachineImage, nil
		}
	}
	return nil, fmt.Errorf("no machine image found for image %q with version %q and capabilities %v", name, version, machineCapabilities)
}

func findMachineImageFlavor(
	machineImages []stackitv1alpha1.MachineImages,
	imageName, imageVersion, region string,
	arch *string,
	machineCapabilities gardencorev1beta1.Capabilities,
	capabilityDefinitions []gardencorev1beta1.CapabilityDefinition,
) (*stackitv1alpha1.MachineImageFlavor, error) {
	for _, machineImage := range machineImages {
		if machineImage.Name != imageName {
			continue
		}
		for _, version := range machineImage.Versions {
			if imageVersion != version.Version {
				continue
			}

			if len(capabilityDefinitions) == 0 {
				for _, mapping := range version.Regions {
					if region == mapping.Name && ptr.Equal(arch, mapping.Architecture) {
						return &stackitv1alpha1.MachineImageFlavor{
							Image:        version.Image,
							Regions:      []stackitv1alpha1.RegionIDMapping{mapping},
							Capabilities: gardencorev1beta1.Capabilities{},
						}, nil
					}
				}
				if version.Image != "" && ptr.Deref(arch, v1beta1constants.ArchitectureAMD64) == v1beta1constants.ArchitectureAMD64 {
					return &stackitv1alpha1.MachineImageFlavor{
						Image: version.Image,
						Regions: []stackitv1alpha1.RegionIDMapping{{
							Name:         region,
							Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
						}},
						Capabilities: gardencorev1beta1.Capabilities{},
					}, nil
				}
				continue
			}

			filteredCapabilityFlavors := filterCapabilityFlavorsByRegion(version.CapabilityFlavors, region)
			bestMatch, err := worker.FindBestImageFlavor(filteredCapabilityFlavors, machineCapabilities, capabilityDefinitions)
			if err != nil {
				return nil, fmt.Errorf("could not determine best flavor: %w", err)
			}
			return bestMatch, nil
		}
	}
	return nil, nil
}

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
		}
	}
	return compatibleFlavors
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
