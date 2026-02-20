// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller/infrastructure"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// configValidator implements ConfigValidator for stackit infrastructure resources.
type configValidator struct {
	client client.Client
	logger logr.Logger
}

// NewConfigValidator creates a new ConfigValidator.
func NewConfigValidator(mgr manager.Manager, logger logr.Logger) infrastructure.ConfigValidator {
	return &configValidator{
		client: mgr.GetClient(),
		logger: logger.WithName("stackit-infrastructure-config-validator"),
	}
}

// Validate validates the provider config of the given infrastructure resource with the cloud provider.
func (c *configValidator) Validate(ctx context.Context, infra *extensionsv1alpha1.Infrastructure) field.ErrorList {
	allErrs := field.ErrorList{}
	// TODO not impemented
	return allErrs
}
