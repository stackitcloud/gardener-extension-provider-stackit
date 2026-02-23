package client

import (
	"context"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

type IaaSClient interface {
	ProjectID() string

	// Network
	CreateIsolatedNetwork(ctx context.Context, payload iaas.CreateIsolatedNetworkPayload) (*iaas.Network, error)
	GetNetworkById(ctx context.Context, id string) (*iaas.Network, error)
	GetNetworkByName(ctx context.Context, name string) ([]iaas.Network, error)
	UpdateNetwork(ctx context.Context, networkId string, payload iaas.PartialUpdateNetworkPayload) (*iaas.Network, error)
	DeleteNetwork(ctx context.Context, networkID string) error

	CreateSecurityGroup(ctx context.Context, payload iaas.CreateSecurityGroupPayload) (*iaas.SecurityGroup, error)
	DeleteSecurityGroup(ctx context.Context, securityGroupId string) error
	GetSecurityGroupByName(ctx context.Context, name string) ([]iaas.SecurityGroup, error)
	GetSecurityGroupById(ctx context.Context, securityGroupId string) (*iaas.SecurityGroup, error)

	CreateSecurityGroupRule(ctx context.Context, securityGroupId string, wantedRule iaas.SecurityGroupRule) (*iaas.SecurityGroupRule, error)
	ReconcileSecurityGroupRules(ctx context.Context, log logr.Logger, securityGroup *iaas.SecurityGroup, wantedRules []iaas.SecurityGroupRule) error
	UpdateSecurityGroupRules(ctx context.Context, group *iaas.SecurityGroup, desiredRules []iaas.SecurityGroupRule, allowDelete func(rule *iaas.SecurityGroupRule) bool) (modified bool, err error)

	CreateServer(ctx context.Context, payload iaas.CreateServerPayload) (*iaas.Server, error)
	DeleteServer(ctx context.Context, serverId string) error
	GetServerByName(ctx context.Context, name string) ([]iaas.Server, error)

	CreatePublicIp(ctx context.Context, payload iaas.CreatePublicIPPayload) (*iaas.PublicIp, error)
	DeletePublicIp(ctx context.Context, publicIpId string) error
	GetPublicIpByLabels(ctx context.Context, selector stackit.LabelSelector) ([]iaas.PublicIp, error)
	AddPublicIpToServer(ctx context.Context, serverId, publicIpId string) error

	GetKeypair(ctx context.Context, name string) (*iaas.Keypair, error)
	CreateKeypair(ctx context.Context, name, publicKey string) (*iaas.Keypair, error)
	DeleteKeypair(ctx context.Context, name string) error
}

type iaasClient struct {
	Client    iaas.DefaultApi
	projectID string
	region    string
}

func (c iaasClient) UpdateSecurityGroupRules(ctx context.Context, group *iaas.SecurityGroup, desiredRules []iaas.SecurityGroupRule, allowDelete func(rule *iaas.SecurityGroupRule) bool) (modified bool, err error) {
	for i := range group.GetRules() {
		rule := &group.GetRules()[i]
		if desiredRule := findMatchingRule(*rule, desiredRules); desiredRule == nil {
			if allowDelete == nil || allowDelete(rule) {
				if err = c.Client.DeleteSecurityGroupRule(ctx, c.projectID, c.region, group.GetId(), rule.GetId()).Execute(); err != nil {
					err = fmt.Errorf("error deleting rule for security group %s: %s", rule.GetId(), err)
					return
				}
				modified = true
			}
		} else {
			desiredRule.Id = rule.Id // mark as found
		}
	}

	for i := range desiredRules {
		rule := &desiredRules[i]
		if rule.GetId() != "" {
			// ignore found rules
			continue
		}
		createOpts := iaas.CreateSecurityGroupRulePayload{
			Direction:             rule.Direction,
			Description:           rule.Description,
			Ethertype:             rule.Ethertype,
			SecurityGroupId:       rule.SecurityGroupId,
			RemoteSecurityGroupId: rule.RemoteSecurityGroupId,
			IpRange:               rule.IpRange,
		}
		if rule.HasProtocol() {
			createOpts.Protocol = ptr.To(iaas.StringAsCreateProtocol(rule.Protocol.Name))
		}
		// TODO: Ports are only supported for DCCP, SCTP, TCP, UDP and UDPLITE
		if portRange, ok := rule.GetPortRangeOk(); ok {
			createOpts.PortRange = iaas.NewPortRange(portRange.GetMax(), portRange.GetMin())
		}
		if _, err = c.Client.CreateSecurityGroupRule(ctx, c.projectID, c.region, group.GetId()).CreateSecurityGroupRulePayload(createOpts).Execute(); err != nil {
			err = fmt.Errorf("error creating rule %d for security group: %s", i, err)
			return
		}
		modified = true
	}
	return
}

func (c iaasClient) UpdateNetwork(ctx context.Context, networkId string, payload iaas.PartialUpdateNetworkPayload) (*iaas.Network, error) {
	err := c.Client.PartialUpdateNetwork(ctx, c.projectID, c.region, networkId).PartialUpdateNetworkPayload(payload).Execute()
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (c iaasClient) GetNetworkById(ctx context.Context, id string) (*iaas.Network, error) {
	return c.Client.GetNetwork(ctx, c.projectID, c.region, id).Execute()
}

func (c iaasClient) GetNetworkByName(ctx context.Context, name string) ([]iaas.Network, error) {
	networks, err := c.Client.ListNetworks(ctx, c.projectID, c.region).Execute()
	if err != nil {
		return nil, fmt.Errorf("error listing security groups: %w", err)
	}

	filteredNetworks := slices.DeleteFunc(networks.GetItems(), func(network iaas.Network) bool {
		// Delete obj from slice where name does not match
		return network.GetName() != name
	})

	return filteredNetworks, nil
}

func NewIaaSClient(region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) (IaaSClient, error) {
	options := clientOptions(&region, endpoints, credentials)

	if endpoints.IaaS != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.IaaS))
	}

	if endpoints.TokenEndpoint != nil {
		options = append(options, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	apiClient, err := iaas.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &iaasClient{
		Client:    apiClient,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (c iaasClient) CreateIsolatedNetwork(ctx context.Context, payload iaas.CreateIsolatedNetworkPayload) (*iaas.Network, error) {
	return c.Client.CreateIsolatedNetwork(ctx, c.projectID, c.region).CreateIsolatedNetworkPayload(payload).Execute()
}

func (c iaasClient) DeleteNetwork(ctx context.Context, networkID string) error {
	return c.Client.DeleteNetwork(ctx, c.projectID, c.region, networkID).Execute()
}

func (c iaasClient) ProjectID() string {
	return c.projectID
}

func (c iaasClient) CreateSecurityGroup(ctx context.Context, payload iaas.CreateSecurityGroupPayload) (*iaas.SecurityGroup, error) {
	return c.Client.CreateSecurityGroup(ctx, c.projectID, c.region).CreateSecurityGroupPayload(payload).Execute()
}

func (c iaasClient) DeleteSecurityGroup(ctx context.Context, securityGroupId string) error {
	return c.Client.DeleteSecurityGroupExecute(ctx, c.projectID, c.region, securityGroupId)
}

// GetSecurityGroupByName finds the first security group with the given name.
func (c iaasClient) GetSecurityGroupByName(ctx context.Context, name string) ([]iaas.SecurityGroup, error) {
	securityGroups, err := c.Client.ListSecurityGroupsExecute(ctx, c.projectID, c.region)
	if err != nil {
		return nil, fmt.Errorf("error listing security groups: %w", err)
	}

	filteredSecurityGroups := slices.DeleteFunc(securityGroups.GetItems(), func(secGroup iaas.SecurityGroup) bool {
		// Delete obj from slice where name does not match
		return secGroup.GetName() != name
	})

	return filteredSecurityGroups, nil
}

func (c iaasClient) GetSecurityGroupById(ctx context.Context, securityGroupId string) (*iaas.SecurityGroup, error) {
	return c.Client.GetSecurityGroup(ctx, c.projectID, c.region, securityGroupId).Execute()
}

func (c iaasClient) CreateSecurityGroupRule(ctx context.Context, securityGroupId string, wantedRule iaas.SecurityGroupRule) (*iaas.SecurityGroupRule, error) {
	return c.Client.CreateSecurityGroupRule(ctx, c.projectID, c.region, securityGroupId).CreateSecurityGroupRulePayload(securityGroupRuleToCreatePayload(wantedRule)).Execute()
}

// ReconcileSecurityGroupRules updates the rules of the given security group to the desired state.
// The method deletes any unwanted rules (existing rules without matching wanted rules) and creates any missing rules.
// The method relies on SecurityGroup being read from the API beforehand.
func (c iaasClient) ReconcileSecurityGroupRules(ctx context.Context, log logr.Logger, securityGroup *iaas.SecurityGroup, wantedRules []iaas.SecurityGroupRule) error {
	log = log.WithValues("securityGroup", securityGroup.GetId())

	// find matching existing rules and deleted unwanted rules
	for _, existingRule := range securityGroup.GetRules() {
		ruleLog := log.WithValues("securityGroupRule", existingRule.GetId(), "description", existingRule.GetDescription())

		if wantedRule := findMatchingRule(existingRule, wantedRules); wantedRule != nil {
			// wanted rule found in the existing rules, mark the wanted rule as found by storing the ID
			wantedRule.Id = ptr.To(existingRule.GetId())

			ruleLog.V(1).Info("Found existing security group rule")
		} else {
			// delete unwanted rule
			if err := c.Client.DeleteSecurityGroupRuleExecute(ctx, c.projectID, c.region, securityGroup.GetId(), existingRule.GetId()); err != nil {
				return fmt.Errorf("error deleting unwanted security group rule %s in group %s: %w", existingRule.GetId(), securityGroup.GetId(), err)
			}

			ruleLog.Info("Deleted unwanted security group rule")
		}
	}

	// create missing rules
	for _, wantedRule := range wantedRules {
		if wantedRule.HasId() {
			// ignore already existing rules
			continue
		}

		createdRule, err := c.Client.CreateSecurityGroupRule(ctx, c.projectID, c.region, securityGroup.GetId()).
			CreateSecurityGroupRulePayload(securityGroupRuleToCreatePayload(wantedRule)).
			Execute()
		if err != nil {
			return fmt.Errorf("error creating security group rule %q in group %s: %w", wantedRule.GetDescription(), securityGroup.GetId(), err)
		}

		log.Info("Created security group rule", "securityGroupRule", createdRule.GetId(), "description", createdRule.GetDescription())
	}

	return nil
}

// findMatchingRule returns a pointer to the item in wantedRules matching the given rule.
func findMatchingRule(rule iaas.SecurityGroupRule, wantedRules []iaas.SecurityGroupRule) *iaas.SecurityGroupRule {
	for i, wanted := range wantedRules {
		if wanted.HasId() {
			// ignore already existing rules
			continue
		}

		// We want to ignore the description, because OpenStack by default already created egress SecGroupRules for IPv4
		// and IPv6 but without an description. This way we can avoid the infra controller trying to either re-create or
		// recreate essentially the same rule (plus description)

		// The infra controller when creating a SecGroup, unlike OpenStack infra ctrl, now initially wipes the SecGroup so
		// that the default from OpenStack does not carry over.
		if cmp.Equal(rule, wanted, stackit.ProtocolComparison, cmpopts.IgnoreFields(iaas.SecurityGroupRule{}, "Description", "Id", "CreatedAt", "UpdatedAt", "SecurityGroupId")) {
			return &wantedRules[i]
		}
	}

	return nil
}

// securityGroupRuleToCreatePayload transforms the given SecurityGroupRule to an equivalent CreateSecurityGroupRulePayload.
func securityGroupRuleToCreatePayload(rule iaas.SecurityGroupRule) iaas.CreateSecurityGroupRulePayload {
	payload := iaas.CreateSecurityGroupRulePayload{
		Description:           rule.Description,
		Direction:             rule.Direction,
		Ethertype:             rule.Ethertype,
		IcmpParameters:        rule.IcmpParameters,
		IpRange:               rule.IpRange,
		PortRange:             rule.PortRange,
		RemoteSecurityGroupId: rule.RemoteSecurityGroupId,
	}

	if rule.HasProtocol() {
		payload.Protocol = ptr.To(iaas.StringAsCreateProtocol(rule.Protocol.Name))
	}

	return payload
}

func (c iaasClient) CreateServer(ctx context.Context, payload iaas.CreateServerPayload) (*iaas.Server, error) {
	return c.Client.CreateServer(ctx, c.projectID, c.region).CreateServerPayload(payload).Execute()
}

func (c iaasClient) DeleteServer(ctx context.Context, serverId string) error {
	return c.Client.DeleteServerExecute(ctx, c.projectID, c.region, serverId)
}

// GetServerByName finds the first server with the given name.
func (c iaasClient) GetServerByName(ctx context.Context, name string) ([]iaas.Server, error) {
	servers, err := c.Client.ListServersExecute(ctx, c.projectID, c.region)
	if err != nil {
		return nil, fmt.Errorf("error listing servers: %w", err)
	}

	filteredServers := slices.DeleteFunc(servers.GetItems(), func(server iaas.Server) bool {
		// Delete obj from slice where name does not match
		return server.GetName() != name
	})

	return filteredServers, nil
}

func (c iaasClient) CreatePublicIp(ctx context.Context, payload iaas.CreatePublicIPPayload) (*iaas.PublicIp, error) {
	return c.Client.CreatePublicIP(ctx, c.projectID, c.region).CreatePublicIPPayload(payload).Execute()
}

func (c iaasClient) DeletePublicIp(ctx context.Context, publicIpId string) error {
	return c.Client.DeletePublicIPExecute(ctx, c.projectID, c.region, publicIpId)
}

// GetPublicIpByLabels finds the first public IP that matches the given label selector. Public IPs don't have a name,
// so matching by label is our best option.
func (c iaasClient) GetPublicIpByLabels(ctx context.Context, selector stackit.LabelSelector) ([]iaas.PublicIp, error) {
	publicIPs, err := c.Client.ListPublicIPsExecute(ctx, c.projectID, c.region)
	if err != nil {
		return nil, fmt.Errorf("error listing public IPs: %w", err)
	}

	filteredIPs := slices.DeleteFunc(publicIPs.GetItems(), func(ip iaas.PublicIp) bool {
		// Delete obj from slice where label does not match
		return !selector.Matches(ip.GetLabels())
	})

	return filteredIPs, nil
}

func (c iaasClient) AddPublicIpToServer(ctx context.Context, serverId, publicIpId string) error {
	return c.Client.AddPublicIpToServerExecute(ctx, c.projectID, c.region, serverId, publicIpId)
}

func (c iaasClient) GetKeypair(ctx context.Context, name string) (*iaas.Keypair, error) {
	keypair, err := c.Client.GetKeyPairExecute(ctx, name)
	if IsNotFound(err) {
		return nil, nil
	}
	return keypair, err
}

func (c iaasClient) CreateKeypair(ctx context.Context, name, publicKey string) (*iaas.Keypair, error) {
	return c.Client.CreateKeyPair(ctx).CreateKeyPairPayload(iaas.CreateKeyPairPayload{Name: &name, PublicKey: &publicKey}).Execute()
}

func (c iaasClient) DeleteKeypair(ctx context.Context, name string) error {
	return c.Client.DeleteKeyPairExecute(ctx, name)
}

func IsolatedNetworkToPartialUpdate(network iaas.CreateIsolatedNetworkPayload) iaas.PartialUpdateNetworkPayload {
	return iaas.PartialUpdateNetworkPayload{
		Dhcp:   network.Dhcp,
		Labels: network.Labels,
		Name:   network.Name,
		Ipv4: &iaas.UpdateNetworkIPv4Body{
			Gateway:     network.Ipv4.CreateNetworkIPv4WithPrefix.Gateway,
			Nameservers: network.Ipv4.CreateNetworkIPv4WithPrefix.Nameservers,
		},
	}
}
