package selfhostedshootexposure

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"

	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

// Resources holds the STACKIT resources created for a Self-hosted Shoot Exposure
// along with all inputs (options) and the needed clients.
type Resources struct {
	Options
	LBClient stackitclient.LoadBalancingClient

	LoadBalancer *loadbalancer.LoadBalancer
}

func (r *Resources) getExistingResources(ctx context.Context, log logr.Logger) error {
	lb, err := r.LBClient.GetLoadBalancer(ctx, r.ResourceName)
	if err != nil {
		if stackitclient.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("error getting load balancer: %w", err)
	}
	r.LoadBalancer = lb
	log.V(1).Info("Found existing load balancer", "loadBalancer", r.LoadBalancer.GetName())
	return nil
}
