package bastion

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

func (r *Resources) reconcileServer(ctx context.Context, log logr.Logger) error {
	if r.Server != nil {
		// TODO: consider deleting server if it is in ERROR
		return nil
	}

	var err error
	r.Server, err = r.IaaS.CreateServer(ctx, iaas.CreateServerPayload{
		Name:   ptr.To(r.ResourceName),
		Labels: ptr.To(stackit.ToLabels(r.Labels)),

		AvailabilityZone: ptr.To(r.AvailabilityZone),
		MachineType:      ptr.To(r.MachineType),
		BootVolume: &iaas.ServerBootVolume{
			DeleteOnTermination: ptr.To(true),
			Source:              iaas.NewBootVolumeSource(r.ImageID, "image"),
			// TODO: make size and performance class configurable
			Size: ptr.To[int64](10),
		},

		SecurityGroups: ptr.To([]string{r.SecurityGroup.GetId()}),
		Networking: ptr.To(iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(&iaas.CreateServerNetworking{
			NetworkId: ptr.To(r.NetworkID),
		})),

		UserData: ptr.To(r.Bastion.Spec.UserData),
	})
	if err != nil {
		return fmt.Errorf("error creating server: %w", err)
	}

	log.Info("Created server", "server", r.Server.GetId())
	return nil
}

func (r *Resources) deleteServer(ctx context.Context, log logr.Logger) error {
	if r.Server == nil {
		return nil
	}

	if err := r.IaaS.DeleteServer(ctx, r.Server.GetId()); err != nil {
		return fmt.Errorf("error deleting server: %w", err)
	}

	log.Info("Deleted server", "server", r.Server.GetId())
	return nil
}
