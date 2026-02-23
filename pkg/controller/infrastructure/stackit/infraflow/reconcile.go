package infraflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardenv1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/utils/flow"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	infrainternal "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/internal/infrastructure"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

const (
	defaultTimeout = 90 * time.Second
)

func (fctx *FlowContext) Reconcile(ctx context.Context) error {
	fctx.BasicFlowContext = shared.NewBasicFlowContext().WithSpan().WithLogger(fctx.log).WithPersist(fctx.persistState)
	g := fctx.buildReconcileGraph()
	f := g.Compile()
	if err := f.Run(ctx, flow.Opts{Log: fctx.log}); err != nil {
		fctx.log.Error(err, "flow reconciliation failed")
		return errors.Join(flow.Causes(err), fctx.persistState(ctx))
	}

	state := fctx.computeInfrastructureState()
	status := fctx.computeInfrastructureStatus()
	return infrainternal.PatchProviderStatusAndState(ctx, fctx.client, fctx.infra, status, fctx.nodesCIDR, state)
}

func (fctx *FlowContext) buildReconcileGraph() *flow.Graph {
	g := flow.NewGraph("STACKIT infrastructure reconciliation")

	ensureExternalNetwork := fctx.AddTask(g, "ensure external network",
		fctx.ensureExternalNetwork,
		shared.Timeout(defaultTimeout),
		shared.DoIf(fctx.hasOpenStackCredentials),
	)

	ensureNetwork := fctx.AddTask(g, "ensure isolated network",
		fctx.ensureNetwork,
		shared.Timeout(defaultTimeout),
		shared.Dependencies(ensureExternalNetwork))

	_ = fctx.AddTask(g, "ensure openstack subnet id",
		fctx.ensureOpenStackSubnetID,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureNetwork),
		shared.DoIf(fctx.hasOpenStackCredentials),
	)

	_ = fctx.AddTask(g, "ensure egress IP",
		fctx.ensureEgressIP,
		shared.Dependencies(ensureNetwork),
		shared.Timeout(defaultTimeout))

	ensureSecGroup := fctx.AddTask(g, "ensure security group",
		fctx.ensureSecGroup,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureNetwork))

	_ = fctx.AddTask(g, "ensure security group rules",
		fctx.ensureSecGroupRules,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureSecGroup))

	_ = fctx.AddTask(g, "ensure openstack keypair",
		fctx.ensureOpenStackKeyPair,
		shared.DoIf(fctx.hasOpenStackCredentials),
	)

	_ = fctx.AddTask(g, "ensure stackit ssh key pair",
		fctx.ensureStackitSSHKeyPair,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureNetwork))

	return g
}

func (fctx *FlowContext) ensureExternalNetwork(ctx context.Context) error {
	externalNetwork, err := fctx.networking.GetExternalNetworkByName(ctx, fctx.config.FloatingPoolName)
	if err != nil {
		return err
	}
	if externalNetwork == nil {
		return fmt.Errorf("external network for floating pool name %s not found", fctx.config.FloatingPoolName)
	}
	fctx.state.Set(IdentifierFloatingNetwork, externalNetwork.ID)
	fctx.state.Set(NameFloatingNetwork, externalNetwork.Name)
	return nil
}

