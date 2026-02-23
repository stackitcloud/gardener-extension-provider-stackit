package bastion

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

func (r *Resources) reconcilePublicIP(ctx context.Context, log logr.Logger) error {
	if r.PublicIP == nil {
		var err error
		r.PublicIP, err = r.IaaS.CreatePublicIp(ctx, iaas.CreatePublicIPPayload{
			Labels: ptr.To(stackit.ToLabels(r.Labels)),
		})
		if err != nil {
			return fmt.Errorf("error creating public IP: %w", err)
		}

		log.Info("Created public IP", "publicIP", r.PublicIP.GetId())
	}

	if networkInterface := ptr.Deref(r.PublicIP.GetNetworkInterface(), ""); networkInterface != "" {
		log.V(1).Info("Public IP is already associated with network interface", "publicIP", r.PublicIP.GetId(), "networkInterface", networkInterface)
		return nil
	}

	if err := r.IaaS.AddPublicIpToServer(ctx, r.Server.GetId(), r.PublicIP.GetId()); err != nil {
		return fmt.Errorf("error adding public IP %s to server %s: %w", r.PublicIP.GetId(), r.Server.GetId(), err)
	}
	log.Info("Added public IP to server", "server", r.Server.GetId(), "publicIP", r.PublicIP.GetId())

	return nil
}

func (r *Resources) deletePublicIP(ctx context.Context, log logr.Logger) error {
	if r.PublicIP == nil {
		return nil
	}

	if err := r.IaaS.DeletePublicIp(ctx, r.PublicIP.GetId()); err != nil {
		return fmt.Errorf("error deleting public IP: %w", err)
	}

	log.Info("Deleted public IP", "publicIP", r.PublicIP.GetId())
	return nil
}
