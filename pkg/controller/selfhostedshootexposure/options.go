package selfhostedshootexposure

import (
	"context"
	"fmt"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

const (
	ExposureLabelKey = "exposure.stackit.cloud"

	// resourceNameInfix separates the technical ID from the exposure name in the LB resource name.
	resourceNameInfix = "-exp-"
	// resourceNameHashLength is the number of hex chars from SHA-256(exposure.Name) appended when
	// the unabridged name would exceed validation.DNS1123LabelMaxLength. 8 hex chars = 32 bits,
	// plenty of collision resistance for "names sharing a prefix on the same shoot".
	resourceNameHashLength = 8
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
	// PlanID specifies the service plan (size) of the load balancer.
	PlanID string
	// AllowedSourceRanges restricts which source CIDRs may reach the load balancer.
	// Empty means unrestricted.
	AllowedSourceRanges []string
}

func (a *Actuator) DetermineOptions(ctx context.Context, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster, projectID string) (*Options, error) {
	opts := &Options{
		SelfHostedShootExposure: exposure,
		ProjectID:               projectID,
		ResourceName:            buildResourceName(cluster.Shoot.Status.TechnicalID, exposure.Name),
		// STACKIT LB labels do not allow '/' in keys, so we use the flat dot-separated form
		// matching the convention used for other STACKIT LBs (CCM extraLabels in
		// controlplane.valuesprovider, infrastructure cleanup in infraflow/delete.go).
		// TODO: migrate to utils.BuildLabelKey + CustomLabelDomain once the LB API accepts '/'
		// in label keys; this needs to be coordinated across CCM, controlplane and infraflow so
		// the infrastructure cleanup keeps finding all LBs by the same key.
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

	// Decode providerConfig (when present) and apply API defaults.
	providerConfig := &stackitv1alpha1.SelfHostedShootExposureConfig{}
	if exposure.Spec.ProviderConfig != nil {
		if _, _, err := a.Decoder.Decode(exposure.Spec.ProviderConfig.Raw, nil, providerConfig); err != nil {
			return nil, fmt.Errorf("error decoding providerConfig: %w", err)
		}
	} else {
		a.Client.Scheme().Default(providerConfig)
	}

	if errs := validation.ValidateSelfHostedShootExposureConfig(providerConfig, field.NewPath("spec", "providerConfig")); len(errs) > 0 {
		return nil, fmt.Errorf("invalid providerConfig: %w", errs.ToAggregate())
	}

	opts.PlanID = *providerConfig.LoadBalancer.PlanID
	if providerConfig.LoadBalancer.AccessControl != nil {
		opts.AllowedSourceRanges = providerConfig.LoadBalancer.AccessControl.AllowedSourceRanges
	}

	return opts, nil
}

// buildResourceName returns "<technicalID>-exp-<exposureName>" if it fits within
// apivalidation.DNS1123LabelMaxLength, otherwise truncates the exposure name and appends an
// 8-char SHA-256 suffix of the original exposure name to keep the result unique and DNS-compliant.
func buildResourceName(technicalID, exposureName string) string {
	full := technicalID + resourceNameInfix + exposureName
	if len(full) <= utilvalidation.DNS1123LabelMaxLength {
		return full
	}

	hash := gardenerutils.ComputeSHA256Hex([]byte(exposureName))[:resourceNameHashLength]
	budget := max(0, utilvalidation.DNS1123LabelMaxLength-len(technicalID)-len(resourceNameInfix)-resourceNameHashLength-1)
	// strings.TrimRight strips trailing hyphens after truncation to keep the result DNS-1123 compliant.
	truncated := strings.TrimRight(exposureName[:budget], "-")
	return technicalID + resourceNameInfix + truncated + "-" + hash
}

func getInfrastructureStatus(ctx context.Context, c client.Client, cluster *extensionscontroller.Cluster) (*stackitv1alpha1.InfrastructureStatus, error) {
	infra := &extensionsv1alpha1.Infrastructure{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: cluster.ObjectMeta.Name, Name: cluster.Shoot.Name}, infra); err != nil {
		return nil, fmt.Errorf("error getting infrastructure: %w", err)
	}
	return helper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
}
