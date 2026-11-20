// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infraflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	gardenv1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/utils/flow"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/access"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	infrainternal "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/internal/infrastructure"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/utils"
)

const (
	defaultTimeout     = 90 * time.Second
	defaultLongTimeout = 3 * time.Minute
)

// Reconcile creates and runs the flow to reconcile the AWS infrastructure.
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
	var egressCIDRs []string
	if status != nil {
		egressCIDRs = utils.ComputeEgressCIDRs(status.Networks.Router.ExternalFixedIPs)
	}

	return infrainternal.PatchProviderStatusAndState(ctx, fctx.client, fctx.infra, status, fctx.nodesCIDR, egressCIDRs, state)
}

func (fctx *FlowContext) buildReconcileGraph() *flow.Graph {
	g := flow.NewGraph("Openstack infrastructure reconciliation")

	prehook := fctx.AddTask(g, "pre-reconcile hook", func(_ context.Context) error {
		// delete unnecessary state object. RouterIP was replaced by IdentifierEgressCIDRs to handle cases where the router had multiple externalFixedIPs attached to it.
		fctx.state.Delete(RouterIP)
		return nil
	})

	ensureSNAState := fctx.AddTask(g, "ensure SNA state",
		fctx.ensureSNAState,
		shared.Timeout(defaultTimeout),
		shared.DoIf(fctx.isSNAShoot))

	ensureExternalNetwork := fctx.AddTask(g, "ensure external network",
		fctx.ensureExternalNetwork,
		shared.Timeout(defaultTimeout), shared.Dependencies(prehook, ensureSNAState))

	ensureRouter := fctx.AddTask(g, "ensure router",
		fctx.ensureRouter,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureExternalNetwork))

	routerReady := fctx.AddTask(g, "wait until router is ready",
		flow.TaskFn(fctx.ensureRouterReady).RetryUntilTimeout(5*time.Second, defaultTimeout),
		shared.Dependencies(ensureRouter))

	ensureNetwork := fctx.AddTask(g, "ensure network",
		fctx.ensureNetwork,
		shared.Timeout(defaultTimeout), shared.Dependencies(prehook, ensureSNAState))

	networkReady := fctx.AddTask(g, "wait until network is ready",
		flow.TaskFn(fctx.ensureNetworkReady).RetryUntilTimeout(5*time.Second, defaultTimeout),
		shared.Dependencies(ensureNetwork))

	ensureSubnet := fctx.AddTask(g, "ensure subnet",
		fctx.ensureSubnet,
		shared.Timeout(defaultTimeout), shared.Dependencies(networkReady))

	subnetReady := fctx.AddTask(g, "wait until subnet is ready",
		flow.TaskFn(fctx.ensureSubnetReady).RetryUntilTimeout(5*time.Second, defaultTimeout),
		shared.Dependencies(ensureSubnet))

	_ = fctx.AddTask(g, "ensure router interface",
		fctx.ensureRouterInterface,
		shared.Timeout(defaultTimeout), shared.Dependencies(routerReady, subnetReady))

	ensureSecGroup := fctx.AddTask(g, "ensure security group",
		fctx.ensureSecGroup,
		shared.Timeout(defaultTimeout), shared.Dependencies(routerReady))

	_ = fctx.AddTask(g, "ensure security group rules",
		fctx.ensureSecGroupRules,
		shared.Timeout(defaultTimeout), shared.Dependencies(ensureSecGroup))

	_ = fctx.AddTask(g, "ensure ssh key pair",
		fctx.ensureSSHKeyPair,
		shared.Timeout(defaultTimeout), shared.Dependencies(routerReady))

	_ = fctx.AddTask(g, "ensure stackit ssh key pair",
		fctx.ensureStackitSSHKeyPair,
		shared.Timeout(defaultTimeout), shared.Dependencies(routerReady),
		shared.DoIf(fctx.hasStackitMCM),
	)

	return g
}