func (fctx *FlowContext) ensureConfiguredNetwork(ctx context.Context) error {
	networkID := *fctx.config.Networks.ID
	network, err := fctx.iaasClient.GetNetworkById(ctx, networkID)
	if err != nil {
		fctx.state.Set(IdentifierNetwork, "")
		fctx.state.Set(NameNetwork, "")
		return err
	}
	if network == nil {
		return gardenv1beta1helper.NewErrorWithCodes(
			fmt.Errorf("network with ID '%s' was not found", networkID),
			gardencorev1beta1.ErrorInfraDependencies,
		)
	}

	networkIPv4Config := network.GetIpv4()
	// In IaaS API Network can only have 1 Prefix. However, in OpenStack previously it was possible to have more.
	// We never used this but let's bet sure by checking it here.
	if len(networkIPv4Config.GetPrefixes()) > 1 {
		return fmt.Errorf("multiple prefixes found for network '%s'", networkID)
	}
	if len(networkIPv4Config.GetPrefixes()) == 0 {
		return fmt.Errorf("no prefixes found for network '%s'", networkID)
	}
	workerCIDR := networkIPv4Config.GetPrefixes()[0]

	if fctx.isSNAShoot {
		snaConfig := &infrainternal.SNAConfig{
			NetworkID:   networkID,
			WorkersCIDR: workerCIDR,
		}

		infrainternal.InjectConfig(&fctx.config.Networks, snaConfig)
		fctx.nodesCIDR = &snaConfig.WorkersCIDR
	}

	fctx.state.Set(IdentifierNetwork, networkID)
	fctx.state.Set(NameNetwork, network.GetName())
	return nil
}

func (fctx *FlowContext) ensureOpenStackSubnetID(ctx context.Context) error {
	var networkID string

	if fctx.config.Networks.ID != nil {
		networkID = *fctx.config.Networks.ID
	} else {
		networkID = ptr.Deref(fctx.state.Get(IdentifierNetwork), "")
	}

	osNetwork, err := fctx.access.GetNetworkByID(ctx, networkID)
	if err != nil {
		return gardenv1beta1helper.NewErrorWithCodes(
			fmt.Errorf("network with ID '%s' was not found in openstack", networkID),
			gardencorev1beta1.ErrorInfraDependencies,
		)
	}

	// TODO: A network can have multiple subnets. Check if we can just fetch the first one
	fctx.state.Set(IdentifierSubnet, osNetwork.Subnets[0])
	return nil
}

// NOTE: Only used when using openstack mcm with stackit infra controller
func (fctx *FlowContext) ensureOpenStackKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	keyPair, err := fctx.compute.GetKeyPair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if keyPair != nil {
		// if the public keys are matching then return early. In all other cases we should be creating (or replacing) the keypair with a new one.
		if keyPair.PublicKey == string(fctx.infra.Spec.SSHPublicKey) {
			fctx.state.Set(NameKeyPair, keyPair.Name)
			return nil
		}

		log.Info("replacing SSH key pair")
		if err := fctx.compute.DeleteKeyPair(ctx, fctx.defaultSSHKeypairName()); client.IgnoreNotFoundError(err) != nil {
			return err
		}
		keyPair = nil
		fctx.state.Set(NameKeyPair, "")
	}

	log.Info("creating SSH key pair")
	if keyPair, err = fctx.compute.CreateKeyPair(ctx, fctx.defaultSSHKeypairName(), string(fctx.infra.Spec.SSHPublicKey)); err != nil {
		return err
	}
	fctx.state.Set(NameKeyPair, keyPair.Name)
	return nil
}

func (fctx *FlowContext) ensureStackitSSHKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	keyPair, err := fctx.iaasClient.GetKeypair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if keyPair != nil {
		publicKey := ptr.Deref(keyPair.PublicKey, "")
		// if the public keys are matching then return early. In all other cases we should be creating (or replacing) the keypair with a new one.
		if publicKey != "" && publicKey == string(fctx.infra.Spec.SSHPublicKey) {
			fctx.state.Set(NameKeyPair, *keyPair.Name)
			return nil
		}

		log.Info("replacing stackit SSH key pair")
		if err := fctx.iaasClient.DeleteKeypair(ctx, fctx.defaultSSHKeypairName()); client.IgnoreNotFoundError(err) != nil {
			return err
		}
		keyPair = nil
		fctx.state.Set(NameKeyPair, "")
	}

	log.Info("creating stackit SSH key pair")
	if keyPair, err = fctx.iaasClient.CreateKeypair(ctx, fctx.defaultSSHKeypairName(), string(fctx.infra.Spec.SSHPublicKey)); err != nil {
		return err
	}
	if keyPair == nil {
		return fmt.Errorf("internal error: failed to create key pair")
	}
	fctx.state.Set(NameKeyPair, *keyPair.Name)
	return nil
}

