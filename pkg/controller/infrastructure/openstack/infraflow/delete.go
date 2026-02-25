// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infraflow

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/util"
	"github.com/gardener/gardener/pkg/utils/flow"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/internal/infrastructure"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
)

// Delete creates and runs the flow to delete the AWS infrastructure.
func (fctx *FlowContext) Delete(ctx context.Context) error {
	if fctx.state.IsEmpty() {
		// nothing to do, e.g. if cluster was created with wrong credentials
		return nil
	}

	fctx.BasicFlowContext = shared.NewBasicFlowContext().WithSpan().WithLogger(fctx.log).WithPersist(fctx.persistState)

	g := fctx.buildDeleteGraph()
	f := g.Compile()
	if err := f.Run(ctx, flow.Opts{Log: fctx.log}); err != nil {
		return flow.Causes(err)
	}
	return nil
}

func (fctx *FlowContext) buildDeleteGraph() *flow.Graph {
	g := flow.NewGraph("Openstack infrastructure destruction")

	needToDeleteNetwork := fctx.config.Networks.ID == nil && !fctx.isSNAShoot
	needToDeleteSubnet := fctx.config.Networks.SubnetID == nil && !fctx.isSNAShoot
	needToDeleteRouter := fctx.config.Networks.Router == nil && !fctx.isSNAShoot

	_ = fctx.AddTask(g, "delete ssh key pair",
		fctx.deleteSSHKeyPair,
		shared.Timeout(defaultTimeout))

	_ = fctx.AddTask(g, "ensure STACKIT LB deletion",
		fctx.ensureSTACKITLBDeletion,
		shared.Timeout(defaultTimeout),
		shared.DoIf(feature.Gate.Enabled(feature.EnsureSTACKITLBDeletion)),
	)

	// NOTE(Felix Breuer, 12.1.26):
	// the deletion task is currently only active in the feature gate due to a race condition of deleting the service accounts when a stackit project is deleted
	// if a shoot has the stackit MCM feature enabled, this also deletes the ssh keypair
	// TODO: remove this feature gate to always delete ssh key pairs after race condition is fixed
	_ = fctx.AddTask(g, "delete stackit ssh key pair",
		fctx.deleteStackitSSHKeyPair,
		shared.Timeout(defaultTimeout),
		shared.DoIf(fctx.hasStackitMCM))

	_ = fctx.AddTask(g, "delete security group",
		fctx.deleteSecGroup,
		shared.Timeout(defaultTimeout))
	recoverRouterID := fctx.AddTask(g, "recover router ID",
		fctx.recoverRouterID,
		shared.Timeout(defaultTimeout), shared.DoIf(!fctx.isSNAShoot))
	recoverNetworkID := fctx.AddTask(g, "recover network ID",
		fctx.recoverNetworkID,
		shared.Timeout(defaultTimeout), shared.DoIf(!fctx.isSNAShoot))
	recoverSubnetID := fctx.AddTask(g, "recover subnet ID",
		fctx.recoverSubnetID,
		shared.Timeout(defaultTimeout), shared.Dependencies(recoverNetworkID), shared.DoIf(!fctx.isSNAShoot))

	recoverIDs := flow.NewTaskIDs(recoverNetworkID, recoverRouterID, recoverSubnetID)
	k8sRoutes := fctx.AddTask(g, "delete kubernetes routes",
		func(ctx context.Context) error {
			routerID := fctx.state.Get(IdentifierRouter)
			if routerID == nil {
				return nil
			}
			return infrastructure.CleanupKubernetesRoutes(ctx, fctx.networking, *routerID, infrastructure.WorkersCIDR(fctx.config))
		},
		shared.Timeout(defaultTimeout),
		shared.Dependencies(recoverIDs),
		shared.DoIf(!fctx.isSNAShoot),
	)

	deleteRouterInterface := fctx.AddTask(g, "delete router interface",
		fctx.deleteRouterInterface,
		shared.DoIf(needToDeleteSubnet || needToDeleteRouter),
		shared.Timeout(defaultTimeout), shared.Dependencies(recoverIDs, k8sRoutes))
	// subnet deletion only needed if network is given by spec
	deleteSubnetTask := fctx.AddTask(g, "delete subnet",
		fctx.deleteSubnet,
		shared.DoIf(needToDeleteSubnet), shared.Timeout(defaultTimeout), shared.Dependencies(deleteRouterInterface))
	_ = fctx.AddTask(g, "delete network",
		fctx.deleteNetwork,
		shared.DoIf(needToDeleteNetwork), shared.Timeout(defaultTimeout), shared.Dependencies(deleteRouterInterface, deleteSubnetTask))
	_ = fctx.AddTask(g, "delete router",
		fctx.deleteRouter,
		shared.DoIf(needToDeleteRouter), shared.Timeout(defaultTimeout), shared.Dependencies(deleteRouterInterface))
	_ = fctx.AddTask(g, "cleanup marker",
		func(_ context.Context) error {
			fctx.state.Set(CreatedResourcesExistKey, "")
			return nil
		})

	return g
}

func (fctx *FlowContext) deleteRouter(ctx context.Context) error {
	routerID := fctx.state.Get(IdentifierRouter)
	if routerID == nil {
		return nil
	}

	shared.LogFromContext(ctx).Info("deleting...", "router", *routerID)
	if err := fctx.networking.DeleteRouter(ctx, *routerID); client.IgnoreNotFoundError(err) != nil {
		return util.DetermineError(fmt.Errorf("failed to delete router: %w", err), helper.KnownCodes)
	}

	fctx.state.Set(IdentifierRouter, "")
	return nil
}

