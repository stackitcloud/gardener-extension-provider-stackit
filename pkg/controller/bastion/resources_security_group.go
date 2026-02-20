package bastion

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/go-logr/logr"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
)

const portSSH = 22

var portRangeSSH = iaas.NewPortRange(portSSH, portSSH)

func (r *Resources) reconcileSecurityGroup(ctx context.Context, log logr.Logger) error {
	if r.SecurityGroup == nil {
		var err error
		r.SecurityGroup, err = r.IaaS.CreateSecurityGroup(ctx, iaas.CreateSecurityGroupPayload{
			Name:   ptr.To(r.ResourceName),
			Labels: ptr.To(stackit.ToLabels(r.Labels)),

			Description: ptr.To("Security group for Bastion " + r.Bastion.Name),
		})
		if err != nil {
			return fmt.Errorf("error creating security group: %w", err)
		}

		log.Info("Created security group", "securityGroup", r.SecurityGroup.GetId())
	}

	wantedRules, err := r.determineWantedSecurityGroupRules()
	if err != nil {
		return fmt.Errorf("error getting wanted security group rules: %w", err)
	}

	return r.IaaS.ReconcileSecurityGroupRules(ctx, log, r.SecurityGroup, wantedRules)
}

func (r *Resources) deleteSecurityGroup(ctx context.Context, log logr.Logger) error {
	if r.SecurityGroup == nil {
		return nil
	}

	// Delete the security group. This includes all rules in the security group and any rules in other security groups
	// referencing this group via RemoteSecurityGroupId.
	if err := r.IaaS.DeleteSecurityGroup(ctx, r.SecurityGroup.GetId()); err != nil {
		return fmt.Errorf("error deleting security group: %w", err)
	}

	log.Info("Deleted security group", "securityGroup", r.SecurityGroup.GetId())
	return nil
}

func (o *Options) determineWantedSecurityGroupRules() ([]iaas.SecurityGroupRule, error) {
	rules := []iaas.SecurityGroupRule{
		{
			// DHCP tells us our IP and the route to the metadata server
			Description: ptr.To("Allow DHCP requests"),

			Direction: ptr.To(stackit.DirectionEgress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolUDP),
			PortRange: iaas.NewPortRange(68, 67),

			IpRange: ptr.To("255.255.255.255/32"),
		},
		{
			Description: ptr.To("Allow egress to metadata server"),

			Direction: ptr.To(stackit.DirectionEgress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolTCP),
			PortRange: iaas.NewPortRange(80, 80),

			IpRange: ptr.To("169.254.169.254/32"),
		},
		{
			Description: ptr.To(fmt.Sprintf("Allow egress from Bastion %s to %s worker nodes", o.Bastion.Name, o.TechnicalID)),

			Direction: ptr.To(stackit.DirectionEgress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolTCP),
			PortRange: portRangeSSH,

			RemoteSecurityGroupId: ptr.To(o.WorkerSecurityGroupID),
		},
	}

	if len(o.Bastion.Spec.Ingress) == 0 {
		// If the Bastion doesn't specify ingress restrictions, we need to add a rule allowing all ingress
		rules = append(rules, iaas.SecurityGroupRule{
			Description: ptr.To(fmt.Sprintf("Allow ingress to Bastion %s from world", o.Bastion.Name)),

			Direction: ptr.To(stackit.DirectionIngress),
			Ethertype: ptr.To(stackit.EtherTypeIPv4),
			Protocol:  ptr.To(stackit.ProtocolTCP),
			PortRange: portRangeSSH,

			IpRange: ptr.To("0.0.0.0/0"),
		})
	}

	for _, ingress := range o.Bastion.Spec.Ingress {
		cidr := ingress.IPBlock.CIDR
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid Bastion ingress CIDR %q: %w", cidr, err)
		}

		etherType := stackit.EtherTypeIPv4
		if prefix.Addr().Is6() {
			etherType = stackit.EtherTypeIPv6
		}

		normalizedCIDR := prefix.Masked().String()
		rules = append(rules, iaas.SecurityGroupRule{
			Description: ptr.To(fmt.Sprintf("Allow ingress to Bastion %s from %s", o.Bastion.Name, normalizedCIDR)),

			Direction: ptr.To(stackit.DirectionIngress),
			Ethertype: ptr.To(etherType),
			Protocol:  ptr.To(stackit.ProtocolTCP),
			PortRange: portRangeSSH,

			IpRange: ptr.To(normalizedCIDR),
		})
	}

	return rules, nil
}

func (r *Resources) reconcileWorkerSecurityGroupRule(ctx context.Context, log logr.Logger) error {
	// This rule is deleted automatically when the referenced Bastion security group (RemoteSecurityGroupId) is deleted.
	wantedRule := iaas.SecurityGroupRule{
		Description: ptr.To(fmt.Sprintf("Allow ingress to shoot worker nodes from Bastion %s", r.Bastion.Name)),

		Direction: ptr.To(stackit.DirectionIngress),
		Ethertype: ptr.To(stackit.EtherTypeIPv4),
		Protocol:  ptr.To(stackit.ProtocolTCP),
		PortRange: portRangeSSH,

		RemoteSecurityGroupId: ptr.To(r.SecurityGroup.GetId()),
	}

	createdRule, err := r.IaaS.CreateSecurityGroupRule(ctx, r.WorkerSecurityGroupID, wantedRule)
	if err != nil {
		if stackitclient.IsConflictError(err) {
			log.V(1).Info("Worker security group rule already exists", "securityGroup", r.WorkerSecurityGroupID, "description", wantedRule.GetDescription())
			return nil
		}
		return fmt.Errorf("error creating security group rule %q in worker group %s: %w", wantedRule.GetDescription(), r.WorkerSecurityGroupID, err)
	}

	log.Info("Created worker security group rule", "securityGroup", r.WorkerSecurityGroupID, "securityGroupRule", createdRule.GetId(), "description", createdRule.GetDescription())
	return nil
}
