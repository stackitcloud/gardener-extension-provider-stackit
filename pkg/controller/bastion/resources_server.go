package bastion

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

func (r *Resources) reconcileServer(ctx context.Context, log logr.Logger) error {
	if r.Server != nil {
		// TODO: consider deleting server if it is in ERROR
		return nil
	}

	var err error
	r.Server, err = r.IaaS.CreateServer(ctx, iaas.CreateServerPayload{
		Name:   new(r.ResourceName),
		Labels: new(stackit.ToLabels(r.Labels)),

		AvailabilityZone: new(r.AvailabilityZone),
		MachineType:      new(r.MachineType),
		BootVolume: &iaas.ServerBootVolume{
			DeleteOnTermination: new(true),
			Source:              iaas.NewBootVolumeSource(r.ImageID, "image"),
			// TODO: make size and performance class configurable
			Size: new(int64(10)),
		},

		SecurityGroups: new([]string{r.SecurityGroup.GetId()}),
		Networking: new(iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(&iaas.CreateServerNetworking{
			NetworkId: new(r.NetworkID),
		})),

		UserData: new(r.Bastion.Spec.UserData),
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