func (fctx *FlowContext) ensureSecGroup(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	payload := iaas.CreateSecurityGroupPayload{
		Name:        ptr.To(fctx.defaultSecurityGroupName()),
		Description: ptr.To("Cluster Nodes"),
	}

	current, err := findExisting(ctx, fctx.state.Get(IdentifierSecGroup), fctx.defaultSecurityGroupName(), fctx.iaasClient.GetSecurityGroupById, fctx.iaasClient.GetSecurityGroupByName)
	if err != nil {
		return err
	}

	if current != nil {
		fctx.state.Set(IdentifierSecGroup, current.GetId())
		fctx.state.Set(NameSecGroup, current.GetName())
		fctx.state.SetObject(ObjectSecGroup, current)
		return nil
	}

	log.Info("creating...", "security group", fctx.defaultSecurityGroupName())
	created, err := fctx.iaasClient.CreateSecurityGroup(ctx, payload)
	if err != nil {
		return err
	}
	// Delete default egress rules
	err = fctx.iaasClient.ReconcileSecurityGroupRules(ctx, log, created, []iaas.SecurityGroupRule{})
	if err != nil {
		return err
	}
	fctx.state.Set(IdentifierSecGroup, created.GetId())
	fctx.state.Set(NameSecGroup, created.GetName())
	fctx.state.SetObject(ObjectSecGroup, created)
	return nil
}

func (fctx *FlowContext) ensureSecGroupRules(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	obj := fctx.state.GetObject(ObjectSecGroup)
	if obj == nil {
		return fmt.Errorf("internal error: security group object not found")
	}
	group, ok := obj.(*iaas.SecurityGroup)
	if !ok {
		return fmt.Errorf("internal error: casting to SecurityGroup failed")
	}

	// usual clusters have all nodes in an internal network, for which NAT prevents access by non-cluster nodes
	// for SNA we need to be more restrictive as other project in the same network area would otherwise gain
	// direct access to the node ports
	nodesCIDR := "0.0.0.0/0"
	if fctx.isSNAShoot {
		nodesCIDR = *fctx.nodesCIDR
	}

	desiredRules := []iaas.SecurityGroupRule{
		{
			Direction:             ptr.To(stackit.DirectionIngress),
			Ethertype:             ptr.To(stackit.EtherTypeIPv4),
			RemoteSecurityGroupId: ptr.To(group.GetId()),
			Description:           ptr.To("IPv4: allow all incoming traffic within the same security group"),
		},
		{
			Direction:   ptr.To(stackit.DirectionEgress),
			Ethertype:   ptr.To(stackit.EtherTypeIPv4),
			Description: ptr.To("IPv4: allow all outgoing traffic"),
		},
		{
			Direction: ptr.To(stackit.DirectionIngress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolTCP),
			PortRange: &iaas.PortRange{
				Max: ptr.To[int64](32767),
				Min: ptr.To[int64](30000),
			},
			IpRange:     ptr.To(nodesCIDR),
			Description: ptr.To("IPv4: allow all incoming tcp traffic with port range 30000-32767"),
		},
		{
			Direction: ptr.To(stackit.DirectionIngress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolUDP),
			PortRange: &iaas.PortRange{
				Max: ptr.To[int64](32767),
				Min: ptr.To[int64](30000),
			},
			IpRange:     ptr.To(nodesCIDR),
			Description: ptr.To("IPv4: allow all incoming udp traffic with port range 30000-32767"),
		},
	}

	if fctx.cluster.Shoot.Spec.Networking != nil && fctx.cluster.Shoot.Spec.Networking.Pods != nil {
		podCIDRRule := iaas.SecurityGroupRule{
			Direction:   ptr.To(stackit.DirectionIngress),
			Ethertype:   ptr.To(stackit.EtherTypeIPv4),
			IpRange:     ptr.To(*fctx.cluster.Shoot.Spec.Networking.Pods),
			Description: ptr.To("IPv4: allow all incoming traffic from cluster pod CIDR"),
		}
		desiredRules = append(desiredRules, podCIDRRule)
	}

	if modified, err := fctx.iaasClient.UpdateSecurityGroupRules(ctx, group, desiredRules, func(rule *iaas.SecurityGroupRule) bool {
		// Do NOT delete unknown rules to keep permissive behavior as with terraform.
		// As we don't store the role ids in the state, this function needs to be adjusted
		// if values in existing rules are changed to identify them for update by replacement.
		return false
	}); err != nil {
		return err
	} else if modified {
		log.Info("updated rules", "security group", group.GetName())
	}

	return nil
}

