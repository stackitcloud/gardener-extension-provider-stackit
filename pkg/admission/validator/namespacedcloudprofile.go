// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"fmt"
	"slices"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencoreapi "github.com/gardener/gardener/pkg/api"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/apis/core"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/utils/gardener"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	stackithelper "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

// NewNamespacedCloudProfileValidator returns a new instance of a namespaced cloud profile validator.
func NewNamespacedCloudProfileValidator(mgr manager.Manager) extensionswebhook.Validator {
	return &namespacedCloudProfile{
		client: mgr.GetClient(),
	}
}

type namespacedCloudProfile struct {
	client client.Client
}

// Validate validates the given NamespacedCloudProfile objects.
func (p *namespacedCloudProfile) Validate(ctx context.Context, newObj, _ client.Object) error {
	profile, ok := newObj.(*core.NamespacedCloudProfile)
	if !ok {
		return fmt.Errorf("wrong object type %T", newObj)
	}

	if profile.DeletionTimestamp != nil {
		return nil
	}

	cpConfig, err := stackithelper.CloudProfileConfigFromRawExtension(profile.Spec.ProviderConfig)
	if err != nil {
		return err
	}

	parentCloudProfile := profile.Spec.Parent
	if parentCloudProfile.Kind != constants.CloudProfileReferenceKindCloudProfile {
		return fmt.Errorf("parent reference must be of kind CloudProfile (unsupported kind: %s)", parentCloudProfile.Kind)
	}
	parentProfile := &gardencorev1beta1.CloudProfile{}
	if err := p.client.Get(ctx, client.ObjectKey{Name: parentCloudProfile.Name}, parentProfile); err != nil {
		return err
	}

	allErrs := field.ErrorList{}
	if err := p.validateMachineImagesAndAPIEndpointsOnlyInNamespacedCloudProfile(cpConfig); err != nil {
		allErrs = append(allErrs, err.(*field.Error))
	}

	if err := stackithelper.SimulateTransformToParentFormat(cpConfig, profile, parentProfile.Spec.MachineCapabilities); err != nil {
		return err
	}

	allErrs = append(allErrs, p.validateMachineImages(cpConfig, profile.Spec.MachineImages, parentProfile.Spec)...)
	return allErrs.ToAggregate()
}

func (p *namespacedCloudProfile) validateMachineImagesAndAPIEndpointsOnlyInNamespacedCloudProfile(providerConfig *stackitv1alpha1.CloudProfileConfig) error {
	validationProviderConfig := &stackitv1alpha1.CloudProfileConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "CloudProfileConfig",
		},
		MachineImages: providerConfig.MachineImages,
		APIEndpoints:  providerConfig.APIEndpoints,
	}
	if !equality.Semantic.DeepEqual(validationProviderConfig, providerConfig) {
		return field.Forbidden(
			field.NewPath("spec.providerConfig"),
			"must only set machineImages and stackitAPIEndpoints",
		)
	}
	return nil
}

