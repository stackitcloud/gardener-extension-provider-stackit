// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infrastructure

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/infrastructure"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	openstackclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
)

// configValidator implements ConfigValidator for stackit infrastructure resources.
type configValidator struct {
	stackit   infrastructure.ConfigValidator
	openstack infrastructure.ConfigValidator
	client    client.Client
}

// NewConfigValidator creates a new ConfigValidator.
func NewConfigValidator(mgr manager.Manager, logger logr.Logger) infrastructure.ConfigValidator {
	return &configValidator{
		stackit:   stackit.NewConfigValidator(mgr, logger),
		openstack: openstack.NewConfigValidator(mgr, openstackclient.FactoryFactoryFunc(openstackclient.NewOpenstackClientFromCredentials), logger),
		client:    mgr.GetClient(),
	}
}

// Validate validates the provider config of the given infrastructure resource with the cloud provider.
func (c *configValidator) Validate(ctx context.Context, infra *extensionsv1alpha1.Infrastructure) field.ErrorList {
	cluster, err := controller.GetCluster(ctx, c.client, infra.Namespace)
	if err != nil {
		return append(field.ErrorList{}, field.InternalError(nil, err))
	}

	if feature.UseStackitAPIInfrastructureController(cluster) {
		return c.stackit.Validate(ctx, infra)
	}

	return c.openstack.Validate(ctx, infra)
}
