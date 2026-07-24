// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package mutator

import (
	"context"
	"fmt"
	"slices"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
)

// NewCloudProfileMutator returns a new instance of a CloudProfile mutator.
func NewCloudProfileMutator(mgr manager.Manager) extensionswebhook.Mutator {
	return &cloudProfile{
		client:  mgr.GetClient(),
		decoder: serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
	}
}

type cloudProfile struct {
	client  client.Client
	decoder runtime.Decoder
}

// Mutate mutates the given CloudProfile object.
func (p *cloudProfile) Mutate(_ context.Context, newObj, _ client.Object) error {
	profile, ok := newObj.(*gardencorev1beta1.CloudProfile)
	if !ok {
		return fmt.Errorf("wrong object type %T", newObj)
	}

	if profile.DeletionTimestamp != nil || profile.Spec.ProviderConfig == nil || len(profile.Spec.MachineCapabilities) == 0 {
		return nil
	}

	specConfig := &stackitv1alpha1.CloudProfileConfig{}
	if _, _, err := p.decoder.Decode(profile.Spec.ProviderConfig.Raw, nil, specConfig); err != nil {
		return fmt.Errorf("could not decode providerConfig of cloudProfile for %q: %w", profile.Name, err)
	}

	overwriteMachineImageCapabilityFlavors(profile, specConfig)
	return nil
}

func overwriteMachineImageCapabilityFlavors(profile *gardencorev1beta1.CloudProfile, config *stackitv1alpha1.CloudProfileConfig) {
	for _, providerMachineImage := range config.MachineImages {
		imageIdx := slices.IndexFunc(profile.Spec.MachineImages, func(mi gardencorev1beta1.MachineImage) bool {
			return mi.Name == providerMachineImage.Name
		})
		if imageIdx == -1 {
			continue
		}

		for _, providerVersion := range providerMachineImage.Versions {
			versionIdx := slices.IndexFunc(profile.Spec.MachineImages[imageIdx].Versions, func(miv gardencorev1beta1.MachineImageVersion) bool {
				return miv.Version == providerVersion.Version
			})
			if versionIdx == -1 {
				continue
			}

			profile.Spec.MachineImages[imageIdx].Versions[versionIdx].CapabilityFlavors = convertCapabilityFlavors(providerVersion.CapabilityFlavors)
		}
	}
}

func convertCapabilityFlavors(providerFlavors []stackitv1alpha1.MachineImageFlavor) []gardencorev1beta1.MachineImageFlavor {
	capabilityFlavors := make([]gardencorev1beta1.MachineImageFlavor, 0, len(providerFlavors))
	for _, providerFlavor := range providerFlavors {
		capabilityFlavors = append(capabilityFlavors, gardencorev1beta1.MachineImageFlavor{
			Capabilities: providerFlavor.GetCapabilities(),
		})
	}
	return capabilityFlavors
}
