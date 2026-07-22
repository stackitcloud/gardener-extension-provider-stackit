// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
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
	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/charts"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/worker"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
)

var _ = Describe("Machines", func() {
	var (
		ctx = context.Background()

		ctrl         *gomock.Controller
		c            client.Client
		chartApplier *mockkubernetes.MockChartApplier

		workerDelegate genericworkeractuator.WorkerDelegate
		scheme         *runtime.Scheme
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())

		chartApplier = mockkubernetes.NewMockChartApplier(ctrl)

		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(extensionsv1alpha1.AddToScheme(scheme))
		utilruntime.Must(stackitv1alpha1.AddToScheme(scheme))
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

		DescribeTableSubtree("#GenerateMachineDeployments, #DeployMachineClasses", func(isCapabilitiesCloudProfile bool, usesGlobalImageNames bool) {

			var (
				namespace        string
				technicalID      string
				cloudProfileName string

				openstackAuthURL string
				region           string

				machineImageName    string
				machineImageVersion string
				machineImage        string
				machineImageID      string

				archAMD string
				archARM string

				keyName                     string
				machineType, machineTypeArm string
				userData                    []byte
				userDataSecretName          string
				userDataSecretDataKey       string
				nodeAgentSecretName         string
				networkID                   string
				podCIDR                     string
				subnetID                    string
				securityGroupName           string

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

				nodeCapacity           corev1.ResourceList
				nodeTemplatePool1Zone1 machinev1alpha1.NodeTemplate
				nodeTemplatePool2Zone1 machinev1alpha1.NodeTemplate
				nodeTemplatePool3Zone1 machinev1alpha1.NodeTemplate
				nodeTemplatePool1Zone2 machinev1alpha1.NodeTemplate
				nodeTemplatePool2Zone2 machinev1alpha1.NodeTemplate
				nodeTemplatePool3Zone2 machinev1alpha1.NodeTemplate

				machineConfiguration *machinev1alpha1.MachineConfiguration

				workerPoolHash1 string
				workerPoolHash2 string
				workerPoolHash3 string

				shootVersionMajorMinor string
				shootVersion           string
				clusterWithoutImages   *extensionscontroller.Cluster
				cluster                *extensionscontroller.Cluster
				w                      *extensionsv1alpha1.Worker

				emptyClusterAutoscalerAnnotations map[string]string
				capabilitiesAmd, capabilitiesArm  gardencorev1beta1.Capabilities
				capabilityDefinitions             []gardencorev1beta1.CapabilityDefinition
			)

			BeforeEach(func() {
				if isCapabilitiesCloudProfile {
					capabilityDefinitions = []gardencorev1beta1.CapabilityDefinition{
						{Name: "some-capability", Values: []string{"a", "b", "c"}},
						{Name: v1beta1constants.ArchitectureName, Values: []string{v1beta1constants.ArchitectureAMD64, v1beta1constants.ArchitectureARM64}},
					}
					capabilitiesAmd = gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{v1beta1constants.ArchitectureAMD64},
					}
					capabilitiesArm = gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"arm64"},
					}

				}
				if usesGlobalImageNames {
					machineImageID = ""
				} else {
					machineImageID = "my-image-ID"
				}

				namespace = "control-plane-namespace"
				technicalID = "shoot--foobar--openstack"
				cloudProfileName = "openstack"

				region = "eu-de-1"

				openstackAuthURL = "auth-url"

				machineImageName = "my-os"
				machineImageVersion = "123.4.5-foo+bar123"
				machineImage = "my-image-in-glance"

				archAMD = "amd64"
				archARM = "arm64"

				keyName = "key-name"
				machineType = "large"
				machineTypeArm = "large-arm"
				userData = []byte("some-user-data")
				userDataSecretName = "userdata-secret-name"
				userDataSecretDataKey = "userdata-secret-key"
				nodeAgentSecretName = "node-agent-secret-name"
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
				priorityPool2 = new(int32(100))
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
				nodeTemplatePool1Zone1 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       region,
					Zone:         zone1,
					Architecture: &archAMD,
				}
				nodeTemplatePool1Zone2 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       region,
					Zone:         zone2,
					Architecture: &archAMD,
				}

				nodeTemplatePool2Zone1 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       region,
					Zone:         zone1,
					Architecture: &archAMD,
				}
				nodeTemplatePool2Zone2 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineType,
					Region:       region,
					Zone:         zone2,
					Architecture: &archAMD,
				}

				nodeTemplatePool3Zone1 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineTypeArm,
					Region:       region,
					Zone:         zone1,
					Architecture: &archARM,
				}
				nodeTemplatePool3Zone2 = machinev1alpha1.NodeTemplate{
					Capacity:     nodeCapacity,
					InstanceType: machineTypeArm,
					Region:       region,
					Zone:         zone2,
					Architecture: &archARM,
				}

				machineConfiguration = &machinev1alpha1.MachineConfiguration{}

				shootVersionMajorMinor = "1.32"
				shootVersion = shootVersionMajorMinor + ".14"

				cloudProfileConfig := &stackitv1alpha1.CloudProfileConfig{
					TypeMeta: metav1.TypeMeta{
						APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
						Kind:       "CloudProfileConfig",
					},
					KeyStoneURL: openstackAuthURL,
				}
				cloudProfileConfigJSON, _ := json.Marshal(cloudProfileConfig)

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

				machineImages := []stackitv1alpha1.MachineImages{
					{
						Name: machineImageName,
						Versions: []stackitv1alpha1.MachineImageVersion{
							{
								Version: machineImageVersion,
								CapabilityFlavors: []stackitv1alpha1.MachineImageFlavor{
									{
										Capabilities: capabilitiesArm,
										Image:        machineImage,
										Regions: []stackitv1alpha1.RegionIDMapping{
											{
												Name: region,
												ID:   machineImageID,
											},
										},
									},
									{
										Capabilities: capabilitiesAmd,
										Image:        machineImage,
										Regions: []stackitv1alpha1.RegionIDMapping{
											{
												Name: region,
												ID:   machineImageID,
											},
										},
									},
								},
							},
						},
					},
				}

				if !isCapabilitiesCloudProfile {
					machineImages = []stackitv1alpha1.MachineImages{
						{
							Name: machineImageName,
							Versions: []stackitv1alpha1.MachineImageVersion{
								{
									Version: machineImageVersion,
									Image:   machineImage,
									Regions: []stackitv1alpha1.RegionIDMapping{
										{
											Name:         region,
											ID:           machineImageID,
											Architecture: new(archARM),
										},
										{
											Name:         region,
											ID:           machineImageID,
											Architecture: new(archAMD),
										},
									},
								},
							},
						},
					}
				}

				cloudProfileConfig2 := &stackitv1alpha1.CloudProfileConfig{
					TypeMeta: metav1.TypeMeta{
						APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
						Kind:       "CloudProfileConfig",
					},
					KeyStoneURL:   openstackAuthURL,
					MachineImages: machineImages,
				}

				cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig2)
				cluster = &extensionscontroller.Cluster{
					CloudProfile: &gardencorev1beta1.CloudProfile{
						ObjectMeta: metav1.ObjectMeta{
							Name: cloudProfileName,
						},
						Spec: gardencorev1beta1.CloudProfileSpec{
							MachineCapabilities: capabilityDefinitions,
							MachineTypes: []gardencorev1beta1.MachineType{
								{
									Name:         machineType,
									Capabilities: capabilitiesAmd,
								},
								{
									Name:         machineTypeArm,
									Architecture: new(archARM),
									Capabilities: capabilitiesArm,
								},
							},
							ProviderConfig: &runtime.RawExtension{
								Raw: cloudProfileConfigJSON,
							},
						},
					},
					Shoot: clusterWithoutImages.Shoot,
				}

				w = &extensionsv1alpha1.Worker{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "worker",
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
								NodeAgentSecretName: &nodeAgentSecretName,
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
								NodeAgentSecretName: &nodeAgentSecretName,
								Zones: []string{
									zone1,
									zone2,
								},
								UpdateStrategy:    new(gardencorev1beta1.AutoInPlaceUpdate),
								KubernetesVersion: new(shootVersion),
							},
							{
								Name:           namePool3,
								Minimum:        minPool2,
								Maximum:        maxPool2,
								Priority:       priorityPool2,
								MaxSurge:       maxSurgePool2,
								Architecture:   &archARM,
								MaxUnavailable: maxUnavailablePool2,
								MachineType:    machineTypeArm,
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
								NodeAgentSecretName: &nodeAgentSecretName,
								Zones: []string{
									zone1,
									zone2,
								},
								UpdateStrategy:    new(gardencorev1beta1.ManualInPlaceUpdate),
								KubernetesVersion: new(shootVersion),
							},
						},
					},
				}

				workerPoolHash1, _ = worker.WorkerPoolHash(w.Spec.Pools[0], cluster, nil, nil)
				workerPoolHash2, _ = worker.WorkerPoolHash(w.Spec.Pools[1], cluster, nil, nil)
				workerPoolHash3, _ = worker.WorkerPoolHash(w.Spec.Pools[2], cluster, nil, nil)

				fakeScheme := runtime.NewScheme()
				Expect(corev1.AddToScheme(fakeScheme)).To(Succeed())
				Expect(extensionsv1alpha1.AddToScheme(fakeScheme)).To(Succeed())
				c = fakeclient.NewClientBuilder().
					WithScheme(fakeScheme).
					WithObjects(
						w,
						&corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      userDataSecretName,
								Namespace: namespace,
							},
							Data: map[string][]byte{userDataSecretDataKey: userData},
						},
					).
					WithStatusSubresource(&extensionsv1alpha1.Worker{}).
					Build()

				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, clusterWithoutImages, "")
			})

			Describe("machine images", func() {
				var (
					defaultMachineClass map[string]interface{}
					machineDeployments  worker.MachineDeployments
					machineClasses      map[string]interface{}
					workerWithRegion    *extensionsv1alpha1.Worker
					clusterWithRegion   *extensionscontroller.Cluster
				)
				BeforeEach(func() {

					workerWithRegion = w.DeepCopy()
					zone1 = region + "a"
					zone2 = region + "b"
					workerWithRegion.Spec.Region = region
					workerWithRegion.Spec.Pools[0].Zones = []string{zone1, zone2}
					workerWithRegion.Spec.Pools[1].Zones = []string{zone1, zone2}
					workerWithRegion.Spec.Pools[2].Zones = []string{zone1, zone2}

					clusterWithRegion = &extensionscontroller.Cluster{
						CloudProfile: cluster.CloudProfile,
						Shoot:        cluster.Shoot.DeepCopy(),
						Seed:         cluster.Seed,
					}
					clusterWithRegion.Shoot.Spec.Region = region

					defaultMachineClass = map[string]interface{}{
						"region":          region,
						"keyName":         keyName,
						"networkID":       networkID,
						"subnetID":        subnetID,
						"podNetworkCIDRs": []string{podCIDR},
						"securityGroups":  []string{securityGroupName},
						"tags": map[string]string{
							fmt.Sprintf("kubernetes.io-cluster-%s", technicalID): "1",
							"kubernetes.io-role-node":                            "1",
						},
						"secret": map[string]interface{}{
							"cloudConfig": string(userData),
						},
						"operatingSystem": map[string]interface{}{
							"operatingSystemName":    machineImageName,
							"operatingSystemVersion": strings.ReplaceAll(machineImageVersion, "+", "_"),
						},
					}

					if usesGlobalImageNames {
						defaultMachineClass["imageName"] = machineImage
					} else {
						defaultMachineClass["imageID"] = machineImageID
					}

					newNodeTemplatePool1Zone1 := &nodeTemplatePool1Zone1
					newNodeTemplatePool1Zone2 := &nodeTemplatePool1Zone2
					newNodeTemplatePool2Zone1 := &nodeTemplatePool2Zone1
					newNodeTemplatePool2Zone2 := &nodeTemplatePool2Zone2
					newNodeTemplatePool3Zone1 := &nodeTemplatePool3Zone1
					newNodeTemplatePool3Zone2 := &nodeTemplatePool3Zone2

					var (
						machineClassPool1Zone1 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone1)
						machineClassPool1Zone2 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone2)
						machineClassPool2Zone1 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone1)
						machineClassPool2Zone2 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone2)
						machineClassPool3Zone1 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone1)
						machineClassPool3Zone2 = addKeyValueToMap(defaultMachineClass, "availabilityZone", zone2)

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
					machineClassPool1Zone1 = addKeyValueToMap(machineClassPool1Zone1, "machineType", machineType)
					machineClassPool1Zone2 = addKeyValueToMap(machineClassPool1Zone2, "machineType", machineType)
					machineClassPool2Zone1 = addKeyValueToMap(machineClassPool2Zone1, "machineType", machineType)
					machineClassPool2Zone2 = addKeyValueToMap(machineClassPool2Zone2, "machineType", machineType)
					machineClassPool3Zone1 = addKeyValueToMap(machineClassPool3Zone1, "machineType", machineTypeArm)
					machineClassPool3Zone2 = addKeyValueToMap(machineClassPool3Zone2, "machineType", machineTypeArm)

					addNameAndSecretToMachineClass(machineClassPool1Zone1, machineClassWithHashPool1Zone1, w.Spec.SecretRef)
					addNameAndSecretToMachineClass(machineClassPool1Zone2, machineClassWithHashPool1Zone2, w.Spec.SecretRef)
					addNameAndSecretToMachineClass(machineClassPool2Zone1, machineClassWithHashPool2Zone1, w.Spec.SecretRef)
					addNameAndSecretToMachineClass(machineClassPool2Zone2, machineClassWithHashPool2Zone2, w.Spec.SecretRef)
					addNameAndSecretToMachineClass(machineClassPool3Zone1, machineClassWithHashPool3Zone1, w.Spec.SecretRef)
					addNameAndSecretToMachineClass(machineClassPool3Zone2, machineClassWithHashPool3Zone2, w.Spec.SecretRef)

					addNodeTemplateToMachineClass(machineClassPool1Zone1, *newNodeTemplatePool1Zone1)
					addNodeTemplateToMachineClass(machineClassPool1Zone2, *newNodeTemplatePool1Zone2)
					addNodeTemplateToMachineClass(machineClassPool2Zone1, *newNodeTemplatePool2Zone1)
					addNodeTemplateToMachineClass(machineClassPool2Zone2, *newNodeTemplatePool2Zone2)
					addNodeTemplateToMachineClass(machineClassPool3Zone1, *newNodeTemplatePool3Zone1)
					addNodeTemplateToMachineClass(machineClassPool3Zone2, *newNodeTemplatePool3Zone2)

					machineClasses = map[string]interface{}{"machineClasses": []map[string]interface{}{
						machineClassPool1Zone1,
						machineClassPool1Zone2,
						machineClassPool2Zone1,
						machineClassPool2Zone2,
						machineClassPool3Zone1,
						machineClassPool3Zone2,
					}}

					labelsZone1 := map[string]string{openstack.CSIDiskDriverTopologyKey: zone1, openstack.CSISTACKITDriverTopologyKey: zone1}
					labelsZone2 := map[string]string{openstack.CSIDiskDriverTopologyKey: zone2, openstack.CSISTACKITDriverTopologyKey: zone2}
					machineDeployments = worker.MachineDeployments{
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool1, 2, minPool1)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(0, maxSurgePool1, 2, maxPool1)),
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool1, 2, minPool1)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(1, maxSurgePool1, 2, maxPool1)),
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool2, 2, minPool2)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(0, maxSurgePool2, 2, maxPool2)),
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool2, 2, minPool2)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(1, maxSurgePool2, 2, maxPool2)),
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(0, maxUnavailablePool2, 2, minPool2)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(0, maxSurgePool2, 2, maxPool2)),
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
										MaxUnavailable: new(worker.DistributePositiveIntOrPercent(1, maxUnavailablePool2, 2, minPool2)),
										MaxSurge:       new(worker.DistributePositiveIntOrPercent(1, maxSurgePool2, 2, maxPool2)),
									},
								},
							},
							Labels:                       labelsZone2,
							MachineConfiguration:         machineConfiguration,
							ClusterAutoscalerAnnotations: emptyClusterAutoscalerAnnotations,
						},
					}

					workerPoolHash1, _ = worker.WorkerPoolHash(w.Spec.Pools[0], cluster, nil, nil)
					workerPoolHash2, _ = worker.WorkerPoolHash(w.Spec.Pools[1], cluster, nil, nil)
					workerPoolHash3, _ = worker.WorkerPoolHash(w.Spec.Pools[2], cluster, nil, nil)

				})

				It("should return the expected machine deployments for profile image types", func() {
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

					// Test workerDelegate.DeployMachineClasses()
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
					// When using global image names (machineImageID==""), Image field is set;
					// Test WorkerDelegate.UpdateMachineDeployments()
					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()

					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})

				It("should return the expected machine deployments for profile image types with id", func() {
					// setup(region, "", machineImageID, archARM)
					workerDelegate, _ := NewWorkerDelegate(c, scheme, chartApplier, "", workerWithRegion, clusterWithRegion, "")
					clusterWithRegion.Shoot.Spec.Hibernation = &gardencorev1beta1.Hibernation{Enabled: ptr.To(true)}

					// Test workerDelegate.DeployMachineClasses()

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
					// When using global image names (machineImageID==""), Image field is set;
					// Test workerDelegate.GetMachineImages()
					ctx := ctx
					err = workerDelegate.UpdateMachineImagesStatus(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Test workerDelegate.GenerateMachineDeployments()

					result, err := workerDelegate.GenerateMachineDeployments(ctx)
					Expect(err).NotTo(HaveOccurred())
					Expect(result).To(Equal(machineDeployments))
				})

				Context("Machine Labels", func() {
					It("should consider rolling machine labels for the worker pool hash", func() {
						//setup(region, machineImage, "")

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
					MaxNodeProvisionTime:             new(metav1.Duration{Duration: time.Minute}),
					ScaleDownGpuUtilizationThreshold: new("0.4"),
					ScaleDownUnneededTime:            new(metav1.Duration{Duration: 2 * time.Minute}),
					ScaleDownUnreadyTime:             new(metav1.Duration{Duration: 3 * time.Minute}),
					ScaleDownUtilizationThreshold:    new("0.5"),
				}
				w.Spec.Pools[1].ClusterAutoscaler = nil
				workerDelegate, _ = NewWorkerDelegate(c, scheme, chartApplier, "", w, cluster, "")

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
		},
			Entry("with capabilities and using imageIDs", true, false),
			Entry("with capabilities and using ImageNames", true, true),
			Entry("without capabilities and using imageIDs", false, false),
			Entry("without capabilities and using ImageNames", false, true),
		)

		DescribeTable("EnsureUniformMachineImages", func(capabilityDefinitions []gardencorev1beta1.CapabilityDefinition, expectedImages []stackitv1alpha1.MachineImage) {
			machineImages := []stackitv1alpha1.MachineImage{
				// images with capability sets
				{
					Name:    "some-image",
					Version: "1.2.1",
					ID:      "id-for-arm64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"arm64"},
					},
				},
				{
					Name:    "some-image",
					Version: "1.2.2",
					ID:      "id-for-amd64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"amd64"},
					},
				},
				// legacy image entry without capability sets
				{
					Name:         "some-image",
					Version:      "1.2.3",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
				{
					Name:         "some-image",
					Version:      "1.2.2",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
				{
					Name:         "some-image",
					Version:      "1.2.1",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
			}
			actualImages := EnsureUniformMachineImages(machineImages, capabilityDefinitions)
			Expect(actualImages).To(ContainElements(expectedImages))

		},
			Entry("should return images with Architecture", nil, []stackitv1alpha1.MachineImage{
				// images with capability sets
				{
					Name:         "some-image",
					Version:      "1.2.1",
					ID:           "id-for-arm64",
					Architecture: new("arm64"),
				},
				{
					Name:         "some-image",
					Version:      "1.2.2",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
				// legacy image entry without capability sets
				{
					Name:         "some-image",
					Version:      "1.2.3",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
				{
					Name:         "some-image",
					Version:      "1.2.1",
					ID:           "id-for-amd64",
					Architecture: new("amd64"),
				},
			}),
			Entry("should return images with Capabilities", []gardencorev1beta1.CapabilityDefinition{{
				Name:   v1beta1constants.ArchitectureName,
				Values: []string{"amd64", "arm64"},
			}}, []stackitv1alpha1.MachineImage{
				// images with capability sets
				{
					Name:    "some-image",
					Version: "1.2.1",
					ID:      "id-for-arm64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"arm64"},
					},
				},
				{
					Name:    "some-image",
					Version: "1.2.2",
					ID:      "id-for-amd64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"amd64"},
					},
				},
				// legacy image entry without capability sets
				{
					Name:    "some-image",
					Version: "1.2.3",
					ID:      "id-for-amd64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"amd64"},
					}},
				{
					Name:    "some-image",
					Version: "1.2.1",
					ID:      "id-for-amd64",
					Capabilities: gardencorev1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"amd64"},
					},
				},
			}),
		)
	})
})

func encode(obj runtime.Object) []byte {
	data, _ := json.Marshal(obj)
	return data
}

// nolint:unparam
func addKeyValueToMap(def map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(def)+1)

	for k, v := range def {
		out[k] = v
	}

	out[key] = value
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