func (fctx *FlowContext) deleteNetwork(ctx context.Context) error {
	networkID := fctx.state.Get(IdentifierNetwork)
	if networkID == nil {
		return nil
	}

	shared.LogFromContext(ctx).Info("deleting...", "network", *networkID)
	if err := fctx.networking.DeleteNetwork(ctx, *networkID); client.IgnoreNotFoundError(err) != nil {
		return util.DetermineError(fmt.Errorf("failed to delete network: %w", err), helper.KnownCodes)
	}

	fctx.state.Set(NameNetwork, "")
	fctx.state.Set(IdentifierNetwork, "")
	return nil
}

func (fctx *FlowContext) deleteSubnet(ctx context.Context) error {
	subnetID := fctx.state.Get(IdentifierSubnet)
	if subnetID == nil {
		return nil
	}

	shared.LogFromContext(ctx).Info("deleting...", "subnet", *subnetID)
	if err := fctx.networking.DeleteSubnet(ctx, *subnetID); client.IgnoreNotFoundError(err) != nil {
		return fmt.Errorf("failed to delete subnet: %w", err)
	}
	fctx.state.Set(IdentifierSubnet, "")
	return nil
}

func (fctx *FlowContext) recoverRouterID(ctx context.Context) error {
	if fctx.config.Networks.Router != nil {
		fctx.state.Set(IdentifierRouter, fctx.config.Networks.Router.ID)
		return nil
	}
	routerID := fctx.state.Get(IdentifierRouter)
	if routerID != nil {
		return nil
	}
	router, err := fctx.findExistingRouter(ctx)
	if err != nil {
		return err
	}
	if router != nil {
		fctx.state.Set(IdentifierRouter, router.ID)
	}
	return nil
}

func (fctx *FlowContext) recoverNetworkID(ctx context.Context) error {
	_, err := fctx.getNetworkID(ctx)
	return err
}

func (fctx *FlowContext) recoverSubnetID(ctx context.Context) error {
	if fctx.state.Get(IdentifierSubnet) != nil {
		return nil
	}

	subnet, err := fctx.findExistingSubnet(ctx)
	if err != nil {
		return err
	}
	if subnet != nil {
		fctx.state.Set(IdentifierSubnet, subnet.ID)
	}
	return nil
}

func (fctx *FlowContext) deleteRouterInterface(ctx context.Context) error {
	routerID := fctx.state.Get(IdentifierRouter)
	if routerID == nil {
		return nil
	}
	subnetID := fctx.state.Get(IdentifierSubnet)
	if subnetID == nil {
		return nil
	}

	portID, err := fctx.access.GetRouterInterfacePortID(ctx, *routerID, *subnetID)
	if err != nil {
		return err
	}
	if portID == nil {
		return nil
	}

	log := shared.LogFromContext(ctx)
	log.Info("deleting...")
	err = fctx.access.RemoveRouterInterfaceAndWait(ctx, *routerID, *subnetID, *portID)
	if err != nil {
		return err
	}
	return nil
}

func (fctx *FlowContext) deleteSecGroup(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	current, err := findExisting(ctx, fctx.state.Get(IdentifierSecGroup), fctx.defaultSecurityGroupName(), fctx.access.GetSecurityGroupByID, fctx.access.GetSecurityGroupByName)
	if err != nil {
		return err
	}
	if current != nil {
		log.Info("deleting...", "securityGroup", current.ID)
		if err := fctx.networking.DeleteSecurityGroup(ctx, current.ID); client.IgnoreNotFoundError(err) != nil {
			return util.DetermineError(fmt.Errorf("failed to delete security groups: %w", err), helper.KnownCodes)
		}
	}
	fctx.state.Set(NameSecGroup, "")
	fctx.state.SetObject(ObjectSecGroup, nil)
	return nil
}

func (fctx *FlowContext) ensureSTACKITLBDeletion(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	lb, err := fctx.stackitLB.ListLoadBalancers(ctx)
	if err != nil {
		return err
	}
	for i := range lb {
		// Filter out all other LB's that are in the project but do not long belong to this shoot
		// TODO: migrate to utils.BuildLabelKey
		if val, ok := lb[i].GetLabels()[controlplane.STACKITLBClusterLabelKey]; ok && val == fctx.technicalID {
			log.Info("deleting...", "load balancer", lb[i].GetName())
			err = fctx.stackitLB.DeleteLoadBalancer(ctx, lb[i].GetName())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (fctx *FlowContext) deleteSSHKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	current, err := fctx.compute.GetKeyPair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if current != nil {
		log.Info("deleting ssh keypair...")
		if err := fctx.compute.DeleteKeyPair(ctx, current.Name); client.IgnoreNotFoundError(err) != nil {
			return util.DetermineError(fmt.Errorf("failed to delete SSH key pair: %w", err), helper.KnownCodes)
		}
	}
	return nil
}

func (fctx *FlowContext) deleteStackitSSHKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	current, err := fctx.iaasClient.GetKeypair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if current != nil {
		log.Info("deleting stackit ssh keypair...")
		if err := fctx.iaasClient.DeleteKeypair(ctx, *current.Name); client.IgnoreNotFoundError(err) != nil {
			return fmt.Errorf("failed to delete STACKIT SSH key pair: %w", err)
		}
	}
	return nil
}
