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

// Migrate deletes resources in the seed cluster related to the Infrastructure object without deleting the corresponding
// IaaS resources in the cloud provider. As this Infrastructure controller does not create any Kubernetes objects in the
// seed cluster, there is nothing to do when preparing the control plane migration. The finalizer is removed from the
// Infrastructure object so the Delete method is not called and the IaaS resources are simply adopted by the Restore
// method invoked on the destination seed cluster.
func (a *actuator) Migrate(context.Context, logr.Logger, *extensionsv1alpha1.Infrastructure, *controller.Cluster) error {
	return nil
}