func (fctx *FlowContext) ensureNetwork(ctx context.Context) error {
	// SNA Case: Network already provided
	if fctx.config.Networks.ID != nil {
		return fctx.ensureConfiguredNetwork(ctx)
	}
	return fctx.ensureIsolatedNetwork(ctx)
}

func (fctx *FlowContext) ensureIsolatedNetwork(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	// Configure DNS settings with the cloud profile as default, while allowing overrides through shoot configuration.
	var dnsServers []string
	dnsServers = fctx.cloudProfileConfig.DNSServers
	if fctx.config.Networks.DNSServers != nil {
		dnsServers = *fctx.config.Networks.DNSServers
	}

	network := iaas.CreateNetworkIPv4{
		CreateNetworkIPv4WithPrefix: &iaas.CreateNetworkIPv4WithPrefix{
			Nameservers: ptr.To(dnsServers),
			Prefix:      ptr.To(fctx.workerCIDR()),
		},
	}

	desired := iaas.CreateIsolatedNetworkPayload{
		Dhcp: ptr.To(true),
		Ipv4: ptr.To(network),
		Name: ptr.To(fctx.technicalID),
	}
	current, err := findExisting(ctx, fctx.state.Get(IdentifierNetwork), fctx.defaultNetworkName(), fctx.iaasClient.GetNetworkById, fctx.iaasClient.GetNetworkByName)
	if err != nil {
		return err
	}
	if current != nil {
		fctx.state.Set(IdentifierNetwork, current.GetId())
		fctx.state.Set(NameNetwork, current.GetName())
		if _, err := fctx.iaasClient.UpdateNetwork(ctx, current.GetId(), client.IsolatedNetworkToPartialUpdate(desired)); err != nil {
			return err
		}
		// Update dnsNameservers when update was successful
		fctx.dnsNameservers = ptr.To(desired.Ipv4.CreateNetworkIPv4WithPrefix.GetNameservers())
	} else {
		log.Info("creating...", "network", fctx.defaultNetworkName())
		created, err := fctx.iaasClient.CreateIsolatedNetwork(ctx, desired)
		if err != nil {
			return err
		}
		fctx.state.Set(IdentifierNetwork, created.GetId())
		fctx.state.Set(NameNetwork, created.GetName())
		fctx.dnsNameservers = ptr.To(created.Ipv4.GetNameservers())
	}
	return nil
}

func (fctx *FlowContext) ensureEgressIP(ctx context.Context) error {
	var result []string
	networkID := fctx.state.Get(IdentifierNetwork)
	network, err := fctx.iaasClient.GetNetworkById(ctx, *networkID)
	if err != nil {
		return err
	}
	routerIP, ok := network.Ipv4.GetPublicIpOk()
	if ok {
		result = append(result, routerIP)
		fctx.state.SetObject(IdentifierEgressCIDRs, result)
		return nil
	}
	return fmt.Errorf("egress IP not found for network %s", network.GetId())
}
