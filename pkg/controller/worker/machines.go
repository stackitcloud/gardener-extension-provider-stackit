// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	genericworkeractuator "github.com/gardener/gardener/extensions/pkg/controller/worker/genericactuator"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	extensionsv1alpha1helper "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1/helper"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenutils "github.com/gardener/gardener/pkg/utils"
	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/charts"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	stackitutils "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/utils"
)

// MachineClassKind yields the name of the machine class kind used by OpenStack provider.
func (w *workerDelegate) MachineClassKind() string {
	return "MachineClass"
}

// MachineClass yields a newly initialized machine class object.
func (w *workerDelegate) MachineClass() client.Object {
	return &machinev1alpha1.MachineClass{}
}

// MachineClassList yields a newly initialized MachineClassList object.
func (w *workerDelegate) MachineClassList() client.ObjectList {
	return &machinev1alpha1.MachineClassList{}
}

// DeployMachineClasses generates and creates the OpenStack specific machine classes.
func (w *workerDelegate) DeployMachineClasses(ctx context.Context) error {
	if w.machineClasses == nil {
		if err := w.generateMachineConfig(ctx); err != nil {
			return err
		}
	}

	chartPath := "machineclass"
	if feature.UseStackitMachineControllerManager(w.cluster) {
		chartPath = "machineclass-stackit"
	}
	return w.seedChartApplier.ApplyFromEmbeddedFS(ctx, charts.InternalChart, filepath.Join(charts.InternalChartsPath, chartPath), w.worker.Namespace, "machineclass", kubernetes.Values(map[string]any{"machineClasses": w.machineClasses}))
}

// GenerateMachineDeployments generates the configuration for the desired machine deployments.
func (w *workerDelegate) GenerateMachineDeployments(ctx context.Context) (worker.MachineDeployments, error) {
	if w.machineDeployments == nil {
		if err := w.generateMachineConfig(ctx); err != nil {
			return nil, err
		}
	}
	return w.machineDeployments, nil
}