func (p *namespacedCloudProfile) validateMachineImages(providerConfig *stackitv1alpha1.CloudProfileConfig, machineImages []core.MachineImage, parentSpec gardencorev1beta1.CloudProfileSpec) field.ErrorList {
	allErrs := field.ErrorList{}

	machineImagesPath := field.NewPath("spec.providerConfig.machineImages")
	for i, machineImage := range providerConfig.MachineImages {
		idxPath := machineImagesPath.Index(i)
		allErrs = append(allErrs, validation.ValidateProviderMachineImage(machineImage, parentSpec.MachineCapabilities, idxPath)...)
	}

	profileImages := gardener.NewCoreImagesContext(machineImages)
	parentImages := gardener.NewV1beta1ImagesContext(parentSpec.MachineImages)
	providerImages := validation.NewProviderImagesContext(providerConfig.MachineImages)

	for _, machineImage := range profileImages.Images {
		// Check that for each new image version defined in the NamespacedCloudProfile, the image is also defined in the providerConfig.
		_, existsInParent := parentImages.GetImage(machineImage.Name)
		if _, existsInProvider := providerImages.GetImage(machineImage.Name); !existsInParent && !existsInProvider {
			allErrs = append(allErrs, field.Required(
				field.NewPath("spec.providerConfig.machineImages"),
				fmt.Sprintf("machine image %s is not defined in the NamespacedCloudProfile providerConfig", machineImage.Name),
			))
			continue
		}
		for _, version := range machineImage.Versions {
			_, existsInParent := parentImages.GetImageVersion(machineImage.Name, version.Version)
			providerImageVersion, exists := providerImages.GetImageVersion(machineImage.Name, version.Version)
			if !existsInParent && !exists {
				allErrs = append(allErrs, field.Required(
					field.NewPath("spec.providerConfig.machineImages"),
					fmt.Sprintf("machine image version %s@%s is not defined in the NamespacedCloudProfile providerConfig", machineImage.Name, version.Version),
				))
				continue
			}

			if len(parentSpec.MachineCapabilities) == 0 {
				allErrs = append(allErrs, validateMachineImageArchitectures(machineImage, version, providerImageVersion)...)
			} else {
				var v1beta1Version gardencorev1beta1.MachineImageVersion
				if err := gardencoreapi.Scheme.Convert(&version, &v1beta1Version, nil); err != nil {
					return append(allErrs, field.InternalError(machineImagesPath, err))
				}
				allErrs = append(allErrs, validateMachineImageCapabilities(machineImage, v1beta1Version, providerImageVersion, parentSpec.MachineCapabilities)...)
			}
		}
	}
	for imageIdx, machineImage := range providerConfig.MachineImages {
		// Check that the machine image version is not already defined in the parent CloudProfile.
		if _, exists := parentImages.GetImage(machineImage.Name); exists {
			for versionIdx, version := range machineImage.Versions {
				if _, exists := parentImages.GetImageVersion(machineImage.Name, version.Version); exists {
					allErrs = append(allErrs, field.Forbidden(
						field.NewPath("spec.providerConfig.machineImages").Index(imageIdx).Child("versions").Index(versionIdx),
						fmt.Sprintf("machine image version %s@%s is already defined in the parent CloudProfile", machineImage.Name, version.Version),
					))
				}
			}
		}
		// Check that the machine image version is defined in the NamespacedCloudProfile.
		if _, exists := profileImages.GetImage(machineImage.Name); !exists {
			allErrs = append(allErrs, field.Required(
				field.NewPath("spec.providerConfig.machineImages").Index(imageIdx),
				fmt.Sprintf("machine image %s is not defined in the NamespacedCloudProfile .spec.machineImages", machineImage.Name),
			))
			continue
		}
		for versionIdx, version := range machineImage.Versions {
			if _, exists := profileImages.GetImageVersion(machineImage.Name, version.Version); !exists {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec.providerConfig.machineImages").Index(imageIdx).Child("versions").Index(versionIdx),
					fmt.Sprintf("%s@%s", machineImage.Name, version.Version),
					"machine image version is not defined in the NamespacedCloudProfile",
				))
			}
		}
	}

	return allErrs
}

