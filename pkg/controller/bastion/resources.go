package bastion

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"

	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

// Resources holds the STACKIT resources created for a Bastion along with all input (options) and the needed clients.
type Resources struct {
	Options
	IaaS stackitclient.IaaSClient

	SecurityGroup *iaas.SecurityGroup
	Server        *iaas.Server
	PublicIP      *iaas.PublicIp
}

func (r *Resources) getExistingResources(ctx context.Context, log logr.Logger) error {
	var err error

	secGroups, err := r.IaaS.GetSecurityGroupByName(ctx, r.ResourceName)
	if err != nil {
		return fmt.Errorf("error getting security group: %w", err)
	}
	if len(secGroups) > 1 {
		return fmt.Errorf("found multiple secGroups with the name %s", r.ResourceName)
	}
	if len(secGroups) == 1 {
		r.SecurityGroup = &secGroups[0]
		log.V(1).Info("Found existing security group", "securityGroup", r.SecurityGroup.GetId())
	}

	servers, err := r.IaaS.GetServerByName(ctx, r.ResourceName)
	if err != nil {
		return fmt.Errorf("error getting server: %w", err)
	}
	if len(servers) > 1 {
		return fmt.Errorf("found multiple servers with the name %s", r.ResourceName)
	}
	if len(secGroups) == 1 {
		r.Server = &servers[0]
		log.V(1).Info("Found existing server", "server", r.Server.GetId())
	}

	publicIPs, err := r.IaaS.GetPublicIpByLabels(ctx, r.Labels)
	if err != nil {
		return fmt.Errorf("error getting public IP: %w", err)
	}
	if len(servers) > 1 {
		return fmt.Errorf("found multiple servers with the name %s", r.ResourceName)
	}
	if len(secGroups) == 1 {
		r.PublicIP = &publicIPs[0]
		log.V(1).Info("Found existing public IP", "publicIP", r.PublicIP.GetId())
	}
	return nil
}
