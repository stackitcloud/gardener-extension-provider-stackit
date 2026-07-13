// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator

import (
	"context"
	"fmt"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitvalidation "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

// NewShootValidator returns a new instance of a shoot validator.
func NewShootValidator(mgr manager.Manager) extensionswebhook.Validator {
	return &shoot{}
}

type shoot struct{}

// Validate validates the given shoot objects.
func (s *shoot) Validate(_ context.Context, newObj, oldObj client.Object) error {
	shoot, ok := newObj.(*core.Shoot)
	if !ok {
		return fmt.Errorf("wrong object type %T", newObj)
	}

	if shoot.Spec.Provider.ControlPlaneConfig == nil {
		return nil
	}

	cpConfig, err := helper.ControlPlaneConfigFromRawExtension(shoot.Spec.Provider.ControlPlaneConfig)
	if err != nil {
		return err
	}

	infraConfig, err := helper.InfrastructureConfigFromRawExtension(shoot.Spec.Provider.InfrastructureConfig)
	if err != nil {
		return err
	}

	allErrs := field.ErrorList{}

	allErrs = append(allErrs, stackitvalidation.ValidateControlPlaneConfig(cpConfig, shoot.Spec.Kubernetes.Version, field.NewPath("spec").Child("provider").Child("controlPlaneConfig"))...)

	allErrs = append(allErrs, stackitvalidation.ValidateInfrastructureConfig(infraConfig, shoot.Spec.Networking.Nodes, field.NewPath("spec").Child("provider").Child("infrastructureConfig"))...)

	if oldObj != nil {
		oldShoot, ok := oldObj.(*core.Shoot)
		if !ok {
			return fmt.Errorf("wrong object type %T for old object", oldObj)
		}
		oldInfraConfig, err := helper.InfrastructureConfigFromRawExtension(oldShoot.Spec.Provider.InfrastructureConfig)
		if err != nil {
			return err
		}
		allErrs = append(allErrs, stackitvalidation.ValidateInfrastructureConfigUpdate(oldInfraConfig, infraConfig, field.NewPath("spec").Child("provider").Child("infrastructureConfig"))...)
	}

	return allErrs.ToAggregate()
}
