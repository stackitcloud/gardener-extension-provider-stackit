package infraflow

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/utils/flow"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

func (fctx *FlowContext) Delete(ctx context.Context) error {
	fctx.BasicFlowContext = shared.NewBasicFlowContext().WithSpan().WithLogger(fctx.log).WithPersist(fctx.persistState)
	g := fctx.buildDeleteGraph()
	f := g.Compile()

	if err := f.Run(ctx, flow.Opts{Log: fctx.log}); err != nil {
		return flow.Causes(err)
	}
	return nil
}

func (fctx *FlowContext) buildDeleteGraph() *flow.Graph {
	g := flow.NewGraph("STACKIT infrastructure deletion")

	needToDeleteNetwork := fctx.config.Networks.ID == nil && !fctx.isSNAShoot

	recoverNetwork := fctx.AddTask(g, "recover network ID",
		fctx.recoverNetworkID, shared.Timeout(defaultTimeout))

	_ = fctx.AddTask(g, "ensure deletion network",
		fctx.deleteIsolatedNetwork,
		shared.Timeout(defaultTimeout),
		shared.Dependencies(recoverNetwork),
		shared.DoIf(needToDeleteNetwork),
	)

	_ = fctx.AddTask(g, "ensure deletion security group",
		fctx.deleteSecGroup,
		shared.Timeout(defaultTimeout),
	)

	_ = fctx.AddTask(g, "delete OpenStack KeyPair",
		fctx.deleteOpenStackKeyPair,
		shared.Timeout(defaultTimeout), shared.DoIf(fctx.hasOpenStackCredentials))

	_ = fctx.AddTask(g, "ensure deletion SSH key pair",
		fctx.deleteStackitSSHKeyPair,
		shared.Timeout(defaultTimeout),
	)

	_ = fctx.AddTask(g, "ensure STACKIT LB deletion",
		fctx.ensureStackitLoadBalancerDeletion,
		shared.Timeout(defaultTimeout),
		shared.DoIf(feature.Gate.Enabled(feature.EnsureSTACKITLBDeletion)),
	)

	return g
}

func (fctx *FlowContext) ensureStackitLoadBalancerDeletion(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	lb, err := fctx.stackitLB.ListLoadBalancers(ctx)
	if err != nil {
		return err
	}
	for i := range lb {
		// Filter out all other LB's that are in the project but do not long belong to this shoot
		// TODO: use utils.BuildLabelKey
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

// recoverNetworkID fixes potential issues by recovering the InfrastructureState
func (fctx *FlowContext) recoverNetworkID(ctx context.Context) error {
	// If user provided a network, never delete it. Therefore, there is also nothing to recover.
	if fctx.config.Networks.ID != nil {
		return nil
	}
	networkID := fctx.state.Get(IdentifierNetwork)
	if networkID != nil {
		return nil
	}
	network, err := findExisting(ctx, fctx.state.Get(IdentifierNetwork), fctx.defaultNetworkName(), fctx.iaasClient.GetNetworkById, fctx.iaasClient.GetNetworkByName)
	if err != nil {
		return err
	}
	if network != nil {
		fctx.state.Set(IdentifierNetwork, network.GetId())
		return nil
	}
	return nil
}

func (fctx *FlowContext) deleteIsolatedNetwork(ctx context.Context) error {
	networkID := fctx.state.Get(IdentifierNetwork)
	if networkID == nil {
		return nil
	}

	if err := fctx.iaasClient.DeleteNetwork(ctx, *networkID); stackitclient.IgnoreNotFoundError(err) != nil {
		if stackitclient.IsConflict(err) {
			return fmt.Errorf("failed to delete network r due to 409 conflict: %w", err)
		}
		return fmt.Errorf("failed to delete network: %w", err)
	}
	fctx.state.Set(NameNetwork, "")
	fctx.state.Set(IdentifierNetwork, "")
	return nil
}

func (fctx *FlowContext) deleteSecGroup(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	current, err := findExisting(ctx, fctx.state.Get(IdentifierSecGroup), fctx.defaultSecurityGroupName(), fctx.iaasClient.GetSecurityGroupById, fctx.iaasClient.GetSecurityGroupByName)
	if err != nil {
		return err
	}
	if current != nil {
		log.Info("deleting...", "securityGroup", current.GetId())
		if err := fctx.iaasClient.DeleteSecurityGroup(ctx, current.GetId()); stackitclient.IgnoreNotFoundError(err) != nil {
			if stackitclient.IsConflict(err) {
				return fmt.Errorf("failed to delete security group r due to 409 conflict: %w", err)
			}
			return fmt.Errorf("failed to delete security group: %w", err)
		}
	}
	fctx.state.Set(NameSecGroup, "")
	fctx.state.SetObject(ObjectSecGroup, nil)
	return nil
}

func (fctx *FlowContext) deleteOpenStackKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)
	current, err := fctx.compute.GetKeyPair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if current != nil {
		log.Info("deleting ssh keypair...")
		if err := fctx.compute.DeleteKeyPair(ctx, current.Name); client.IgnoreNotFoundError(err) != nil {
			return err
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
			return err
		}
	}
	return nil
}
