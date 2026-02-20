// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/stackit/infraflow"
	openstackutils "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack"
	openstackclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
)

// Delete the Infrastructure config.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	return util.DetermineError(
		a.delete(ctx, log, infra, cluster),
		helper.KnownCodes,
	)
}

// ForceDelete forcefully deletes the Infrastructure.
func (a *actuator) ForceDelete(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.Infrastructure, _ *extensionscontroller.Cluster) error {
	return nil
}

// delete deletes the infrastructure resource using the flow reconciler.
func (a *actuator) delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	var clientFactory openstackclient.Factory
	var useOpenStackClient bool
	infraState, err := infrastructureStateFromRaw(infra)
	if err != nil {
		return err
	}

	region := stackit.DetermineRegion(cluster)
	iaasClient, err := stackitclient.New(region, cluster).IaaS(ctx, a.client, infra.Spec.SecretRef)
	if err != nil {
		return err
	}

	stackitLBClient, err := stackitclient.New(region, cluster).LoadBalancing(ctx, a.client, infra.Spec.SecretRef)
	if err != nil {
		return err
	}

	// Try to retrieve OpenStack credentials from cloudprovider secret, if they are not available then that's also fine.
	// This is only for the migration mode where we still need to use both, since for example we want to use STACKIT infra
	// controller together with the old openstack mcm (at least temporarily).

	// Mainly the OS Client fetches the Subnet, External Network and creates the SSH Keypair for the MCM to work properly.
	if credentials, _ := openstackutils.GetCredentials(ctx, a.client, infra.Spec.SecretRef, false); credentials != nil {
		clientFactory, err = openstackclient.NewOpenstackClientFromCredentials(ctx, credentials)
		if err != nil {
			return err
		}
		useOpenStackClient = true
	}

	fctx, err := infraflow.NewFlowContext(ctx, infraflow.Opts{
		Log:                log,
		Infrastructure:     infra,
		State:              infraState,
		Cluster:            cluster,
		ClientFactory:      clientFactory,
		UseOpenStackClient: useOpenStackClient,
		Client:             a.client,
		IaaSClient:         iaasClient,
		StackitLB:          stackitLBClient,
		CustomLabelDomain:  a.customLabelDomain,
	})
	if err != nil {
		return fmt.Errorf("failed to create flow context: %w", err)
	}

	return fctx.Delete(ctx)
}