func (w *workerDelegate) generateMachineConfig(ctx context.Context) error {
	var (
		machineDeployments = worker.MachineDeployments{}
		machineClasses     []map[string]any
		machineImages      []stackitv1alpha1.MachineImage
	)

	infrastructureStatus := &stackitv1alpha1.InfrastructureStatus{}
	if _, _, err := w.decoder.Decode(w.worker.Spec.InfrastructureProviderStatus.Raw, nil, infrastructureStatus); err != nil {
		return err
	}

	nodesSecurityGroup, err := helper.FindSecurityGroupByPurpose(infrastructureStatus.SecurityGroups, stackitv1alpha1.PurposeNodes)
	if err != nil {
		return err
	}

	var subnet *stackitv1alpha1.Subnet
	// There is no subnet resource in the IaaS API. The machine-controller-manager-provider-stackit do not require this field.
	if !feature.UseStackitMachineControllerManager(w.cluster) {
		subnet, err = helper.FindSubnetByPurpose(infrastructureStatus.Networks.Subnets, stackitv1alpha1.PurposeNodes)
		if err != nil {
			return err
		}
	}

	for _, pool := range w.worker.Spec.Pools {
		if len(pool.Zones) > math.MaxInt32 {
			return fmt.Errorf("amount of zones exceeded 32bit, overflow")
		}

		// nolint:gosec // check above ensures no overflow can occur
		zoneLen := int32(len(pool.Zones))

		architecture := ptr.Deref(pool.Architecture, v1beta1constants.ArchitectureAMD64)
		machineImage, err := w.findMachineImage(pool.MachineImage.Name, pool.MachineImage.Version, architecture)
		if err != nil {
			return err
		}
		machineImages = appendMachineImage(machineImages, *machineImage)

		var volumeSize int
		if pool.Volume != nil {
			volumeSize, err = worker.DiskSize(pool.Volume.Size)
			if err != nil {
				return err
			}
		}

		workerConfig, err := helper.WorkerConfigFromRawExtension(pool.ProviderConfig)
		if err != nil {
			return err
		}

		workerPoolHash, err := w.generateWorkerPoolHash(pool, workerConfig)
		if err != nil {
			return err
		}

		machineLabels := map[string]string{}
		for _, pair := range workerConfig.MachineLabels {
			machineLabels[pair.Name] = pair.Value
		}

		userData, err := worker.FetchUserData(ctx, w.seedClient, w.worker.Namespace, pool)
		if err != nil {
			return err
		}

		region := w.worker.Spec.Region
		securityGroups := []string{nodesSecurityGroup.Name}
		tags := gardenutils.MergeStringMaps(
			NormalizeLabelsForMachineClass(pool.Labels),
			NormalizeLabelsForMachineClass(machineLabels),
			map[string]string{
				fmt.Sprintf("kubernetes.io-cluster-%s", w.cluster.Shoot.Status.TechnicalID): "1",
				"kubernetes.io-role-node": "1",
			},
		)
		if feature.UseStackitMachineControllerManager(w.cluster) {
			region = stackit.DetermineRegion(w.cluster)
			securityGroups = []string{nodesSecurityGroup.ID}
			tags = map[string]string{
				stackitutils.ClusterLabelKey(w.customLabelDomain): w.cluster.Shoot.Status.TechnicalID,
			}
		}

		for zoneIndex, zone := range pool.Zones {
			zoneIdx := int32(zoneIndex)
			machineClassSpec := map[string]any{
				"region":           region,
				"availabilityZone": zone,
				"machineType":      pool.MachineType,
				"keyName":          infrastructureStatus.Node.KeyName,
				"networkID":        infrastructureStatus.Networks.ID,
				"podNetworkCIDRs":  extensionscontroller.GetPodNetwork(w.cluster),
				"securityGroups":   securityGroups,
				"tags":             tags,
				"credentialsSecretRef": map[string]any{
					"name":      w.worker.Spec.SecretRef.Name,
					"namespace": w.worker.Spec.SecretRef.Namespace,
				},
				"secret": map[string]any{
					"cloudConfig": string(userData),
				},
			}

			if !feature.UseStackitMachineControllerManager(w.cluster) {
				machineClassSpec["subnetID"] = subnet.ID
			}

			if volumeSize > 0 {
				machineClassSpec["rootDiskSize"] = volumeSize
			}

			// specifying the volume type requires a custom volume size to be specified too.
			if pool.Volume != nil && pool.Volume.Type != nil {
				machineClassSpec["rootDiskType"] = *pool.Volume.Type
			}

			if machineImage.ID != "" {
				machineClassSpec["imageID"] = machineImage.ID
			} else {
				machineClassSpec["imageName"] = machineImage.Image
			}

			if workerConfig.NodeTemplate != nil {
				machineClassSpec["nodeTemplate"] = machinev1alpha1.NodeTemplate{
					Capacity:     workerConfig.NodeTemplate.Capacity,
					InstanceType: pool.MachineType,
					Region:       region,
					Zone:         zone,
					Architecture: ptr.To(architecture),
				}
			} else if pool.NodeTemplate != nil {
				machineClassSpec["nodeTemplate"] = machinev1alpha1.NodeTemplate{
					Capacity:     pool.NodeTemplate.Capacity,
					InstanceType: pool.MachineType,
					Region:       region,
					Zone:         zone,
					Architecture: ptr.To(architecture),
				}
			}

			var (
				deploymentName = fmt.Sprintf("%s-%s-z%d", w.cluster.Shoot.Status.TechnicalID, pool.Name, zoneIndex+1)
				className      = fmt.Sprintf("%s-%s", deploymentName, workerPoolHash)
			)

			updateConfiguration := machinev1alpha1.UpdateConfiguration{
				MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(zoneIdx, pool.MaxUnavailable, zoneLen, pool.Minimum)),
				MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(zoneIdx, pool.MaxSurge, zoneLen, pool.Maximum)),
			}

			machineDeploymentStrategy := machinev1alpha1.MachineDeploymentStrategy{
				Type: machinev1alpha1.RollingUpdateMachineDeploymentStrategyType,
				RollingUpdate: &machinev1alpha1.RollingUpdateMachineDeployment{
					UpdateConfiguration: updateConfiguration,
				},
			}

			if gardencorev1beta1helper.IsUpdateStrategyInPlace(pool.UpdateStrategy) {
				machineDeploymentStrategy = machinev1alpha1.MachineDeploymentStrategy{
					Type: machinev1alpha1.InPlaceUpdateMachineDeploymentStrategyType,
					InPlaceUpdate: &machinev1alpha1.InPlaceUpdateMachineDeployment{
						UpdateConfiguration: updateConfiguration,
						OrchestrationType:   machinev1alpha1.OrchestrationTypeAuto,
					},
				}

				if gardencorev1beta1helper.IsUpdateStrategyManualInPlace(pool.UpdateStrategy) {
					machineDeploymentStrategy.InPlaceUpdate.OrchestrationType = machinev1alpha1.OrchestrationTypeManual
				}
			}

			machineDeployments = append(machineDeployments, worker.MachineDeployment{
				Name:                         deploymentName,
				ClassName:                    className,
				SecretName:                   className,
				PoolName:                     pool.Name,
				Minimum:                      worker.DistributeOverZones(zoneIdx, pool.Minimum, zoneLen),
				Maximum:                      worker.DistributeOverZones(zoneIdx, pool.Maximum, zoneLen),
				Strategy:                     machineDeploymentStrategy,
				Priority:                     pool.Priority,
				Labels:                       addTopologyLabel(pool.Labels, zone),
				Annotations:                  pool.Annotations,
				Taints:                       pool.Taints,
				MachineConfiguration:         genericworkeractuator.ReadMachineConfiguration(pool),
				ClusterAutoscalerAnnotations: extensionsv1alpha1helper.GetMachineDeploymentClusterAutoscalerAnnotations(pool.ClusterAutoscaler),
			})

			machineClassSpec["name"] = className
			machineClassSpec["labels"] = map[string]string{
				v1beta1constants.GardenerPurpose: v1beta1constants.GardenPurposeMachineClass,
			}

			if pool.MachineImage.Name != "" && pool.MachineImage.Version != "" {
				machineClassSpec["operatingSystem"] = map[string]any{
					"operatingSystemName":    pool.MachineImage.Name,
					"operatingSystemVersion": strings.ReplaceAll(pool.MachineImage.Version, "+", "_"),
				}
			}

			machineClasses = append(machineClasses, machineClassSpec)
		}
	}

	w.machineDeployments = machineDeployments
	w.machineClasses = machineClasses
	w.machineImages = machineImages

	return nil
}

