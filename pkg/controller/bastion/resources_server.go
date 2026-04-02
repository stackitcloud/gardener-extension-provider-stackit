package bastion

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/go-logr/logr"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

func (r *Resources) reconcileServer(ctx context.Context, log logr.Logger) error {
	if r.Server != nil {
		// TODO: consider deleting server if it is in ERROR
		return nil
	}

	var err error
	r.Server, err = r.IaaS.CreateServer(ctx, iaas.CreateServerPayload{
		Name:   r.ResourceName,
		Labels: stackit.ToLabels(r.Labels),

		AvailabilityZone: new(r.AvailabilityZone),
		MachineType:      r.MachineType,
		BootVolume: &iaas.BootVolume{
			DeleteOnTermination: new(true),
			Source:              iaas.NewBootVolumeSource(r.ImageID, "image"),
			// TODO: make size and performance class configurable
			Size: new(int64(10)),
		},

		SecurityGroups: []string{r.SecurityGroup.GetId()},
		Networking: iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(&iaas.CreateServerNetworking{
			NetworkId: new(r.NetworkID),
		}),

		UserData: new(base64.StdEncoding.EncodeToString(r.Bastion.Spec.UserData)),
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
