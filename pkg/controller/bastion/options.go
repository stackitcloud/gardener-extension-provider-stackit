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

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/utils"
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
	// Machine type and image for the Bastion, determined from CloudProfile (spec.bastion and spec.providerConfig.machineImages).
	MachineType, ImageID string
	// Network and security group used by shoot workers, determined from Infrastructure.status.providerStatus.
	NetworkID, WorkerSecurityGroupID string
}

func (a *Actuator) DetermineOptions(ctx context.Context, bastion *extensionsv1alpha1.Bastion, cluster *extensionscontroller.Cluster, projectID string) (*Options, error) {
	opts := &Options{
		Bastion:      bastion,
		ProjectID:    projectID,
		ResourceName: fmt.Sprintf("%s-bastion-%s", cluster.Shoot.Status.TechnicalID, bastion.Name),
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

	opts.ImageID, err = determineImageID(bastionSpec, cluster)
	if err != nil {
		return nil, err
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

func determineImageID(bastionSpec extensionsbastion.MachineSpec, cluster *extensionscontroller.Cluster) (string, error) {
	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return "", fmt.Errorf("error getting CloudProfileConfig from Cluster: %w", err)
	}

	imageID, err := determineRegionalImageIDForSpec(bastionSpec, cluster.Shoot.Spec.Region, cloudProfileConfig.MachineImages)
	if err != nil {
		return "", fmt.Errorf("error determining image ID for Bastion from CloudProfileConfig: %w", err)
	}

	return imageID, nil
}

func determineRegionalImageIDForSpec(bastionSpec extensionsbastion.MachineSpec, region string, images []stackitv1alpha1.MachineImages) (string, error) {
	imageIndex := slices.IndexFunc(images, func(image stackitv1alpha1.MachineImages) bool {
		return image.Name == bastionSpec.ImageBaseName
	})
	if imageIndex == -1 {
		return "", fmt.Errorf("machine image with name %s not found", bastionSpec.ImageBaseName)
	}

	versions := images[imageIndex].Versions
	versionIndex := slices.IndexFunc(versions, func(version stackitv1alpha1.MachineImageVersion) bool {
		return version.Version == bastionSpec.ImageVersion
	})
	if versionIndex == -1 {
		return "", fmt.Errorf("machine image %s with version %s not found", bastionSpec.ImageBaseName, bastionSpec.ImageVersion)
	}

	regions := versions[versionIndex].Regions
	regionIndex := slices.IndexFunc(regions, func(regionID stackitv1alpha1.RegionIDMapping) bool {
		return regionID.Name == region && *regionID.Architecture == bastionSpec.Architecture
	})
	if regionIndex == -1 {
		return "", fmt.Errorf("machine image %s with version %s for arch %s in region %s not found", bastionSpec.ImageBaseName, bastionSpec.ImageVersion, bastionSpec.Architecture, region)
	}

	return regions[regionIndex].ID, nil
}

func getInfrastructureStatus(ctx context.Context, c client.Client, cluster *extensionscontroller.Cluster) (*stackitv1alpha1.InfrastructureStatus, error) {
	infra := &extensionsv1alpha1.Infrastructure{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: cluster.ObjectMeta.Name, Name: cluster.Shoot.Name}, infra); err != nil {
		return nil, fmt.Errorf("error getting infrastructure: %w", err)
	}

	return helper.InfrastructureStatusFromRaw(infra.Status.ProviderStatus)
}
