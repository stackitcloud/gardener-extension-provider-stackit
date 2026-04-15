package selfhostedshootexposure

import (
	"context"
	"fmt"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	reconcilerutils "github.com/gardener/gardener/pkg/controllerutils/reconciler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

const (
	ExposureLabelKey = "exposure.stackit.cloud"
)

// Options contains all input required for creating a STACKIT LB for a self-hosted shoot on STACKIT.
// The options are determined from the SelfHostedShootExposure and Cluster object.
type Options struct {
	SelfHostedShootExposure *extensionsv1alpha1.SelfHostedShootExposure

	// ProjectID is the STACKIT project ID of the shoot. Currently determined from the cloudprovider (credentials) secret.
	ProjectID string
	// ResourceName of all STACKIT resources for this SelfHostedShootExposure.
	ResourceName string
	// Labels added to all STACKIT resources.
	Labels map[string]string

	// Region for the LB, determined from Cluster.spec.shoot.spec.region (RegionOne is replaced with eu01).
	Region string
	// NetworkID is the ID of the network where the control plane nodes reside.
	NetworkID string
	// PlanId specifies the service plan (size) of the load balancer.
	PlanId string
}

func (a *Actuator) DetermineOptions(ctx context.Context, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster, projectID string) (*Options, error) {
	opts := &Options{
		SelfHostedShootExposure: exposure,
		ProjectID:               projectID,
		ResourceName:            fmt.Sprintf("%s-exposure-%s", cluster.Shoot.Status.TechnicalID, exposure.Name),
		// STACKIT LB labels do not allow '/' in keys, so we use the flat dot-separated form
		// matching the convention used for other STACKIT LBs (see controlplane.STACKITLBClusterLabelKey).
		Labels: map[string]string{
			controlplane.STACKITLBClusterLabelKey: cluster.Shoot.Status.TechnicalID,
			ExposureLabelKey:                      exposure.Name,
		},
		Region: stackit.DetermineRegion(cluster),
	}

	// Get the network where the control plane resides
	infraStatus, err := getInfrastructureStatus(ctx, a.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("error getting InfrastructureStatus: %w", err)
	}
	opts.NetworkID = infraStatus.Networks.ID

	// Decode providerConfig to extract STACKIT-specific settings
	if exposure.Spec.ProviderConfig != nil {
		providerConfig := &stackitv1alpha1.SelfHostedShootExposureConfig{}
		if _, _, err := a.Decoder.Decode(exposure.Spec.ProviderConfig.Raw, nil, providerConfig); err != nil {
			return nil, fmt.Errorf("error decoding providerConfig: %w", err)
		}
		if providerConfig.LoadBalancer != nil && providerConfig.LoadBalancer.PlanId != nil {
			opts.PlanId = *providerConfig.LoadBalancer.PlanId
		}
	}
	// Default plan if not specified
	if opts.PlanId == "" {
		opts.PlanId = "p10"
	}

	return opts, nil
}

func getInfrastructureStatus(ctx context.Context, c client.Client, cluster *extensionscontroller.Cluster) (*stackitv1alpha1.InfrastructureStatus, error) {
	infra := &extensionsv1alpha1.Infrastructure{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: cluster.ObjectMeta.Name, Name: cluster.Shoot.Name}, infra); err != nil {
		if apierrors.IsNotFound(err) {
			// Infrastructure is reconciled before SelfHostedShootExposure; absence is a normal transient state on initial creation.
			return nil, &reconcilerutils.RequeueAfterError{
				RequeueAfter: 30 * time.Second,
				Cause:        fmt.Errorf("waiting for Infrastructure resource to be created"),
			}
		}
		return nil, fmt.Errorf("error getting infrastructure: %w", err)
	}
	if infra.Status.ProviderStatus == nil {
		// Infrastructure exists but hasn't been reconciled yet — ProviderStatus (and thus the network ID) is not yet populated.
		return nil, &reconcilerutils.RequeueAfterError{
			RequeueAfter: 30 * time.Second,
			Cause:        fmt.Errorf("waiting for Infrastructure status to be populated"),
		}
	}
	return helper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
}
