// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

func (w *workerDelegate) UpdateMachineImagesStatus(ctx context.Context) error {
	if w.machineImages == nil {
		if err := w.generateMachineConfig(ctx); err != nil {
			return fmt.Errorf("unable to generate the machine config: %w", err)
		}
	}

	// Decode the current worker provider status.
	workerStatus, err := w.decodeWorkerProviderStatus()
	if err != nil {
		return fmt.Errorf("unable to decode the worker provider status: %w", err)
	}

	workerStatus.MachineImages = w.machineImages
	if err := w.updateWorkerProviderStatus(ctx, workerStatus); err != nil {
		return fmt.Errorf("unable to update worker provider status: %w", err)
	}

	return nil
}

func (w *workerDelegate) selectMachineImageForWorkerPool(name, version, region string, arch string, machineCapabilities gardencorev1beta1.Capabilities, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) (*stackitv1alpha1.MachineImage, error) {
	selectedMachineImage := &stackitv1alpha1.MachineImage{
		Name:    name,
		Version: version,
	}

	if capabilitySet, err := helper.FindImageInCloudProfile(w.cloudProfileConfig, name, version, region, machineCapabilities, capabilityDefinitions); err == nil {
		selectedMachineImage.Capabilities = capabilitySet.Capabilities
		// ID takes precedence over Image attribute. Reminder: Image is global name while ID is region specific.
		if len(capabilitySet.Regions) > 0 && capabilitySet.Regions[0].ID != "" {
			selectedMachineImage.ID = capabilitySet.Regions[0].ID
		} else {
			selectedMachineImage.Image = capabilitySet.Image
		}

		return selectedMachineImage, nil
	}

	// Try to look up machine image in worker provider status as it was not found in componentconfig.
	if providerStatus := w.worker.Status.ProviderStatus; providerStatus != nil {
		workerStatus := &stackitv1alpha1.WorkerStatus{}
		if _, _, err := w.decoder.Decode(providerStatus.Raw, nil, workerStatus); err != nil {
			return nil, fmt.Errorf("could not decode worker status of worker '%s': %w", k8sclient.ObjectKeyFromObject(w.worker), err)
		}

		// Pass the original (non-normalized) MachineCapabilities so FindImageInWorkerStatus
		// can distinguish legacy format (Architecture field) from capability format (Capabilities field).
		return helper.FindImageInWorkerStatus(workerStatus.MachineImages, name, version, arch, machineCapabilities, w.cluster.CloudProfile.Spec.MachineCapabilities)
	}

	return nil, worker.ErrorMachineImageNotFound(name, version, arch, region)
}

func appendMachineImage(machineImages []stackitv1alpha1.MachineImage, machineImage stackitv1alpha1.MachineImage, capabilityDefinitions []gardencorev1beta1.CapabilityDefinition) []stackitv1alpha1.MachineImage {
	// support for cloudprofile machine images without capabilities
	if len(capabilityDefinitions) == 0 {
		// Extract architecture from Capabilities if Architecture is not set
		// (happens when image came from capability-based lookup with normalized definitions)
		architecture := machineImage.Architecture
		if architecture == nil {
			if archValues, ok := machineImage.Capabilities[v1beta1constants.ArchitectureName]; ok && len(archValues) > 0 {
				architecture = &archValues[0]
			}
		}
		for _, image := range machineImages {
			if image.Name == machineImage.Name && image.Version == machineImage.Version && ptr.Deref(architecture, "") == ptr.Deref(image.Architecture, "") {
				// If the image already exists without capabilities, we can just return the existing list.
				return machineImages
			}
		}
		return append(machineImages, stackitv1alpha1.MachineImage{
			Name:         machineImage.Name,
			Version:      machineImage.Version,
			Image:        machineImage.Image,
			ID:           machineImage.ID,
			Architecture: architecture,
		})
	}

	defaultedCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(machineImage.Capabilities, capabilityDefinitions)

	for _, existingMachineImage := range machineImages {
		existingDefaultedCapabilities := gardencorev1beta1.GetCapabilitiesWithAppliedDefaults(existingMachineImage.Capabilities, capabilityDefinitions)
		if existingMachineImage.Name == machineImage.Name && existingMachineImage.Version == machineImage.Version && gardencorev1beta1helper.AreCapabilitiesEqual(defaultedCapabilities, existingDefaultedCapabilities) {
			// If the image already exists with the same capabilities return the existing list.
			return machineImages
		}
	}

	// If the image does not exist, we create a new machine image entry with the capabilities.
	machineImages = append(machineImages, stackitv1alpha1.MachineImage{
		Name:         machineImage.Name,
		Version:      machineImage.Version,
		Image:        machineImage.Image,
		ID:           machineImage.ID,
		Capabilities: machineImage.Capabilities,
	})

	return machineImages
}