func validateMachineImageCapabilities(machineImage core.MachineImage, version gardencorev1beta1.MachineImageVersion, providerImageVersion stackitv1alpha1.MachineImageVersion, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) field.ErrorList {
	allErrs := field.ErrorList{}
	path := field.NewPath("spec.providerConfig.machineImages")
	defaultedCapabilityFlavors := gardencorev1beta1helper.GetImageFlavorsWithAppliedDefaults(version.CapabilityFlavors, capabilityDefinitions)
	regionsCapabilitiesMap := map[string][]gardencorev1beta1.Capabilities{}

	for _, capabilityFlavor := range providerImageVersion.CapabilityFlavors {
		isFound := false
		for _, coreDefaultedCapabilitySet := range defaultedCapabilityFlavors {
			defaultedProviderCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(capabilityFlavor.Capabilities, capabilityDefinitions)
			if gardencorev1beta1helper.AreCapabilitiesEqual(coreDefaultedCapabilitySet.Capabilities, defaultedProviderCapabilities) {
				isFound = true
			}
		}
		if !isFound {
			allErrs = append(allErrs, field.Forbidden(path,
				fmt.Sprintf("machine image version %s@%s has an excess capabilityFlavor %v, which is not defined in the machineImages spec",
					machineImage.Name, version.Version, capabilityFlavor.Capabilities)))
		}

		for _, regionMapping := range capabilityFlavor.Regions {
			regionsCapabilitiesMap[regionMapping.Name] = append(regionsCapabilitiesMap[regionMapping.Name], capabilityFlavor.Capabilities)
		}
	}

	for _, coreDefaultedCapabilityFlavor := range defaultedCapabilityFlavors {
		isFound := false
		for _, capabilityFlavor := range providerImageVersion.CapabilityFlavors {
			defaultedProviderCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(capabilityFlavor.Capabilities, capabilityDefinitions)
			if gardencorev1beta1helper.AreCapabilitiesEqual(coreDefaultedCapabilityFlavor.Capabilities, defaultedProviderCapabilities) {
				isFound = true
			}
		}
		if !isFound {
			allErrs = append(allErrs, field.Required(path,
				fmt.Sprintf("machine image version %s@%s has a capabilityFlavor %v not defined in the NamespacedCloudProfile providerConfig",
					machineImage.Name, version.Version, coreDefaultedCapabilityFlavor.Capabilities)))
			continue
		}

		for region, regionCapabilities := range regionsCapabilitiesMap {
			isFound := false
			for _, capabilities := range regionCapabilities {
				regionDefaultedCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(capabilities, capabilityDefinitions)
				if gardencorev1beta1helper.AreCapabilitiesEqual(regionDefaultedCapabilities, coreDefaultedCapabilityFlavor.Capabilities) {
					isFound = true
				}
			}
			if !isFound {
				allErrs = append(allErrs, field.Required(path,
					fmt.Sprintf("machine image version %s@%s is missing region %q in capabilityFlavor %v in the NamespacedCloudProfile providerConfig",
						machineImage.Name, version.Version, region, coreDefaultedCapabilityFlavor.Capabilities)))
			}
		}
	}

	return allErrs
}

func validateMachineImageArchitectures(machineImage core.MachineImage, version core.MachineImageVersion, providerImageVersion stackitv1alpha1.MachineImageVersion) field.ErrorList {
	allErrs := field.ErrorList{}
	regionsArchitectureMap := map[string][]string{}

	for _, regionMapping := range providerImageVersion.Regions {
		providerConfigArchitecture := ptr.Deref(regionMapping.Architecture, constants.ArchitectureAMD64)
		if !slices.Contains(version.Architectures, providerConfigArchitecture) {
			allErrs = append(allErrs, field.Forbidden(
				field.NewPath("spec.providerConfig.machineImages"),
				fmt.Sprintf("machine image version %s@%s in region %q has an excess entry for architecture %q, which is not defined in the machineImages spec",
					machineImage.Name, version.Version, regionMapping.Name, providerConfigArchitecture),
			))
		}
		regionsArchitectureMap[regionMapping.Name] = append(regionsArchitectureMap[regionMapping.Name], providerConfigArchitecture)
	}

	for _, expectedArchitecture := range version.Architectures {
		if len(regionsArchitectureMap) == 0 {
			allErrs = append(allErrs, field.Required(
				field.NewPath("spec.providerConfig.machineImages"),
				fmt.Sprintf("machine image version %s@%s with architecture %q is not defined in the NamespacedCloudProfile providerConfig",
					machineImage.Name, version.Version, expectedArchitecture),
			))
		}
		for region, architectures := range regionsArchitectureMap {
			if !slices.Contains(architectures, expectedArchitecture) {
				allErrs = append(allErrs, field.Required(
					field.NewPath("spec.providerConfig.machineImages"),
					fmt.Sprintf("machine image version %s@%s for region %q with architecture %q is not defined in the NamespacedCloudProfile providerConfig",
						machineImage.Name, version.Version, region, expectedArchitecture),
				))
			}
		}
	}

	return allErrs
}