func (w *workerDelegate) generateWorkerPoolHash(pool extensionsv1alpha1.WorkerPool, workerConfig *stackitv1alpha1.WorkerConfig) (string, error) {
	var additionalHashData []string

	var pairs []string
	for _, pair := range workerConfig.MachineLabels {
		if pair.TriggerRollingOnUpdate {
			pairs = append(pairs, pair.Name+"="+pair.Value)
		}
	}

	if len(pairs) > 0 {
		// include machine labels marked for rolling
		sort.Strings(pairs)
		additionalHashData = append(additionalHashData, pairs...)
	}

	// hash v1 would otherwise hash the ProviderConfig
	pool.ProviderConfig = nil

	// Generate the worker pool hash.
	// since the ProviderConfig is in this provider is always nil, we just add the same additionalHashdata to v2.
	return worker.WorkerPoolHash(pool, w.cluster, additionalHashData, additionalHashData, nil)
}

// NormalizeLabelsForMachineClass because metadata in OpenStack resources do not allow for certain characters that present in k8s labels e.g. "/",
// normalize the label by replacing illegal characters with "-"
func NormalizeLabelsForMachineClass(in map[string]string) map[string]string {
	notAllowedChars := regexp.MustCompile(`[^a-zA-Z0-9-_:. ]`)
	res := make(map[string]string)
	for k, v := range in {
		newKey := notAllowedChars.ReplaceAllLiteralString(k, "-")
		res[newKey] = v
	}
	return res
}

func addTopologyLabel(labels map[string]string, zone string) map[string]string {
	return gardenutils.MergeStringMaps(labels, map[string]string{
		openstack.CSIDiskDriverTopologyKey:    zone,
		openstack.CSISTACKITDriverTopologyKey: zone,
	})
}
