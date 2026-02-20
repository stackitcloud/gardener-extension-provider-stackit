package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	osclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack/client"
)

// SNAConfig contains relevant values for SNA clusters that can be determined
// using the provided Network ID.
type SNAConfig struct {
	NetworkID   string
	RouterID    string
	SubnetID    string
	WorkersCIDR string
}

const (
	// LabelAreaID is the label key used to identify Shoots that are SNA-enabled.
	LabelAreaID = "stackit.cloud/area-id"
)

func GetSNAConfigFromNetworkID(ctx context.Context, networking osclient.Networking, networkID *string) (*SNAConfig, error) {
	if networkID == nil {
		return nil, fmt.Errorf("no networkID available")
	}

	subnet, err := getSubnet(ctx, networking, *networkID)
	if err != nil {
		return nil, err
	}

	routerID, err := getSNARouterIDFromNetworkID(ctx, networking, *networkID)
	if err != nil {
		return nil, err
	}

	return &SNAConfig{
		NetworkID:   *networkID,
		RouterID:    routerID,
		SubnetID:    subnet.ID,
		WorkersCIDR: subnet.CIDR,
	}, nil
}

func getSNARouterIDFromNetworkID(ctx context.Context, networking osclient.Networking, networkID string) (string, error) {
	list, err := networking.GetRouterInterfacePortsByNetwork(ctx, networkID)
	if err != nil {
		return "", fmt.Errorf("failed to list ports for network %s: %w", networkID, err)
	}

	filtered := make([]*routers.Router, 0, len(list))
	for _, port := range list {
		router, err := networking.GetRouterByID(ctx, port.DeviceID)
		if err != nil {
			return "", fmt.Errorf("failed to resolve router %s: %w", port.DeviceID, err)
		}
		if len(router.GatewayInfo.ExternalFixedIPs) == 0 {
			continue
		}
		if !slices.Contains(router.Tags, "SNA") {
			return "", fmt.Errorf("found non-SNA router with external gateway %s", port.DeviceID)
		}

		filtered = append(filtered, router)
	}

	if len(filtered) == 1 {
		router := filtered[0]
		if slices.Contains(router.Tags, "internal") {
			return "", errors.New("only internal router available")
		}
		return router.ID, nil
	} else if len(list) > 1 {
		var externalRouter *routers.Router
		for _, router := range filtered {
			// if multiple routers exist, then use the one with the external tag
			if slices.Contains(router.Tags, "external") {
				if externalRouter != nil {
					return "", errors.New("multiple external routers found")
				}
				externalRouter = router
			}
		}
		if externalRouter != nil {
			return externalRouter.ID, nil
		}
	}

	return "", fmt.Errorf("no router found in given network %s", networkID)
}

// isSNAShoot determines if a Shoot is in fact SNA-enabled based on its labels.
// For this to be true, the areaID label value must be non-empty.
func IsSNAShoot(labels map[string]string) bool {
	return labels[LabelAreaID] != ""
}

func getSubnet(ctx context.Context, networking osclient.Networking, networkID string) (*subnets.Subnet, error) {
	snets, err := networking.ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID})

	if err != nil {
		return nil, fmt.Errorf("error retrieving routers: %w", err)
	}
	if len(snets) == 0 {
		return nil, fmt.Errorf("no subnets available")
	}
	if len(snets) != 1 {
		return nil, fmt.Errorf("found multiple subnets, only one is expected")
	}

	return &snets[0], nil
}

func InjectConfig(config *stackitv1alpha1.Networks, snaConfig *SNAConfig) {
	config.Router = &stackitv1alpha1.Router{ID: snaConfig.RouterID}
	config.Workers = snaConfig.WorkersCIDR
	config.ID = &snaConfig.NetworkID
	config.SubnetID = &snaConfig.SubnetID
}