func (fctx *FlowContext) ensureSNAState(ctx context.Context) error {
	snaConfig, err := infrainternal.GetSNAConfigFromNetworkID(ctx, fctx.networking, fctx.config.Networks.ID)
	if err != nil {
		return err
	}

	infrainternal.InjectConfig(&fctx.config.Networks, snaConfig)
	fctx.nodesCIDR = &snaConfig.WorkersCIDR
	return nil
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

func (fctx *FlowContext) ensureRouter(ctx context.Context) error {
	externalNetworkID := fctx.state.Get(IdentifierFloatingNetwork)
	if externalNetworkID == nil {
		return fmt.Errorf("missing external network ID")
	}

	if fctx.config.Networks.Router != nil {
		fctx.state.Set(IdentifierRouter, fctx.config.Networks.Router.ID)
		return nil
	}
	return fctx.ensureNewRouter(ctx, *externalNetworkID)
}

func (fctx *FlowContext) ensureRouterReady(ctx context.Context) error {
	routerID := fctx.state.Get(IdentifierRouter)
	if routerID == nil || *routerID == "" {
		return fmt.Errorf("no router ID found in the state")
	}

	router, err := fctx.access.GetRouterByID(ctx, *routerID)
	if err != nil {
		return err
	}
	if router == nil {
		return gardenv1beta1helper.NewErrorWithCodes(
			fmt.Errorf("router with ID '%s' was not found", *routerID),
			gardencorev1beta1.ErrorInfraDependencies,
		)
	}
	if len(router.ExternalFixedIPs) < 1 {
		return fmt.Errorf("expected at least one external fixed ip")
	}

	return fctx.ensureEgressCIDRs(router)
}

func (fctx *FlowContext) ensureNewRouter(ctx context.Context, externalNetworkID string) error {
	log := shared.LogFromContext(ctx)

	desired := &access.Router{
		Name:              fctx.defaultRouterName(),
		ExternalNetworkID: externalNetworkID,
		EnableSNAT:        fctx.cloudProfileConfig.UseSNAT,
	}
	current, err := fctx.findExistingRouter(ctx)
	if err != nil {
		return err
	}
	if current == nil {
		floatingPoolSubnetName := fctx.findFloatingPoolSubnetName()
		fctx.state.SetPtr(NameFloatingPoolSubnet, floatingPoolSubnetName)
		if floatingPoolSubnetName != nil {
			log.Info("looking up floating pool subnets...")
			desired.ExternalSubnetIDs, err = fctx.access.LookupFloatingPoolSubnetIDs(ctx, externalNetworkID, *floatingPoolSubnetName)
			if err != nil {
				return err
			}
		}

		log.Info("creating...", "router", fctx.defaultRouterName())
		// TODO: add tags to created resources
		created, err := fctx.access.CreateRouter(ctx, desired)
		if err != nil {
			return err
		}
		fctx.state.Set(IdentifierRouter, created.ID)
		return nil
	}

	fctx.state.Set(IdentifierRouter, current.ID)
	if isRouterUpToDate(current, desired) {
		return nil
	}

	log.Info("updating...", "router", current.Name)
	if _, _, err = fctx.access.UpdateRouter(ctx, desired, current); err != nil {
		return err
	}

	return nil
}

func isRouterUpToDate(current *access.Router, desired *access.Router) bool {
	if current.Name != desired.Name {
		return false
	}
	if current.ExternalNetworkID != desired.ExternalNetworkID {
		return false
	}
	if desired.EnableSNAT != nil && !ptr.Equal(desired.EnableSNAT, current.EnableSNAT) {
		return false
	}
	return true
}

func (fctx *FlowContext) findExistingRouter(ctx context.Context) (*access.Router, error) {
	return findExisting(ctx, fctx.state.Get(IdentifierRouter), fctx.defaultRouterName(), fctx.access.GetRouterByID, fctx.access.GetRouterByName)
}

func (fctx *FlowContext) findFloatingPoolSubnetName() *string {
	if fctx.config.FloatingPoolSubnetName != nil {
		return fctx.config.FloatingPoolSubnetName
	}

	// Second: Check if the CloudProfile contains a default floating subnet and use it.
	if floatingPool, err := helper.FindFloatingPool(fctx.cloudProfileConfig.Constraints.FloatingPools, fctx.config.FloatingPoolName, fctx.infra.Spec.Region, nil); err == nil && floatingPool.DefaultFloatingSubnet != nil {
		return floatingPool.DefaultFloatingSubnet
	}

	return nil
}

func (fctx *FlowContext) ensureNetwork(ctx context.Context) error {
	if fctx.config.Networks.ID != nil {
		fctx.state.Set(IdentifierNetwork, *fctx.config.Networks.ID)
		return nil
	}
	return fctx.ensureNewNetwork(ctx)
}

func (fctx *FlowContext) ensureNetworkReady(ctx context.Context) error {
	networkID := fctx.state.Get(IdentifierNetwork)
	if networkID == nil || *networkID == "" {
		return fmt.Errorf("no network ID found in the state")
	}

	network, err := fctx.access.GetNetworkByID(ctx, *networkID)
	if err != nil {
		return err
	}
	if network == nil {
		return gardenv1beta1helper.NewErrorWithCodes(
			fmt.Errorf("network with ID '%s' was not found", *networkID),
			gardencorev1beta1.ErrorInfraDependencies,
		)
	}

	fctx.state.Set(NameNetwork, network.Name)
	return nil
}

func (fctx *FlowContext) ensureNewNetwork(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	desired := &access.Network{
		Name:         fctx.defaultNetworkName(),
		AdminStateUp: true,
	}
	current, err := fctx.findExistingNetwork(ctx)
	if err != nil {
		return err
	}
	if current == nil {
		log.Info("creating...", "network", fctx.defaultNetworkName())
		created, err := fctx.access.CreateNetwork(ctx, desired)
		if err != nil {
			return err
		}
		fctx.state.Set(IdentifierNetwork, created.ID)
		return nil
	}

	fctx.state.Set(IdentifierNetwork, current.ID)
	if isNetworkUpToDate(current, desired) {
		return nil
	}

	log.Info("updating...", "network", current.Name)
	if _, err := fctx.access.UpdateNetwork(ctx, desired, current); err != nil {
		return err
	}

	return nil
}

func isNetworkUpToDate(current *access.Network, desired *access.Network) bool {
	if current.Name != desired.Name {
		return false
	}
	return current.AdminStateUp == desired.AdminStateUp
}

func (fctx *FlowContext) findExistingNetwork(ctx context.Context) (*access.Network, error) {
	return findExisting(ctx, fctx.state.Get(IdentifierNetwork), fctx.defaultNetworkName(), fctx.access.GetNetworkByID, fctx.access.GetNetworkByName)
}

func (fctx *FlowContext) getNetworkID(ctx context.Context) (*string, error) {
	if fctx.config.Networks.ID != nil {
		return fctx.config.Networks.ID, nil
	}
	networkID := fctx.state.Get(IdentifierNetwork)
	if networkID != nil {
		return networkID, nil
	}
	network, err := fctx.findExistingNetwork(ctx)
	if err != nil {
		return nil, err
	}
	if network != nil {
		fctx.state.Set(IdentifierNetwork, network.ID)
		return &network.ID, nil
	}
	return nil, nil
}

func (fctx *FlowContext) ensureSubnet(ctx context.Context) error {
	// SNA case: because the corresponding shoots SubnetID is never nil.
	if fctx.config.Networks.SubnetID != nil {
		// SNA case
		fctx.state.Set(IdentifierSubnet, *fctx.config.Networks.SubnetID)
		return nil
	}
	// Non SNA case
	return fctx.ensureNewSubnet(ctx)
}

func (fctx *FlowContext) ensureSubnetReady(ctx context.Context) error {
	subnetID := fctx.state.Get(IdentifierSubnet)
	if subnetID == nil || *subnetID == "" {
		return fmt.Errorf("no subnet ID found in the state")
	}

	subnet, err := fctx.access.GetSubnetByID(ctx, *subnetID)
	if err != nil {
		return err
	}
	if subnet == nil {
		return gardenv1beta1helper.NewErrorWithCodes(
			fmt.Errorf("subnet with ID '%s' was not found", *subnetID),
			gardencorev1beta1.ErrorInfraDependencies,
		)
	}

	fctx.dnsNameservers = &subnet.DNSNameservers
	return nil
}

func (fctx *FlowContext) ensureNewSubnet(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	if fctx.state.Get(IdentifierNetwork) == nil {
		return fmt.Errorf("missing cluster network ID")
	}
	networkID := ptr.Deref(fctx.state.Get(IdentifierNetwork), "")

	// Configure DNS settings with the cloud profile as default, while allowing overrides through shoot configuration.
	var dnsServers []string
	dnsServers = fctx.cloudProfileConfig.DNSServers
	if fctx.config.Networks.DNSServers != nil {
		dnsServers = *fctx.config.Networks.DNSServers
	}

	// Backwards compatibility - remove this code in a future version.
	desired := &subnets.Subnet{
		Name:           fctx.defaultSubnetName(),
		NetworkID:      networkID,
		CIDR:           fctx.workerCIDR(),
		IPVersion:      4,
		DNSNameservers: dnsServers,
	}
	current, err := fctx.findExistingSubnet(ctx)
	if err != nil {
		return err
	}
	if current == nil {
		log.Info("creating...", "subnet", fctx.defaultSubnetName())
		created, err := fctx.access.CreateSubnet(ctx, desired)
		if err != nil {
			return err
		}
		fctx.state.Set(IdentifierSubnet, created.ID)
		return nil
	}

	fctx.state.Set(IdentifierSubnet, current.ID)
	if isSubnetUpToDate(current, desired) {
		return nil
	}

	log.Info("updating...", "subnet", current.Name)
	if _, err := fctx.access.UpdateSubnet(ctx, desired, current); err != nil {
		return err
	}

	return nil
}

func isSubnetUpToDate(current, desired *subnets.Subnet) bool {
	if current.Name != desired.Name {
		return false
	}
	return slices.Equal(current.DNSNameservers, desired.DNSNameservers)
}

func (fctx *FlowContext) findExistingSubnet(ctx context.Context) (*subnets.Subnet, error) {
	networkID, err := fctx.getNetworkID(ctx)
	if err != nil {
		return nil, err
	}
	if networkID == nil {
		return nil, nil
	}
	getByName := func(ctx context.Context, name string) ([]*subnets.Subnet, error) {
		return fctx.access.GetSubnetByName(ctx, *networkID, name)
	}
	return findExisting(ctx, fctx.state.Get(IdentifierSubnet), fctx.defaultSubnetName(), fctx.access.GetSubnetByID, getByName)
}

func (fctx *FlowContext) ensureRouterInterface(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	routerID := fctx.state.Get(IdentifierRouter)
	if routerID == nil {
		return fmt.Errorf("internal error: missing routerID")
	}
	subnetID := fctx.state.Get(IdentifierSubnet)
	if subnetID == nil {
		return fmt.Errorf("internal error: missing subnetID")
	}
	portID, err := fctx.access.GetRouterInterfacePortID(ctx, *routerID, *subnetID)
	if err != nil {
		return err
	}
	if portID != nil {
		return nil
	}
	log.Info("creating...")
	return fctx.access.AddRouterInterfaceAndWait(ctx, *routerID, *subnetID)
}

func (fctx *FlowContext) ensureSecGroup(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	desired := &groups.SecGroup{
		Name:        fctx.defaultSecurityGroupName(),
		Description: "Cluster Nodes",
	}
	current, err := findExisting(ctx, fctx.state.Get(IdentifierSecGroup), fctx.defaultSecurityGroupName(), fctx.access.GetSecurityGroupByID, fctx.access.GetSecurityGroupByName)
	if err != nil {
		return err
	}

	if current != nil {
		fctx.state.Set(IdentifierSecGroup, current.ID)
		fctx.state.Set(NameSecGroup, current.Name)
		fctx.state.SetObject(ObjectSecGroup, current)
		return nil
	}

	log.Info("creating...")
	created, err := fctx.access.CreateSecurityGroup(ctx, desired)
	if err != nil {
		return err
	}
	fctx.state.Set(IdentifierSecGroup, created.ID)
	fctx.state.Set(NameSecGroup, created.Name)
	fctx.state.SetObject(ObjectSecGroup, created)
	return nil
}

func (fctx *FlowContext) ensureSecGroupRules(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	obj := fctx.state.GetObject(ObjectSecGroup)
	if obj == nil {
		return fmt.Errorf("internal error: security group object not found")
	}
	group, ok := obj.(*groups.SecGroup)
	if !ok {
		return fmt.Errorf("internal error: casting to SecGroup failed")
	}

	// usual clusters have all nodes in an internal network, for which NAT prevents access by non-cluster nodes
	// for SNA we need to be more restrictive as other project in the same network area would otherwise gain
	// direct access to the node ports
	nodesCIDR := "0.0.0.0/0"
	if fctx.isSNAShoot {
		nodesCIDR = *fctx.nodesCIDR
	}

	desiredRules := []rules.SecGroupRule{
		{
			Direction:     string(rules.DirIngress),
			EtherType:     string(rules.EtherType4),
			RemoteGroupID: access.SecurityGroupIDSelf,
			Description:   "IPv4: allow all incoming traffic within the same security group",
		},
		{
			Direction:   string(rules.DirEgress),
			EtherType:   string(rules.EtherType4),
			Description: "IPv4: allow all outgoing traffic",
		},
		// {
		// 	Direction:   string(rules.DirEgress),
		// 	EtherType:   string(rules.EtherType6),
		// 	Description: "IPv6: allow all outgoing traffic",
		// },
		{
			Direction:      string(rules.DirIngress),
			EtherType:      string(rules.EtherType4),
			Protocol:       string(rules.ProtocolTCP),
			PortRangeMin:   30000,
			PortRangeMax:   32767,
			RemoteIPPrefix: nodesCIDR,
			Description:    "IPv4: allow all incoming tcp traffic with port range 30000-32767",
		},
		{
			Direction:      string(rules.DirIngress),
			EtherType:      string(rules.EtherType4),
			Protocol:       string(rules.ProtocolUDP),
			PortRangeMin:   30000,
			PortRangeMax:   32767,
			RemoteIPPrefix: nodesCIDR,
			Description:    "IPv4: allow all incoming udp traffic with port range 30000-32767",
		},
		{
			Direction:      string(rules.DirIngress),
			EtherType:      string(rules.EtherType4),
			Protocol:       string(rules.ProtocolTCP),
			PortRangeMin:   10256,
			PortRangeMax:   10256,
			RemoteIPPrefix: nodesCIDR,
			Description:    "IPv4: allow incoming tcp traffic to kube-proxy healthcheck port",
		},
	}

	if fctx.networkSpec != nil && fctx.networkSpec.Pods != nil {
		podCIDRRule := rules.SecGroupRule{
			Direction:      string(rules.DirIngress),
			EtherType:      string(rules.EtherType4),
			RemoteIPPrefix: *fctx.networkSpec.Pods,
			Description:    "IPv4: allow all incoming traffic from cluster pod CIDR",
		}
		desiredRules = append(desiredRules, podCIDRRule)
	}

	if modified, err := fctx.access.UpdateSecurityGroupRules(ctx, group, desiredRules, func(rule *rules.SecGroupRule) bool {
		// Do NOT delete unknown rules to keep permissive behavior as with terraform.
		// As we don't store the role ids in the state, this function needs to be adjusted
		// if values in existing rules are changed to identify them for update by replacement.
		return false
	}); err != nil {
		return err
	} else if modified {
		log.Info("updated rules")
	}
	return nil
}

func (fctx *FlowContext) ensureStackitSSHKeyPair(ctx context.Context) error {
	log := shared.LogFromContext(ctx)

	keyPair, err := fctx.iaasClient.GetKeypair(ctx, fctx.defaultSSHKeypairName())
	if err != nil {
		return err
	}
	if keyPair != nil {
		publicKey := keyPair.PublicKey
		// if the public keys are matching then return early. In all other cases we should be creating (or replacing) the keypair with a new one.
		if publicKey != "" && publicKey == string(fctx.infra.Spec.SSHPublicKey) {
			fctx.state.Set(NameKeyPair, *keyPair.Name)
			return nil
		}

		log.Info("replacing stackit SSH key pair")
		if err := fctx.iaasClient.DeleteKeypair(ctx, fctx.defaultSSHKeypairName()); stackitclient.IgnoreNotFoundError(err) != nil {
			return err
		}
		keyPair = nil
		fctx.state.Set(NameKeyPair, "")
	}

	if len(fctx.infra.Spec.SSHPublicKey) == 0 {
		fctx.state.Set(NameKeyPair, "")
		return nil
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

func (fctx *FlowContext) ensureSSHKeyPair(ctx context.Context) error {
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

	if len(fctx.infra.Spec.SSHPublicKey) == 0 {
		fctx.state.Set(NameKeyPair, "")
		return nil
	}

	log.Info("creating SSH key pair")
	if keyPair, err = fctx.compute.CreateKeyPair(ctx, fctx.defaultSSHKeypairName(), string(fctx.infra.Spec.SSHPublicKey)); err != nil {
		return err
	}
	fctx.state.Set(NameKeyPair, keyPair.Name)
	return nil
}

func (fctx *FlowContext) ensureEgressCIDRs(router *access.Router) error {
	result := make([]string, 0, len(router.ExternalFixedIPs))
	for _, efip := range router.ExternalFixedIPs {
		result = append(result, efip.IPAddress)
	}
	fctx.state.SetObject(IdentifierEgressCIDRs, result)
	return nil
}
