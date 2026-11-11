package bastion

import (
	"context"
	"fmt"
	"slices"
	"strings"

	extensionsbastion "github.com/gardener/gardener/extensions/pkg/bastion"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/utils"
)

// Options contains all input required for creating a Bastion host for a shoot on STACKIT.
// The options are determined from the Bastion and Cluster object.
type Options struct {
	Bastion *extensionsv1alpha1.Bastion

	// TechnicalID is the shoot's technical ID, as determined from Cluster.status.shoot.status.technicalID.
	TechnicalID string
	// ProjectID is the STACKIT project ID of the shoot. Currently determined from the cloudprovider (credentials) secret.
	ProjectID string
	// ResourceName of all STACKIT resources for this Bastion.
	ResourceName string
	// Labels added to all STACKIT resources.
	Labels map[string]string

	// Region for the Bastion server, determined from Cluster.spec.shoot.spec.region (RegionOne is replaced with eu01).
	Region string
	// AvailabilityZone for the Bastion server, first non-metro zone in CloudProfile.
	AvailabilityZone string
	// RootDiskSize for the bastion server
	RootDiskSize int64
	// Machine type and image for the Bastion, determined from CloudProfile (spec.bastion and spec.providerConfig.machineImages).
	MachineType, ImageID string
	// Network and security group used by shoot workers, determined from Infrastructure.status.providerStatus.
	NetworkID, WorkerSecurityGroupID string
}

const (
	// DefaultRootDiskSize is the default root disk size for the bastion server
	DefaultRootDiskSize = int64(25)
)

func (a *Actuator) DetermineOptions(ctx context.Context, bastion *extensionsv1alpha1.Bastion, cluster *extensionscontroller.Cluster, projectID string) (*Options, error) {
	opts := &Options{
		Bastion:      bastion,
		ProjectID:    projectID,
		ResourceName: stackitclient.BuildResourceName(cluster.Shoot.Status.TechnicalID, "-bastion-", bastion.Name),
		Labels: map[string]string{
			utils.ClusterLabelKey(a.CustomLabelDomain):          cluster.Shoot.Status.TechnicalID,
			utils.BuildLabelKey(a.CustomLabelDomain, "bastion"): bastion.Name,
		},
		Region: stackit.DetermineRegion(cluster),
	}

	var err error
	opts.AvailabilityZone, err = determineAvailabilityZone(cluster)
	if err != nil {
		return nil, fmt.Errorf("error determining availability zone: %w", err)
	}

	bastionSpec, err := extensionsbastion.GetMachineSpecFromCloudProfile(cluster.CloudProfile)
	if err != nil {
		return nil, fmt.Errorf("error getting MachineSpec for Bastion from CloudProfile: %w", err)
	}
	opts.MachineType = bastionSpec.MachineTypeName

	// Fetch provider-specific options from stackitv1alpha1.CloudProfileConfig
	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}

	opts.ImageID, err = determineImageID(bastionSpec, cluster, cloudProfileConfig)
	if err != nil {
		return nil, err
	}

	cpBastionConfig := cloudProfileConfig.Bastion
	if cpBastionConfig != nil && cpBastionConfig.RootDiskSize != nil {
		opts.RootDiskSize = *cpBastionConfig.RootDiskSize
	} else {
		// Set a default in case a custom bastion root disk size is not configured
		opts.RootDiskSize = DefaultRootDiskSize
	}

	infraStatus, err := getInfrastructureStatus(ctx, a.Client, cluster)
	if err != nil {
		return nil, fmt.Errorf("error getting InfrastructureStatus: %w", err)
	}
	opts.NetworkID = infraStatus.Networks.ID

	workerSecurityGroup, err := helper.FindSecurityGroupByPurpose(infraStatus.SecurityGroups, stackitv1alpha1.PurposeNodes)
	if err != nil {
		return nil, fmt.Errorf("error getting worker security group from InfrastructureStatus: %w", err)
	}
	opts.WorkerSecurityGroupID = workerSecurityGroup.ID

	return opts, nil
}

func determineAvailabilityZone(cluster *extensionscontroller.Cluster) (string, error) {
	var region *gardencorev1beta1.Region
	for _, cloudProfileRegion := range cluster.CloudProfile.Spec.Regions {
		if cloudProfileRegion.Name != cluster.Shoot.Spec.Region {
			continue
		}
		region = &cloudProfileRegion
		break
	}

	if region == nil {
		return "", fmt.Errorf("error finding region %q in CloudProfile", cluster.Shoot.Spec.Region)
	}

	// prefer non-metro zones, but fall back to the first zone if all are metro
	nonMetroZones := slices.DeleteFunc(slices.Clone(region.Zones), func(zone gardencorev1beta1.AvailabilityZone) bool {
		return strings.HasSuffix(zone.Name, "-m")
	})
	if len(nonMetroZones) > 0 {
		return nonMetroZones[0].Name, nil
	}
	return region.Zones[0].Name, nil
}

func determineImageID(bastionSpec extensionsbastion.MachineSpec, cluster *extensionscontroller.Cluster, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig) (string, error) {
	capabilityDefinitions := helper.NormalizeCapabilityDefinitions(cluster.CloudProfile.Spec.MachineCapabilities)
	machineCapabilities := helper.NormalizeMachineTypeCapabilities(bastionSpec.MachineTypeCapabilities, &bastionSpec.Architecture, capabilityDefinitions)

	imageFlavor, err := helper.FindImageInCloudProfile(
		cloudProfileConfig,
		bastionSpec.ImageBaseName,
		bastionSpec.ImageVersion,
		cluster.Shoot.Spec.Region,
		machineCapabilities,
		capabilityDefinitions,
	)
	if err != nil {
		return "", fmt.Errorf("error determining image ID for Bastion from CloudProfileConfig: %w", err)
	}

	if len(imageFlavor.Regions) == 0 || imageFlavor.Regions[0].ID == "" {
		return "", fmt.Errorf("error determining image ID for Bastion from CloudProfileConfig: machine image %s with version %s in region %s has no image ID", bastionSpec.ImageBaseName, bastionSpec.ImageVersion, cluster.Shoot.Spec.Region)
	}

	return imageFlavor.Regions[0].ID, nil
}

func getInfrastructureStatus(ctx context.Context, c client.Client, cluster *extensionscontroller.Cluster) (*stackitv1alpha1.InfrastructureStatus, error) {
	infra := &extensionsv1alpha1.Infrastructure{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: cluster.ObjectMeta.Name, Name: cluster.Shoot.Name}, infra); err != nil {
		return nil, fmt.Errorf("error getting infrastructure: %w", err)
	}

	return helper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
}
