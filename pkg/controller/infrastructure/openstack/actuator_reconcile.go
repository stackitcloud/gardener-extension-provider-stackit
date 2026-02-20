// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/openstack/infraflow"
	openstackutils "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack"
	openstackclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
)

// Reconcile the Infrastructure config.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *controller.Cluster) error {
	return util.DetermineError(
		a.reconcile(ctx, log, infra, cluster),
		helper.KnownCodes,
	)
}

// reconcile reconciles the infrastructure and updates the Infrastructure status (state of the world), the state (input for the next loops) or reports any errors that occurred.
func (a *actuator) reconcile(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *controller.Cluster) error {
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
		IaaSClient:     iaasClient,
	})
	if err != nil {
		return fmt.Errorf("failed to create flow context: %w", err)
	}

	return fctx.Reconcile(ctx)
}
