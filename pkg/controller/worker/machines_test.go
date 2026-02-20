// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	genericworkeractuator "github.com/gardener/gardener/extensions/pkg/controller/worker/genericactuator"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	mockkubernetes "github.com/gardener/gardener/pkg/client/kubernetes/mock"
	"github.com/gardener/gardener/pkg/utils"
	testutils "github.com/gardener/gardener/pkg/utils/test"
	mockclient "github.com/gardener/gardener/third_party/mock/controller-runtime/client"
	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/charts"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/worker"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack"
)

var _ = Describe("Machines", func() {
	var (
		ctx = context.Background()

		ctrl         *gomock.Controller
		c            *mockclient.MockClient
		statusWriter *mockclient.MockStatusWriter
		chartApplier *mockkubernetes.MockChartApplier

		workerDelegate genericworkeractuator.WorkerDelegate
		scheme         *runtime.Scheme
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())

		c = mockclient.NewMockClient(ctrl)
		statusWriter = mockclient.NewMockStatusWriter(ctrl)
		chartApplier = mockkubernetes.NewMockChartApplier(ctrl)

		scheme = runtime.NewScheme()
		_ = stackitv1alpha1.AddToScheme(scheme)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("workerDelegate", func() {
		BeforeEach(func() {
			workerDelegate, _ = NewWorkerDelegate(nil, scheme, nil, "", nil, nil, "")
		})

		Describe("#TestLabelNormalization", func() {
			It("should return the correct list of labels", func() {
				input := map[string]string{
					"a/b/c":     "value",
					"test/node": "value",
					"node-role": "value",
				}

				output := NormalizeLabelsForMachineClass(input)
				expected := map[string]string{
					"a-b-c":     "value",
					"test-node": "value",
					"node-role": "value",
				}
				Expect(output).To(Equal(expected))
			})
		})

		Describe("#GenerateMachineDeployments, #DeployMachineClasses", func() {
			var (
				namespace        string
				technicalID      string
				cloudProfileName string

				openstackAuthURL string
				region           string
				regionWithImages string

				machineImageName    string
				machineImageVersion string
				machineImage        string
				machineImageID      string

				archAMD string
				archARM string

				keyName               string
				machineType           string
				userData              []byte
				userDataSecretName    string
				userDataSecretDataKey string
				networkID             string
				podCIDR               string
				subnetID              string
				securityGroupName     string

				namePool1           string
				minPool1            int32
				maxPool1            int32
				maxSurgePool1       intstr.IntOrString
				maxUnavailablePool1 intstr.IntOrString

				namePool2           string
				minPool2            int32
				maxPool2            int32
				priorityPool2       *int32
				maxSurgePool2       intstr.IntOrString
				maxUnavailablePool2 intstr.IntOrString

				namePool3 string

				zone1 string
				zone2 string

				nodeCapacity         corev1.ResourceList
				machineConfiguration *machinev1alpha1.MachineConfiguration

				workerPoolHash1 string
				workerPoolHash2 string
				workerPoolHash3 string

				shootVersionMajorMinor string
				shootVersion           string
				cloudProfileConfig     *stackitv1alpha1.CloudProfileConfig
				cloudProfileConfigJSON []byte
				clusterWithoutImages   *extensionscontroller.Cluster
				cluster                *extensionscontroller.Cluster
				w                      *extensionsv1alpha1.Worker

				emptyClusterAutoscalerAnnotations map[string]string
			)

			BeforeEach(func() {
				namespace = "control-plane-namespace"
				technicalID = "shoot--foobar--openstack"
				cloudProfileName = "openstack"

				region = "eu-de-1"
				regionWithImages = "eu-de-2"

				openstackAuthURL = "auth-url"

				machineImageName = "my-os"
				machineImageVersion = "123.4.5-foo+bar123"
				machineImage = "my-image-in-glance"
				machineImageID = "my-image-id"

				archAMD = "amd64"
				archARM = "arm64"

				keyName = "key-name"
				machineType = "large"
				userData = []byte("some-user-data")
				userDataSecretName = "userdata-secret-name"
				userDataSecretDataKey = "userdata-secret-key"
				networkID = "network-id"
				podCIDR = "1.2.3.4/5"
				subnetID = "subnetID"
				securityGroupName = "nodes-sec-group"

				namePool1 = "pool-1"
				minPool1 = 5
				maxPool1 = 10
				maxSurgePool1 = intstr.FromInt32(3)
				maxUnavailablePool1 = intstr.FromInt32(2)

				namePool2 = "pool-2"
				minPool2 = 30
				maxPool2 = 45
				priorityPool2 = ptr.To[int32](100)
				maxSurgePool2 = intstr.FromInt32(10)
				maxUnavailablePool2 = intstr.FromInt32(15)

				namePool3 = "pool-3"

				zone1 = region + "a"
				zone2 = region + "b"

				emptyClusterAutoscalerAnnotations = map[string]string{
					"autoscaler.gardener.cloud/max-node-provision-time":              "",
					"autoscaler.gardener.cloud/scale-down-gpu-utilization-threshold": "",
					"autoscaler.gardener.cloud/scale-down-unneeded-time":             "",
					"autoscaler.gardener.cloud/scale-down-unready-time":              "",
					"autoscaler.gardener.cloud/scale-down-utilization-threshold":     "",
				}

				nodeCapacity = corev1.ResourceList{
					"cpu":    resource.MustParse("8"),
					"gpu":    resource.MustParse("1"),
					"memory": resource.MustParse("128Gi"),
				}

				machineConfiguration = &machinev1alpha1.MachineConfiguration{}

				shootVersionMajorMinor = "1.28"
				shootVersion = shootVersionMajorMinor + ".3"

				cloudProfileConfig = &stackitv1alpha1.CloudProfileConfig{
					TypeMeta: metav1.TypeMeta{
						APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
						Kind:       "CloudProfileConfig",
					},
					KeyStoneURL: openstackAuthURL,
				}
				cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig)

				clusterWithoutImages = &extensionscontroller.Cluster{
					CloudProfile: &gardencorev1beta1.CloudProfile{
						ObjectMeta: metav1.ObjectMeta{
							Name: cloudProfileName,
						},
						Spec: gardencorev1beta1.CloudProfileSpec{
							ProviderConfig: &runtime.RawExtension{
								Raw: cloudProfileConfigJSON,
							},
						},
					},
					Shoot: &gardencorev1beta1.Shoot{
						Spec: gardencorev1beta1.ShootSpec{
							Networking: &gardencorev1beta1.Networking{
								Pods: &podCIDR,
							},
							Kubernetes: gardencorev1beta1.Kubernetes{
								Version: shootVersion,
							},
						},
						Status: gardencorev1beta1.ShootStatus{
							TechnicalID: technicalID,
						},
					},
				}

				cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{
					{
						Name: machineImageName,
						Versions: []stackitv1alpha1.MachineImageVersion{
							{
								Version: machineImageVersion,
								Image:   machineImage,
								Regions: []stackitv1alpha1.RegionIDMapping{
									{
										Name:         regionWithImages,
										ID:           machineImageID,
										Architecture: &archARM,
									},
									{
										Name:         regionWithImages,
										ID:           machineImageID,
										Architecture: &archAMD,
									},
								},
							},
						},
					},
				}
				cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig)
				cluster = &extensionscontroller.Cluster{
					CloudProfile: &gardencorev1beta1.CloudProfile{
						ObjectMeta: metav1.ObjectMeta{
							Name: cloudProfileName,
						},
						Spec: gardencorev1beta1.CloudProfileSpec{
							ProviderConfig: &runtime.RawExtension{
								Raw: cloudProfileConfigJSON,
							},
						},
					},
					Shoot: clusterWithoutImages.Shoot,
				}

				w = &extensionsv1alpha1.Worker{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
					},
					Spec: extensionsv1alpha1.WorkerSpec{
						SecretRef: corev1.SecretReference{
							Name:      "secret",
							Namespace: namespace,
						},
						Region: region,
						InfrastructureProviderStatus: &runtime.RawExtension{
							Raw: encode(&stackitv1alpha1.InfrastructureStatus{
								SecurityGroups: []stackitv1alpha1.SecurityGroup{
									{
										Purpose: stackitv1alpha1.PurposeNodes,
										Name:    securityGroupName,
									},
								},
								Node: stackitv1alpha1.NodeStatus{
									KeyName: keyName,
								},
								Networks: stackitv1alpha1.NetworkStatus{
									ID: networkID,
									Subnets: []stackitv1alpha1.Subnet{
										{
											Purpose: stackitv1alpha1.PurposeNodes,
											ID:      subnetID,
										},
									},
								},
							}),
						},
						Pools: []extensionsv1alpha1.WorkerPool{
							{
								Name:           namePool1,
								Minimum:        minPool1,
								Maximum:        maxPool1,
								MaxSurge:       maxSurgePool1,
								MaxUnavailable: maxUnavailablePool1,
								MachineType:    machineType,
								Architecture:   &archAMD,
								MachineImage: extensionsv1alpha1.MachineImage{
									Name:    machineImageName,
									Version: machineImageVersion,
								},
								NodeTemplate: &extensionsv1alpha1.NodeTemplate{
									Capacity: nodeCapacity,
								},
								UserDataSecretRef: corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: userDataSecretName},
									Key:                  userDataSecretDataKey,
								},
								Zones: []string{
									zone1,
									zone2,
								},
							},
							{
								Name:           namePool2,
								Minimum:        minPool2,
								Maximum:        maxPool2,
								Priority:       priorityPool2,
								MaxSurge:       maxSurgePool2,
								Architecture:   &archAMD,
								MaxUnavailable: maxUnavailablePool2,
								MachineType:    machineType,
								MachineImage: extensionsv1alpha1.MachineImage{
									Name:    machineImageName,
									Version: machineImageVersion,
								},
								NodeTemplate: &extensionsv1alpha1.NodeTemplate{
									Capacity: nodeCapacity,
								},
								UserDataSecretRef: corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: userDataSecretName},
									Key:                  userDataSecretDataKey,
								},
								Zones: []string{
									zone1,
									zone2,
								},
								UpdateStrategy:    ptr.To(gardencorev1beta1.AutoInPlaceUpdate),
								KubernetesVersion: ptr.To(shootVersion),
							},
							{
								Name:           namePool3,
								Minimum:        minPool2,
								Maximum:        maxPool2,
								Priority:       priorityPool2,
								MaxSurge:       maxSurgePool2,
								Architecture:   &archAMD,
								MaxUnavailable: maxUnavailablePool2,
								MachineType:    machineType,
								MachineImage: extensionsv1alpha1.MachineImage{
									Name:    machineImageName,
									Version: machineImageVersion,
								},
								NodeTemplate: &extensionsv1alpha1.NodeTemplate{
									Capacity: nodeCapacity,
								},
								UserDataSecretRef: corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: userDataSecretName},
									Key:                  userDataSecretDataKey,
								},
								Zones: []string{
									zone1,
									zone2,
								},
								UpdateStrategy:    ptr.To(gardencorev1beta1.ManualInPlaceUpdate),
								KubernetesVersion: ptr.To(shootVersion),
							},
						},
					},
				}

				workerPoolHash1, _ = worker.WorkerPoolHash(w.Spec.Pools[0], cluster, nil, nil, nil)
				workerPoolHash2, _ = worker.WorkerPoolHash(w.Spec.Pools[1], cluster, nil, nil, nil)
				workerPoolHash3, _ = worker.WorkerPoolHash(w.Spec.Pools[2], cluster, nil, nil, nil)

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, clusterWithoutImages, "")
			})

			expectedUserDataSecretRefRead := func() {
				c.EXPECT().Get(ctx, client.ObjectKey{Namespace: namespace, Name: userDataSecretName}, gomock.AssignableToTypeOf(&corev1.Secret{})).DoAndReturn(
					func(_ context.Context, _ client.ObjectKey, secret *corev1.Secret, _ ...client.GetOption) error {
						secret.Data = map[string][]byte{userDataSecretDataKey: userData}
						return nil
					},
				).AnyTimes()
			}

			setupMachineTest := func(region, name, imageID, architecture string, useStackitMCM bool, defaultMachineClass *map[string]any, machineDeployments *worker.MachineDeployments, machineClasses *map[string]any, workerWithRegion **extensionsv1alpha1.Worker, clusterWithRegion **extensionscontroller.Cluster) {
				securityGroupID := "sg-12345"
				*workerWithRegion = w.DeepCopy()
				zone1 = region + "a"
				zone2 = region + "b"
				(*workerWithRegion).Spec.Region = region
				(*workerWithRegion).Spec.Pools[0].Architecture = &architecture
				(*workerWithRegion).Spec.Pools[1].Architecture = &architecture
				(*workerWithRegion).Spec.Pools[2].Architecture = &architecture

				(*workerWithRegion).Spec.Pools[0].Zones = []string{zone1, zone2}
				(*workerWithRegion).Spec.Pools[1].Zones = []string{zone1, zone2}
				(*workerWithRegion).Spec.Pools[2].Zones = []string{zone1, zone2}

				// Update infrastructure status to include security group ID (for STACKIT)
				(*workerWithRegion).Spec.InfrastructureProviderStatus = &runtime.RawExtension{
					Raw: encode(&stackitv1alpha1.InfrastructureStatus{
						SecurityGroups: []stackitv1alpha1.SecurityGroup{
							{
								Purpose: stackitv1alpha1.PurposeNodes,
								Name:    securityGroupName,
								ID:      securityGroupID,
							},
						},
						Node: stackitv1alpha1.NodeStatus{
							KeyName: keyName,
						},
						Networks: stackitv1alpha1.NetworkStatus{
							ID: networkID,
							Subnets: []stackitv1alpha1.Subnet{
								{
									Purpose: stackitv1alpha1.PurposeNodes,
									ID:      subnetID,
								},
							},
						},
					}),
				}

				*clusterWithRegion = &extensionscontroller.Cluster{
					CloudProfile: cluster.CloudProfile,
					Shoot:        cluster.Shoot.DeepCopy(),
					Seed:         cluster.Seed,
				}
				(*clusterWithRegion).Shoot.Spec.Region = region

				// For STACKIT, region is determined using DetermineRegion which handles RegionOne -> eu01 mapping
				effectiveRegion := region
				if useStackitMCM && region == "RegionOne" {
					effectiveRegion = "eu01"
				}

				*defaultMachineClass = map[string]any{
					"region":          effectiveRegion,
					"machineType":     machineType,
					"keyName":         keyName,
					"networkID":       networkID,
					"podNetworkCIDRs": []string{podCIDR},
					"secret": map[string]any{
						"cloudConfig": string(userData),
					},
					"operatingSystem": map[string]any{
						"operatingSystemName":    machineImageName,
						"operatingSystemVersion": strings.ReplaceAll(machineImageVersion, "+", "_"),
					},
				}

				// STACKIT-specific vs OpenStack-specific fields
				if useStackitMCM {
					// STACKIT uses security group IDs and simplified tags
					(*defaultMachineClass)["securityGroups"] = []string{securityGroupID}
					(*defaultMachineClass)["tags"] = map[string]string{
						"kubernetes.io/cluster": technicalID,
					}
					// Note: subnetID is NOT included for STACKIT
				} else {
					// OpenStack uses security group names, full tags, and subnetID
					(*defaultMachineClass)["securityGroups"] = []string{securityGroupName}
					(*defaultMachineClass)["tags"] = map[string]string{
						fmt.Sprintf("kubernetes.io-cluster-%s", technicalID): "1",
						"kubernetes.io-role-node":                            "1",
					}
					(*defaultMachineClass)["subnetID"] = subnetID
				}

				if imageID == "" {
					(*defaultMachineClass)["imageName"] = name
				} else {
					(*defaultMachineClass)["imageID"] = imageID
				}

				newNodeTemplateZone1 := machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       effectiveRegion,
					Zone:         zone1,
					Architecture: &architecture,
				}

				newNodeTemplateZone2 := machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       effectiveRegion,
					Zone:         zone2,
					Architecture: &architecture,
				}

				var (
					machineClassPool1Zone1 = useDefaultMachineClass(*defaultMachineClass, zone1)
					machineClassPool1Zone2 = useDefaultMachineClass(*defaultMachineClass, zone2)
					machineClassPool2Zone1 = useDefaultMachineClass(*defaultMachineClass, zone1)
					machineClassPool2Zone2 = useDefaultMachineClass(*defaultMachineClass, zone2)
					machineClassPool3Zone1 = useDefaultMachineClass(*defaultMachineClass, zone1)
					machineClassPool3Zone2 = useDefaultMachineClass(*defaultMachineClass, zone2)

					machineClassNamePool1Zone1 = fmt.Sprintf("%s-%s-z1", technicalID, namePool1)
					machineClassNamePool1Zone2 = fmt.Sprintf("%s-%s-z2", technicalID, namePool1)
					machineClassNamePool2Zone1 = fmt.Sprintf("%s-%s-z1", technicalID, namePool2)
					machineClassNamePool2Zone2 = fmt.Sprintf("%s-%s-z2", technicalID, namePool2)
					machineClassNamePool3Zone1 = fmt.Sprintf("%s-%s-z1", technicalID, namePool3)
					machineClassNamePool3Zone2 = fmt.Sprintf("%s-%s-z2", technicalID, namePool3)

					machineClassWithHashPool1Zone1 = fmt.Sprintf("%s-%s", machineClassNamePool1Zone1, workerPoolHash1)
					machineClassWithHashPool1Zone2 = fmt.Sprintf("%s-%s", machineClassNamePool1Zone2, workerPoolHash1)
					machineClassWithHashPool2Zone1 = fmt.Sprintf("%s-%s", machineClassNamePool2Zone1, workerPoolHash2)
					machineClassWithHashPool2Zone2 = fmt.Sprintf("%s-%s", machineClassNamePool2Zone2, workerPoolHash2)
					machineClassWithHashPool3Zone1 = fmt.Sprintf("%s-%s", machineClassNamePool3Zone1, workerPoolHash3)
					machineClassWithHashPool3Zone2 = fmt.Sprintf("%s-%s", machineClassNamePool3Zone2, workerPoolHash3)
				)

				addNameAndSecretToMachineClass(machineClassPool1Zone1, machineClassWithHashPool1Zone1, w.Spec.SecretRef)
				addNameAndSecretToMachineClass(machineClassPool1Zone2, machineClassWithHashPool1Zone2, w.Spec.SecretRef)
				addNameAndSecretToMachineClass(machineClassPool2Zone1, machineClassWithHashPool2Zone1, w.Spec.SecretRef)
				addNameAndSecretToMachineClass(machineClassPool2Zone2, machineClassWithHashPool2Zone2, w.Spec.SecretRef)
				addNameAndSecretToMachineClass(machineClassPool3Zone1, machineClassWithHashPool3Zone1, w.Spec.SecretRef)
				addNameAndSecretToMachineClass(machineClassPool3Zone2, machineClassWithHashPool3Zone2, w.Spec.SecretRef)

				addNodeTemplateToMachineClass(machineClassPool1Zone1, newNodeTemplateZone1)
				addNodeTemplateToMachineClass(machineClassPool1Zone2, newNodeTemplateZone2)
				addNodeTemplateToMachineClass(machineClassPool2Zone1, newNodeTemplateZone1)
				addNodeTemplateToMachineClass(machineClassPool2Zone2, newNodeTemplateZone2)
				addNodeTemplateToMachineClass(machineClassPool3Zone1, newNodeTemplateZone1)
				addNodeTemplateToMachineClass(machineClassPool3Zone2, newNodeTemplateZone2)

				*machineClasses = map[string]any{"machineClasses": []map[string]any{
					machineClassPool1Zone1,
					machineClassPool1Zone2,
					machineClassPool2Zone1,
					machineClassPool2Zone2,
					machineClassPool3Zone1,
					machineClassPool3Zone2,
				}}

				labelsZone1 := map[string]string{openstack.CSIDiskDriverTopologyKey: zone1, openstack.CSISTACKITDriverTopologyKey: zone1}
				labelsZone2 := map[string]string{openstack.CSIDiskDriverTopologyKey: zone2, openstack.CSISTACKITDriverTopologyKey: zone2}
				*machineDeployments = worker.MachineDeployments{
					{
						Name:       machineClassNamePool1Zone1,
						ClassName:  machineClassWithHashPool1Zone1,
						SecretName: machineClassWithHashPool1Zone1,
						Minimum:    worker.DistributeOverZones(0, minPool1, 2),
						Maximum:    worker.DistributeOverZones(0, maxPool1, 2),
						PoolName:   namePool1,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.RollingUpdateMachineDeploymentStrategyType,
							RollingUpdate: &machinev1alpha1.RollingUpdateMachineDeployment{
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool1, 2, minPool1)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(0, maxSurgePool1, 2, maxPool1)),
								},
							},
						},
						Labels:                       labelsZone1,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
					{
						Name:       machineClassNamePool1Zone2,
						ClassName:  machineClassWithHashPool1Zone2,
						SecretName: machineClassWithHashPool1Zone2,
						Minimum:    worker.DistributeOverZones(1, minPool1, 2),
						Maximum:    worker.DistributeOverZones(1, maxPool1, 2),
						PoolName:   namePool1,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.RollingUpdateMachineDeploymentStrategyType,
							RollingUpdate: &machinev1alpha1.RollingUpdateMachineDeployment{
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool1, 2, minPool1)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(1, maxSurgePool1, 2, maxPool1)),
								},
							},
						},
						Labels:                       labelsZone2,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
					{
						Name:       machineClassNamePool2Zone1,
						ClassName:  machineClassWithHashPool2Zone1,
						SecretName: machineClassWithHashPool2Zone1,
						Minimum:    worker.DistributeOverZones(0, minPool2, 2),
						Maximum:    worker.DistributeOverZones(0, maxPool2, 2),
						Priority:   priorityPool2,
						PoolName:   namePool2,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.InPlaceUpdateMachineDeploymentStrategyType,
							InPlaceUpdate: &machinev1alpha1.InPlaceUpdateMachineDeployment{
								OrchestrationType: machinev1alpha1.OrchestrationTypeAuto,
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool2, 2, minPool2)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(0, maxSurgePool2, 2, maxPool2)),
								},
							},
						},
						Labels:                       labelsZone1,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
					{
						Name:       machineClassNamePool2Zone2,
						ClassName:  machineClassWithHashPool2Zone2,
						SecretName: machineClassWithHashPool2Zone2,
						Minimum:    worker.DistributeOverZones(1, minPool2, 2),
						Maximum:    worker.DistributeOverZones(1, maxPool2, 2),
						Priority:   priorityPool2,
						PoolName:   namePool2,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.InPlaceUpdateMachineDeploymentStrategyType,
							InPlaceUpdate: &machinev1alpha1.InPlaceUpdateMachineDeployment{
								OrchestrationType: machinev1alpha1.OrchestrationTypeAuto,
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool2, 2, minPool2)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(1, maxSurgePool2, 2, maxPool2)),
								},
							},
						},
						Labels:                       labelsZone2,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
					{
						Name:       machineClassNamePool3Zone1,
						ClassName:  machineClassWithHashPool3Zone1,
						SecretName: machineClassWithHashPool3Zone1,
						Minimum:    worker.DistributeOverZones(0, minPool2, 2),
						Maximum:    worker.DistributeOverZones(0, maxPool2, 2),
						Priority:   priorityPool2,
						PoolName:   namePool3,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.InPlaceUpdateMachineDeploymentStrategyType,
							InPlaceUpdate: &machinev1alpha1.InPlaceUpdateMachineDeployment{
								OrchestrationType: machinev1alpha1.OrchestrationTypeManual,
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool2, 2, minPool2)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(0, maxSurgePool2, 2, maxPool2)),
								},
							},
						},
						Labels:                       labelsZone1,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
					{
						Name:       machineClassNamePool3Zone2,
						ClassName:  machineClassWithHashPool3Zone2,
						SecretName: machineClassWithHashPool3Zone2,
						Minimum:    worker.DistributeOverZones(1, minPool2, 2),
						Maximum:    worker.DistributeOverZones(1, maxPool2, 2),
						Priority:   priorityPool2,
						PoolName:   namePool3,
						Strategy: machinev1alpha1.MachineDeploymentStrategy{
							Type: machinev1alpha1.InPlaceUpdateMachineDeploymentStrategyType,
							InPlaceUpdate: &machinev1alpha1.InPlaceUpdateMachineDeployment{
								OrchestrationType: machinev1alpha1.OrchestrationTypeManual,
								UpdateConfiguration: machinev1alpha1.UpdateConfiguration{
									MaxUnavailable: ptr.To(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool2, 2, minPool2)),
									MaxSurge:       ptr.To(worker.DistributePositiveIntOrPercent(1, maxSurgePool2, 2, maxPool2)),
								},
							},
						},
						Labels:                       labelsZone2,
						MachineConfiguration:         machineConfiguration,
						ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
					},
				}
			}

			Describe("machine images", func() {
				var (
					defaultMachineClass map[string]any
					machineDeployments  worker.MachineDeployments
					machineClasses      map[string]any
					workerWithRegion    *extensionsv1alpha1.Worker
					clusterWithRegion   *extensionscontroller.Cluster
				)

				BeforeEach(func() {
					// Disable STACKIT feature flags for OpenStack-only machineclass tests
					DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.UseSTACKITMachineControllerManager, false))
				})

				setup := func(region, name, imageID, architecture string) {
					setupMachineTest(region, name, imageID, architecture, false, &defaultMachineClass, &machineDeployments, &machineClasses, &workerWithRegion, &clusterWithRegion)
				}

				It("should return the expected machine deployments for profile image types", func() {
					setup(region, machineImage, "", archAMD)
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

					// Test workerDelegate.DeployMachineClasses()
					expectedUserDataSecretRefRead()

					chartApplier.
						EXPECT().
						ApplyFromEmbeddedFS(
							ctx,
							charts.InternalChart,
							filepath.Join("internal", "machineclass"),
							namespace,
							"machineclass",
							kubernetes.Values(machineClasses),
						).
						Return(nil)

					err := workerDelegate.DeployMachineClasses(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.UpdateMachineDeployments()

					expectedImages := &stackitv1alpha1.WorkerStatus{
						TypeMeta: metav1.TypeMeta{
							APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
							Kind:       "WorkerStatus",
						},
						MachineImages: []stackitv1alpha1.MachineImage{
							{
								Name:         machineImageName,
								Version:      machineImageVersion,
								Image:        machineImage,
								Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
							},
						},
					}

					workerWithExpectedImages := w.DeepCopy()
					workerWithExpectedImages.Status.ProviderStatus = &runtime.RawExtension{
						Object: expectedImages,
					}

					c.EXPECT().Status().Return(statusWriter)
					statusWriter.EXPECT().Patch(ctx, workerWithExpectedImages, gomock.Any()).Return(nil)

					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()

					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})

				It("should return the expected machine deployments for profile image types with id", func() {
					setup(regionWithImages, "", machineImageID, archARM)
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", workerWithRegion, clusterWithRegion, "")
					clusterWithRegion.Shoot.Spec.Hibernation = &gardencorev1beta1.Hibernation{Enabled: ptr.To(true)}

					// Test workerDelegate.DeployMachineClasses()
					expectedUserDataSecretRefRead()

					chartApplier.
						EXPECT().
						ApplyFromEmbeddedFS(
							ctx,
							charts.InternalChart,
							filepath.Join("internal", "machineclass"),
							namespace,
							"machineclass",
							kubernetes.Values(machineClasses),
						).
						Return(nil)

					err := workerDelegate.DeployMachineClasses(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GetMachineImages()
					expectedImages := &stackitv1alpha1.WorkerStatus{
						TypeMeta: metav1.TypeMeta{
							APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
							Kind:       "WorkerStatus",
						},
						MachineImages: []stackitv1alpha1.MachineImage{
							{
								Name:         machineImageName,
								Version:      machineImageVersion,
								ID:           machineImageID,
								Architecture: ptr.To(v1beta1constants.ArchitectureARM64),
							},
						},
					}

					workerWithExpectedImages := workerWithRegion.DeepCopy()
					workerWithExpectedImages.Status.ProviderStatus = &runtime.RawExtension{
						Object: expectedImages,
					}

					ctx := ctx
					c.EXPECT().Status().Return(statusWriter)
					statusWriter.EXPECT().Patch(ctx, workerWithExpectedImages, gomock.Any()).Return(nil)

					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()

					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})

				Context("Machine Labels", func() {
					It("should consider rolling machine labels for the worker pool hash", func() {
						setup(region, machineImage, "", archAMD)

						applyLabelsAndPolicy := func(labels []stackitv1alpha1.MachineLabel) string {
							w.Spec.Pools[0].Labels = utils.MergeStringMaps(w.Spec.Pools[0].Labels, map[string]string{"k1": "v1"})
							workerConfig := &stackitv1alpha1.WorkerConfig{
								TypeMeta: metav1.TypeMeta{
									Kind:       "WorkerConfig",
									APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
								},
								MachineLabels: labels,
							}

							w.Spec.Pools[0].ProviderConfig = &runtime.RawExtension{
								Raw: encode(workerConfig),
							}
							workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

							expectedUserDataSecretRefRead()

							result, err := workerDelegate.GenerateMachineDeployments(ctx)
							Expect(err).NotTo(HaveOccurred())
							Expect(result[0].Labels).To(HaveKeyWithValue("k1", "v1"))
							return result[0].ClassName
						}

						className0 := applyLabelsAndPolicy(nil)
						className1 := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "foo", Value: "bar"},
						})
						className1b := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "foo", Value: "bar2"},
						})
						className2 := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "foo", Value: "bar"},
							{Name: "vmspec/a", Value: "blabla", TriggerRollingOnUpdate: true},
							{Name: "vmspec/c", Value: "rabarber1", TriggerRollingOnUpdate: true},
						})
						className2b := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "vmspec/c", Value: "rabarber1", TriggerRollingOnUpdate: true},
							{Name: "vmspec/b", Value: "abc"},
							{Name: "vmspec/a", Value: "blabla", TriggerRollingOnUpdate: true},
						})
						className3 := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "foo", Value: "bar"},
							{Name: "vmspec/a", Value: "blabla", TriggerRollingOnUpdate: true},
							{Name: "vmspec/c", Value: "rabarber2", TriggerRollingOnUpdate: true},
						})
						className4 := applyLabelsAndPolicy([]stackitv1alpha1.MachineLabel{
							{Name: "foo", Value: "bar"},
							{Name: "vmspec/a", Value: "blabla", TriggerRollingOnUpdate: true},
							{Name: "vmspec/c", Value: "rabarber2", TriggerRollingOnUpdate: false},
						})

						Expect(className0).To(Equal(className1))
						Expect(className1).To(Equal(className1b))
						Expect(className0).NotTo(Equal(className2))
						Expect(className2).To(Equal(className2b))
						Expect(className0).NotTo(Equal(className3))
						Expect(className2).NotTo(Equal(className3))
						Expect(className3).NotTo(Equal(className4))
					})
				})
			})

			Describe("machine images with STACKIT MCM", func() {
				var (
					defaultMachineClass map[string]any
					machineDeployments  worker.MachineDeployments
					machineClasses      map[string]any
					workerWithRegion    *extensionsv1alpha1.Worker
					clusterWithRegion   *extensionscontroller.Cluster
				)

				setup := func(region, name, imageID, architecture string) {
					setupMachineTest(region, name, imageID, architecture, true, &defaultMachineClass, &machineDeployments, &machineClasses, &workerWithRegion, &clusterWithRegion)
				}

				It("should return the expected machine deployments for STACKIT with profile image types", func() {
					setup(region, machineImage, "", archAMD)
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", workerWithRegion, clusterWithRegion, "kubernetes.io")

					// Test workerDelegate.DeployMachineClasses()
					expectedUserDataSecretRefRead()

					chartApplier.
						EXPECT().
						ApplyFromEmbeddedFS(
							ctx,
							charts.InternalChart,
							filepath.Join("internal", "machineclass-stackit"),
							namespace,
							"machineclass",
							kubernetes.Values(machineClasses),
						).
						Return(nil)

					err := workerDelegate.DeployMachineClasses(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.UpdateMachineImagesStatus()
					expectedImages := &stackitv1alpha1.WorkerStatus{
						TypeMeta: metav1.TypeMeta{
							APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
							Kind:       "WorkerStatus",
						},
						MachineImages: []stackitv1alpha1.MachineImage{
							{
								Name:         machineImageName,
								Version:      machineImageVersion,
								Image:        machineImage,
								Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
							},
						},
					}

					workerWithExpectedImages := workerWithRegion.DeepCopy()
					workerWithExpectedImages.Status.ProviderStatus = &runtime.RawExtension{
						Object: expectedImages,
					}

					c.EXPECT().Status().Return(statusWriter)
					statusWriter.EXPECT().Patch(ctx, workerWithExpectedImages, gomock.Any()).Return(nil)

					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()
					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})

				It("should return the expected machine deployments for STACKIT with profile image types with id", func() {
					setup(regionWithImages, "", machineImageID, archARM)
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", workerWithRegion, clusterWithRegion, "kubernetes.io")
					clusterWithRegion.Shoot.Spec.Hibernation = &gardencorev1beta1.Hibernation{Enabled: ptr.To(true)}

					// Test workerDelegate.DeployMachineClasses()
					expectedUserDataSecretRefRead()

					chartApplier.
						EXPECT().
						ApplyFromEmbeddedFS(
							ctx,
							charts.InternalChart,
							filepath.Join("internal", "machineclass-stackit"),
							namespace,
							"machineclass",
							kubernetes.Values(machineClasses),
						).
						Return(nil)

					err := workerDelegate.DeployMachineClasses(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.UpdateMachineImagesStatus()
					expectedImages := &stackitv1alpha1.WorkerStatus{
						TypeMeta: metav1.TypeMeta{
							APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
							Kind:       "WorkerStatus",
						},
						MachineImages: []stackitv1alpha1.MachineImage{
							{
								Name:         machineImageName,
								Version:      machineImageVersion,
								ID:           machineImageID,
								Architecture: ptr.To(v1beta1constants.ArchitectureARM64),
							},
						},
					}

					workerWithExpectedImages := workerWithRegion.DeepCopy()
					workerWithExpectedImages.Status.ProviderStatus = &runtime.RawExtension{
						Object: expectedImages,
					}

					c.EXPECT().Status().Return(statusWriter)
					statusWriter.EXPECT().Patch(ctx, workerWithExpectedImages, gomock.Any()).Return(nil)

					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()
					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})
			})

			It("should fail because the version is invalid", func() {
				clusterWithoutImages.Shoot.Spec.Kubernetes.Version = "invalid"
				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should fail because the infrastructure status cannot be decoded", func() {
				w.Spec.InfrastructureProviderStatus = &runtime.RawExtension{}

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should fail because the security group cannot be found", func() {
				w.Spec.InfrastructureProviderStatus = &runtime.RawExtension{
					Raw: encode(&stackitv1alpha1.InfrastructureStatus{}),
				}

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should fail because the machine image for this cloud profile cannot be found", func() {
				clusterWithoutImages.CloudProfile.Name = "another-cloud-profile"

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, clusterWithoutImages, "")

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				Expect(err).To(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should set expected machineControllerManager settings on machine deployment", func() {
				testDrainTimeout := metav1.Duration{Duration: 10 * time.Minute}
				testHealthTimeout := metav1.Duration{Duration: 20 * time.Minute}
				testCreationTimeout := metav1.Duration{Duration: 30 * time.Minute}
				testMaxEvictRetries := int32(30)
				testNodeConditions := []string{"ReadonlyFilesystem", "KernelDeadlock", "DiskPressure"}
				w.Spec.Pools[0].MachineControllerManagerSettings = &gardencorev1beta1.MachineControllerManagerSettings{
					MachineDrainTimeout:    &testDrainTimeout,
					MachineCreationTimeout: &testCreationTimeout,
					MachineHealthTimeout:   &testHealthTimeout,
					MaxEvictRetries:        &testMaxEvictRetries,
					NodeConditions:         testNodeConditions,
				}

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

				expectedUserDataSecretRefRead()

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				resultSettings := result[0].MachineConfiguration
				resultNodeConditions := strings.Join(testNodeConditions, ",")

				Expect(err).NotTo(HaveOccurred())
				Expect(resultSettings.MachineDrainTimeout).To(Equal(&testDrainTimeout))
				Expect(resultSettings.MachineCreationTimeout).To(Equal(&testCreationTimeout))
				Expect(resultSettings.MachineHealthTimeout).To(Equal(&testHealthTimeout))
				Expect(resultSettings.MaxEvictRetries).To(Equal(&testMaxEvictRetries))
				Expect(resultSettings.NodeConditions).To(Equal(&resultNodeConditions))
			})

			It("should set expected cluster-autoscaler annotations on the machine deployment", func() {
				w.Spec.Pools[0].ClusterAutoscaler = &extensionsv1alpha1.ClusterAutoscalerOptions{
					MaxNodeProvisionTime:             ptr.To(metav1.Duration{Duration: time.Minute}),
					ScaleDownGpuUtilizationThreshold: ptr.To("0.4"),
					ScaleDownUnneededTime:            ptr.To(metav1.Duration{Duration: 2 * time.Minute}),
					ScaleDownUnreadyTime:             ptr.To(metav1.Duration{Duration: 3 * time.Minute}),
					ScaleDownUtilizationThreshold:    ptr.To("0.5"),
				}
				w.Spec.Pools[1].ClusterAutoscaler = nil
				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

				expectedUserDataSecretRefRead()

				result, err := workerDelegate.GenerateMachineDeployments(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())

				Expect(result[0].ClusterAutoscalerAnnotations).NotTo(BeNil())
				Expect(result[1].ClusterAutoscalerAnnotations).NotTo(BeNil())
				for k, v := range result[2].ClusterAutoscalerAnnotations {
					Expect(v).To(BeEmpty(), "entry for key %v is not empty", k)
				}
				for k, v := range result[3].ClusterAutoscalerAnnotations {
					Expect(v).To(BeEmpty(), "entry for key %v is not empty", k)
				}

				Expect(result[0].ClusterAutoscalerAnnotations[extensionsv1alpha1.MaxNodeProvisionTimeAnnotation]).To(Equal("1m0s"))
				Expect(result[0].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownGpuUtilizationThresholdAnnotation]).To(Equal("0.4"))
				Expect(result[0].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUnneededTimeAnnotation]).To(Equal("2m0s"))
				Expect(result[0].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUnreadyTimeAnnotation]).To(Equal("3m0s"))
				Expect(result[0].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUtilizationThresholdAnnotation]).To(Equal("0.5"))

				Expect(result[1].ClusterAutoscalerAnnotations[extensionsv1alpha1.MaxNodeProvisionTimeAnnotation]).To(Equal("1m0s"))
				Expect(result[1].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownGpuUtilizationThresholdAnnotation]).To(Equal("0.4"))
				Expect(result[1].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUnneededTimeAnnotation]).To(Equal("2m0s"))
				Expect(result[1].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUnreadyTimeAnnotation]).To(Equal("3m0s"))
				Expect(result[1].ClusterAutoscalerAnnotations[extensionsv1alpha1.ScaleDownUtilizationThresholdAnnotation]).To(Equal("0.5"))
			})

			DescribeTable("customLabelDomain in machineclass helm chart",
				func(customDomain string) {
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, customDomain)

					expectedUserDataSecretRefRead()

					chartApplier.
						EXPECT().
						ApplyFromEmbeddedFS(
							ctx,
							charts.InternalChart,
							filepath.Join("internal", "machineclass-stackit"),
							namespace,
							"machineclass",
							gomock.Any(),
						).
						Return(nil)

					err := workerDelegate.DeployMachineClasses(ctx)
					Expect(err).NotTo(HaveOccurred())
				},
				Entry("with default kubernetes.io domain",
					"kubernetes.io",
				),
				Entry("with custom ske.stackit.cloud domain",
					"ske.stackit.cloud",
				),
				Entry("with custom example.com domain",
					"example.com",
				),
				Entry("with empty domain",
					"",
				),
			)
		})
	})
})

func encode(obj runtime.Object) []byte {
	data, _ := json.Marshal(obj)
	return data
}

func useDefaultMachineClass(def map[string]any, value any) map[string]any {
	out := make(map[string]any, len(def)+1)

	for k, v := range def {
		out[k] = v
	}

	out["availabilityZone"] = value
	return out
}

func useDefaultMachineClassWith(def map[string]any, add map[string]any) map[string]any {
	out := make(map[string]any, len(add))

	for k, v := range def {
		out[k] = v
	}

	for k, v := range add {
		out[k] = v
	}

	return out
}

func addNodeTemplateToMachineClass(class map[string]any, nodeTemplate machinev1alpha1.NodeTemplate) {
	class["nodeTemplate"] = nodeTemplate
}

func addNameAndSecretToMachineClass(class map[string]any, name string, credentialsSecretRef corev1.SecretReference) {
	class["name"] = name
	class["labels"] = map[string]string{
		v1beta1constants.GardenerPurpose: v1beta1constants.GardenPurposeMachineClass,
	}
	class["credentialsSecretRef"] = map[string]any{
		"name":      credentialsSecretRef.Name,
		"namespace": credentialsSecretRef.Namespace,
	}
}
