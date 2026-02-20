// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
)

// Migrate deletes the k8s infrastructure resources without deleting the corresponding resources in the IaaS provider.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, _ *controller.Cluster) error {
	return nil
}
