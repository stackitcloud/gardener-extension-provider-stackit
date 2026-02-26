// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenerapihelper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow"
	openstackutils "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
	openstackclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

// Delete the Infrastructure config.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	err := a.delete(ctx, log, infra, cluster)
	if stackitclient.IsConflict(err) {
		return gardenerapihelper.NewErrorWithCodes(err, gardencorev1beta1.ErrorInfraDependencies)
	}

	return util.DetermineError(
		err,
		helper.KnownCodes,
	)
}

// ForceDelete forcefully deletes the Infrastructure.
func (a *actuator) ForceDelete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Infrastructure, _ *extensionscontroller.Cluster) error {
	return nil
}

// delete deletes the infrastructure resource using the flow reconciler.
func (a *actuator) delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	infraState, err := infrastructureStateFromRaw(infra)
	if err != nil {
		return err
	}

	credentials, err := openstackutils.GetCredentials(ctx, a.client, infra.Spec.SecretRef, false)
	if err != nil {
		return fmt.Errorf("could not get Openstack credentials: %w", err)
	}
	clientFactory, err := openstackclient.NewOpenstackClientFromCredentials(ctx, credentials)
	if err != nil {
		return err
	}

	region := stackit.DetermineRegion(cluster)
	stackitLBClient, err := stackitclient.New(region, cluster).LoadBalancing(ctx, a.client, infra.Spec.SecretRef)
	if err != nil {
		return err
	}

	iaasClient, err := stackitclient.New(region, cluster).IaaS(ctx, a.client, infra.Spec.SecretRef)
	if err != nil {
		return err
	}

	fctx, err := infraflow.NewFlowContext(ctx, infraflow.Opts{
		Log:            log,
		Infrastructure: infra,
		State:          infraState,
		Cluster:        cluster,
		ClientFactory:  clientFactory,
		Client:         a.client,
		StackitLB:      stackitLBClient,
		IaaSClient:     iaasClient,
	})
	if err != nil {
		return fmt.Errorf("failed to create flow context: %w", err)
	}
	return fctx.Delete(ctx)
}
