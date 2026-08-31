// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"context"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	genericworkeractuator "github.com/gardener/gardener/extensions/pkg/controller/worker/genericactuator"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/api/core/v1beta1/helper"
	extensionsv1alpha1helper "github.com/gardener/gardener/pkg/api/extensions/v1alpha1/helper"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
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
	iaas2 "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
)

const (
	shouldMigrateMachineAnnotation = "stackit.cloud/machine-should-be-migrated"
	migratedMachineAnnotation      = "stackit.cloud/migrated-machine"
	workerMigratedAnnotation       = "stackit.cloud/machine-controller-manager-migrated"
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
	err := w.seedChartApplier.ApplyFromEmbeddedFS(ctx, charts.InternalChart, filepath.Join(charts.InternalChartsPath, chartPath), w.worker.Namespace, "machineclass", kubernetes.Values(map[string]any{"machineClasses": w.machineClasses}))
	if err != nil {
		return err
	}

	if feature.MigrateStackitMachineControllerManager(w.cluster) && w.worker.Annotations[workerMigratedAnnotation] != "true" {
		err = w.migrateMachines(ctx)
		if err != nil {
			return err
		}
	}

	return nil
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
		//nolint:staticcheck // SA1019: Will be removed once we drop OpenStack API support
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

		machineTypeFromCloudProfile := gardencorev1beta1helper.FindMachineTypeByName(w.cluster.CloudProfile.Spec.MachineTypes, pool.MachineType)
		if machineTypeFromCloudProfile == nil {
			return fmt.Errorf("machine type %q not found in cloud profile %q", pool.MachineType, w.cluster.CloudProfile.Name)
		}

		capabilityDefinitions := helper.NormalizeCapabilityDefinitions(w.cluster.CloudProfile.Spec.MachineCapabilities)
		architecture := ptr.Deref(pool.Architecture, v1beta1constants.ArchitectureAMD64)
		machineTypeCapabilities := helper.NormalizeMachineTypeCapabilities(machineTypeFromCloudProfile.Capabilities, &architecture, capabilityDefinitions)

		machineImage, err := w.selectMachineImageForWorkerPool(pool.MachineImage.Name, pool.MachineImage.Version, w.worker.Spec.Region, architecture, machineTypeCapabilities, capabilityDefinitions)
		if err != nil {
			return err
		}
		machineImages = EnsureUniformMachineImages(machineImages, w.cluster.CloudProfile.Spec.MachineCapabilities)
		machineImages = appendMachineImage(machineImages, *machineImage, w.cluster.CloudProfile.Spec.MachineCapabilities)

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
			region = stackit.DetermineRegionFromWorkerSpec(w.worker)
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
					Architecture: new(architecture),
				}
			} else if pool.NodeTemplate != nil {
				machineClassSpec["nodeTemplate"] = machinev1alpha1.NodeTemplate{
					Capacity:     pool.NodeTemplate.Capacity,
					InstanceType: pool.MachineType,
					Region:       region,
					Zone:         zone,
					Architecture: new(architecture),
				}
			}

			var (
				deploymentName = fmt.Sprintf("%s-%s-z%d", w.cluster.Shoot.Status.TechnicalID, pool.Name, zoneIndex+1)
				className      = fmt.Sprintf("%s-%s", deploymentName, workerPoolHash)
			)

			updateConfiguration := machinev1alpha1.UpdateConfiguration{
				MaxUnavailable: new(worker.DistributePositiveIntOrPercent(zoneIdx, pool.MaxUnavailable, zoneLen, pool.Minimum)),
				MaxSurge:       new(worker.DistributePositiveIntOrPercent(zoneIdx, pool.MaxSurge, zoneLen, pool.Maximum)),
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

			var preserveMax int32
			if pool.MachineControllerManagerSettings != nil {
				preserveMax = ptr.Deref(pool.MachineControllerManagerSettings.AutoPreserveFailedMachineMax, 0)
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
				AutoPreserveFailedMachineMax: worker.DistributeOverZones(zoneIdx, preserveMax, zoneLen),
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
	w.machineImages = EnsureUniformMachineImages(machineImages, w.cluster.CloudProfile.Spec.MachineCapabilities)

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

	// The provider config is not part of the worker pool hash
	pool.ProviderConfig = nil

	// Generate the worker pool hash.
	// since the ProviderConfig is in this provider is always nil, we add some additionalHashdata
	return worker.WorkerPoolHash(pool, w.cluster, additionalHashData, nil)
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

// EnsureUniformMachineImages ensures that all machine images use the same legacy or capability-based format.
func EnsureUniformMachineImages(images []stackitv1alpha1.MachineImage, definitions []gardencorev1beta1.CapabilityDefinition) []stackitv1alpha1.MachineImage {
	var uniformMachineImages []stackitv1alpha1.MachineImage

	if len(definitions) == 0 {
		for _, img := range images {
			if len(img.Capabilities) == 0 {
				uniformMachineImages = appendMachineImage(uniformMachineImages, img, definitions)
				continue
			}
			var architecture *string
			if len(img.Capabilities[v1beta1constants.ArchitectureName]) > 0 {
				architecture = &img.Capabilities[v1beta1constants.ArchitectureName][0]
			}
			uniformMachineImages = appendMachineImage(uniformMachineImages, stackitv1alpha1.MachineImage{
				Name:         img.Name,
				Version:      img.Version,
				Image:        img.Image,
				ID:           img.ID,
				Architecture: architecture,
			}, definitions)
		}
		return uniformMachineImages
	}

	for _, img := range images {
		if len(img.Capabilities) > 0 {
			uniformMachineImages = appendMachineImage(uniformMachineImages, img, definitions)
			continue
		}

		architecture := ptr.Deref(img.Architecture, v1beta1constants.ArchitectureAMD64)
		uniformMachineImages = appendMachineImage(uniformMachineImages, stackitv1alpha1.MachineImage{
			Name:    img.Name,
			Version: img.Version,
			Image:   img.Image,
			ID:      img.ID,
			Capabilities: gardencorev1beta1.Capabilities{
				v1beta1constants.ArchitectureName: []string{architecture},
			},
		}, definitions)
	}
	return uniformMachineImages
}

func (w *workerDelegate) migrateMachines(ctx context.Context) error {
	var allMachines machinev1alpha1.MachineList
	var migrateMachines []machinev1alpha1.Machine

	err := w.seedClient.List(ctx, &allMachines, &client.ListOptions{Namespace: w.worker.Namespace})
	if err != nil {
		return err
	}

	for i := range allMachines.Items {
		// ignore error as default is false
		migrateAnnotation, _ := strconv.ParseBool(allMachines.Items[i].Annotations[shouldMigrateMachineAnnotation])
		if !strings.HasPrefix(allMachines.Items[i].Spec.ProviderID, "stackit://") || migrateAnnotation {
			migrateMachines = append(migrateMachines, allMachines.Items[i])
		}
	}

	if len(migrateMachines) == 0 {
		// no old openstack machine
		return w.markWorkerAsMigrated(ctx)
	}

	iaas, err := w.stackitClient.IaaS(ctx, w.seedClient, w.worker.Spec.SecretRef)
	if err != nil {
		return err
	}

	for _, m := range migrateMachines {
		patchAnnotations := client.MergeFrom(m.DeepCopy())
		m.Annotations[shouldMigrateMachineAnnotation] = "true"
		m.Annotations[migratedMachineAnnotation] = "true"
		err = w.seedClient.Patch(ctx, &m, patchAnnotations)
		if err != nil {
			return err
		}

		if m.Spec.ProviderID != "" {
			providerIDParts := strings.Split(m.Spec.ProviderID, "/")
			if len(providerIDParts) == 0 {
				return fmt.Errorf("migrateMachines: malformed machine provider ID: %s", m.Spec.ProviderID)
			}
			serverID := providerIDParts[len(providerIDParts)-1]

			patch := client.MergeFrom(m.DeepCopy())
			m.Spec.ProviderID = fmt.Sprintf("stackit://%s/%s", iaas.ProjectID(), serverID)
			err = w.seedClient.Patch(ctx, &m, patch)
			if err != nil {
				return err
			}

			_, err = iaas.UpdateServer(ctx, serverID, iaas2.UpdateServerPayload{
				Labels: map[string]any{
					//	// TODO refine labels
					"mcm.gardener.cloud/machine":      m.Name,
					"mcm.gardener.cloud/machineclass": m.Spec.Class.Name,
					"mcm.gardener.cloud/role":         "node",
				},
			})
			if err != nil {
				return err
			}
		}

		patchRemoveMigrationAnnotation := client.MergeFrom(m.DeepCopy())
		delete(m.Annotations, shouldMigrateMachineAnnotation)
		err = w.seedClient.Patch(ctx, &m, patchRemoveMigrationAnnotation)
		if err != nil {
			return err
		}
	}

	return w.markWorkerAsMigrated(ctx)
}

//func (w *workerDelegate) markWorkerAsMigrated(ctx context.Context) error {
//	patchWorker := client.MergeFrom(w.worker.DeepCopy())
//
//	if w.worker.Annotations == nil {
//		w.worker.Annotations = make(map[string]string)
//	}
//
//	w.worker.Annotations[workerMigratedAnnotation] = "true"
//
//	return w.seedClient.Patch(ctx, w.worker, patchWorker)
//}

// this is a diagnosis code and should not be used in production code.
func (w *workerDelegate) markWorkerAsMigrated(ctx context.Context) error {
	log.Printf(
		"markWorkerAsMigrated: worker=%s namespace=%s resourceVersion=%s annotationsBefore=%v",
		w.worker.Name,
		w.worker.Namespace,
		w.worker.ResourceVersion,
		w.worker.Annotations,
	)

	patchWorker := client.MergeFrom(w.worker.DeepCopy())

	if w.worker.Annotations == nil {
		w.worker.Annotations = make(map[string]string)
	}

	w.worker.Annotations[workerMigratedAnnotation] = "true"

	log.Printf(
		"markWorkerAsMigrated: patching worker=%s annotation=%s value=%s",
		w.worker.Name,
		workerMigratedAnnotation,
		w.worker.Annotations[workerMigratedAnnotation],
	)

	if err := w.seedClient.Patch(ctx, w.worker, patchWorker); err != nil {
		log.Printf(
			"markWorkerAsMigrated: PATCH FAILED worker=%s error=%v",
			w.worker.Name,
			err,
		)
		return fmt.Errorf("patch worker migration annotation: %w", err)
	}

	log.Printf(
		"markWorkerAsMigrated: PATCH SUCCEEDED worker=%s resourceVersion=%s annotations=%v",
		w.worker.Name,
		w.worker.ResourceVersion,
		w.worker.Annotations,
	)

	// Read the Worker again from the API server.
	var worker extensionsv1alpha1.Worker
	if err := w.seedClient.Get(
		ctx,
		client.ObjectKeyFromObject(w.worker),
		&worker,
	); err != nil {
		log.Printf(
			"markWorkerAsMigrated: GET AFTER PATCH FAILED worker=%s error=%v",
			w.worker.Name,
			err,
		)
		return fmt.Errorf("get worker after migration patch: %w", err)
	}

	actualValue := worker.Annotations[workerMigratedAnnotation]

	log.Printf(
		"markWorkerAsMigrated: API SERVER VALUE worker=%s annotation=%s value=%q allAnnotations=%v",
		worker.Name,
		workerMigratedAnnotation,
		actualValue,
		worker.Annotations,
	)

	if actualValue != "true" {
		return fmt.Errorf(
			"worker migration annotation was not persisted: worker=%s value=%q",
			worker.Name,
			actualValue,
		)
	}

	return nil
}
