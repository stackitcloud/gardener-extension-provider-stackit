// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/controlplane/genericactuator"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	testutils "github.com/gardener/gardener/pkg/utils/test"
	mockclient "github.com/gardener/gardener/third_party/mock/controller-runtime/client"
	mockmanager "github.com/gardener/gardener/third_party/mock/controller-runtime/manager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

const (
	namespace                        = "test"
	authURL                          = "someurl"
	region                           = "europe"
	technicalID                      = "shoot--dev--test"
	genericTokenKubeconfigSecretName = "generic-token-kubeconfig-92e9ae14"
)

var (
	requestTimeout = &metav1.Duration{
		Duration: func() time.Duration { d, _ := time.ParseDuration("5m"); return d }(),
	}
)

func defaultControlPlane() *extensionsv1alpha1.ControlPlane {
	cpConfig := defaultControlPlaneConfig()
	cp := controlPlane("floating-network-id", cpConfig)
	return cp
}

func defaultControlPlaneWithSTACKIT() *extensionsv1alpha1.ControlPlane {
	cpConfig := defaultControlPlaneConfig()
	cpConfig.Storage.CSI.Name = string(stackitv1alpha1.STACKIT)
	cp := controlPlane("floating-network-id", cpConfig)
	return cp
}

func defaultControlPlaneConfig() *stackitv1alpha1.ControlPlaneConfig {
	cpConfig := &stackitv1alpha1.ControlPlaneConfig{
		CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
			FeatureGates: map[string]bool{
				"SomeKubernetesFeature": true,
			},
		},
		Storage: &stackitv1alpha1.Storage{
			CSI: &stackitv1alpha1.CSI{
				Name: stackitv1alpha1.DefaultCSIName,
			},
		},
	}
	return cpConfig
}

func controlPlane(floatingPoolID string, cfg *stackitv1alpha1.ControlPlaneConfig) *extensionsv1alpha1.ControlPlane {
	return &extensionsv1alpha1.ControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "control-plane",
			Namespace: namespace,
		},
		Spec: extensionsv1alpha1.ControlPlaneSpec{
			SecretRef: corev1.SecretReference{
				Name:      v1beta1constants.SecretNameCloudProvider,
				Namespace: namespace,
			},
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				ProviderConfig: &runtime.RawExtension{
					Raw: encode(cfg),
				},
			},
			Region: region,
			InfrastructureProviderStatus: &runtime.RawExtension{
				Raw: encode(&stackitv1alpha1.InfrastructureStatus{
					Networks: stackitv1alpha1.NetworkStatus{
						Name: technicalID,
						ID:   "network-acbd1234",
						FloatingPool: stackitv1alpha1.FloatingPoolStatus{
							ID: floatingPoolID,
						},
						Router: stackitv1alpha1.RouterStatus{
							ID: "routerID",
						},
						Subnets: []stackitv1alpha1.Subnet{
							{
								ID:      "subnet-acbd1234",
								Purpose: stackitv1alpha1.PurposeNodes,
							},
						},
					},
				}),
			},
		},
	}
}

var _ = Describe("ValuesProvider", func() {
	format.MaxLength = 8000
	var (
		ctx = context.TODO()

		ctrl *gomock.Controller

		scheme = runtime.NewScheme()
		_      = stackitv1alpha1.AddToScheme(scheme)

		fakeClient         client.Client
		fakeSecretsManager secretsmanager.Interface

		vp  genericactuator.ValuesProvider
		c   *mockclient.MockClient
		mgr *mockmanager.MockManager

		cp = defaultControlPlane()

		cidr                             = "10.250.0.0/19"
		rescanBlockStorageOnResize       = true
		ignoreVolumeAZ                   = true
		nodeVoluemAttachLimit      int32 = 25

		cloudProfileConfig = &stackitv1alpha1.CloudProfileConfig{
			KeyStoneURL:                authURL,
			RequestTimeout:             requestTimeout,
			RescanBlockStorageOnResize: ptr.To(rescanBlockStorageOnResize),
			IgnoreVolumeAZ:             ptr.To(ignoreVolumeAZ),
			NodeVolumeAttachLimit:      ptr.To[int32](nodeVoluemAttachLimit),
		}
		cloudProfileConfigJSON, _ = json.Marshal(cloudProfileConfig)

		cluster = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"generic-token-kubeconfig.secret.gardener.cloud/name": genericTokenKubeconfigSecretName,
				},
			},
			CloudProfile: &gardencorev1beta1.CloudProfile{
				Spec: gardencorev1beta1.CloudProfileSpec{
					ProviderConfig: &runtime.RawExtension{
						Raw: cloudProfileConfigJSON,
					},
				},
			},
			Seed: &gardencorev1beta1.Seed{},
			Shoot: &gardencorev1beta1.Shoot{
				Spec: gardencorev1beta1.ShootSpec{
					Region: "RegionOne",
					Networking: &gardencorev1beta1.Networking{
						Pods: &cidr,
					},
					Kubernetes: gardencorev1beta1.Kubernetes{
						Version: "1.29.0",
						VerticalPodAutoscaler: &gardencorev1beta1.VerticalPodAutoscaler{
							Enabled: true,
						},
					},
					Provider: gardencorev1beta1.Provider{
						InfrastructureConfig: &runtime.RawExtension{
							Raw: encode(&stackitv1alpha1.InfrastructureConfig{
								TypeMeta: metav1.TypeMeta{
									APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
									Kind:       "InfrastructureConfig",
								},
								Networks: stackitv1alpha1.Networks{
									Workers: "10.200.0.0/19",
								},
							}),
						},
						Workers: []gardencorev1beta1.Worker{
							{
								Name:  "worker",
								Zones: []string{"zone2", "zone1"},
							},
						},
					},
				},
				Status: gardencorev1beta1.ShootStatus{
					TechnicalID: technicalID,
				},
			},
		}
		clusterNoOverlay = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"generic-token-kubeconfig.secret.gardener.cloud/name": genericTokenKubeconfigSecretName,
				},
			},
			CloudProfile: &gardencorev1beta1.CloudProfile{
				Spec: gardencorev1beta1.CloudProfileSpec{
					ProviderConfig: &runtime.RawExtension{
						Raw: cloudProfileConfigJSON,
					},
				},
			},
			Shoot: &gardencorev1beta1.Shoot{
				Spec: gardencorev1beta1.ShootSpec{
					Networking: &gardencorev1beta1.Networking{
						Type: ptr.To("calico"),
						ProviderConfig: &runtime.RawExtension{
							Raw: []byte(`{"overlay":{"enabled": false}}`),
						},
						Pods: &cidr,
					},
					Kubernetes: gardencorev1beta1.Kubernetes{
						Version: "1.31.1",
						VerticalPodAutoscaler: &gardencorev1beta1.VerticalPodAutoscaler{
							Enabled: true,
						},
					},
				},
				Status: gardencorev1beta1.ShootStatus{
					TechnicalID: technicalID,
				},
			},
		}

		domainName  = "domain-name"
		tenantName  = "tenant-name"
		cpSecretKey = client.ObjectKey{Namespace: namespace, Name: v1beta1constants.SecretNameCloudProvider}
		cpSecret    = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      v1beta1constants.SecretNameCloudProvider,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"domainName":      []byte(domainName),
				"tenantName":      []byte(tenantName),
				"username":        []byte(`username`),
				"password":        []byte(`password`),
				"authURL":         []byte(authURL),
				stackit.ProjectID: []byte("foo"),
				stackit.SaKeyJSON: []byte("{}"),
			},
		}

		cpConfigKey = client.ObjectKey{Namespace: namespace, Name: openstack.CloudProviderConfigName}
		cpConfig    = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      openstack.CloudProviderConfigName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				openstack.CloudProviderConfigDataKey: []byte("some data"),
			},
		}

		cloudProviderDiskConfig = []byte("foo")
		cpCSIDiskConfigKey      = client.ObjectKey{Namespace: namespace, Name: openstack.CloudProviderCSIDiskConfigName}
		cpCSIDiskConfig         = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      openstack.CloudProviderCSIDiskConfigName,
				Namespace: namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				openstack.CloudProviderConfigDataKey: cloudProviderDiskConfig,
			},
		}

		checksums = map[string]string{
			v1beta1constants.SecretNameCloudProvider: "8bafb35ff1ac60275d62e1cbd495aceb511fb354f74a20f7d06ecb48b3a68432",
			openstack.CloudProviderConfigName:        "bf19236c3ff3be18cf28cb4f58532bda4fd944857dd163baa05d23f952550392",
			openstack.CloudProviderCSIDiskConfigName: "77627eb2343b9f2dc2fca3cce35f2f9eec55783aa5f7dac21c473019e5825de2",
		}

		enabledTrue  = map[string]any{"enabled": true}
		enabledFalse = map[string]any{"enabled": false}
		empty        = func() map[string]any { return nil } // no idea why this is required, apparently there is a difference between and empty map and just `nil`...
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())

		c = mockclient.NewMockClient(ctrl)

		fakeClient = fakeclient.NewClientBuilder().Build()
		fakeSecretsManager = fakesecretsmanager.New(fakeClient, namespace)

		mgr = mockmanager.NewMockManager(ctrl)
		mgr.EXPECT().GetClient().Return(c)
		mgr.EXPECT().GetScheme().Return(scheme)
		vp = NewValuesProvider(mgr, true, "kubernetes.io")
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#GetConfigChartValues", func() {
		configChartValues := map[string]any{
			"domainName":                  "domain-name",
			"tenantName":                  "tenant-name",
			"username":                    "username",
			"password":                    "password",
			"region":                      region,
			"insecure":                    false,
			"authUrl":                     authURL,
			"requestTimeout":              requestTimeout,
			"ignoreVolumeAZ":              ignoreVolumeAZ,
			"applicationCredentialID":     "",
			"applicationCredentialSecret": "",
			"applicationCredentialName":   "",
			"internalNetworkName":         technicalID,
		}

		BeforeEach(func() {
			// Disable STACKIT feature flags for OpenStack-only tests
			DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.UseSTACKITAPIInfrastructureController, false))
			DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.UseSTACKITMachineControllerManager, false))
		})

		It("should return correct config chart values", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(configChartValues))
		})

		It("should return correct config chart values with application credentials", func() {
			secret2 := *cpSecret
			secret2.Data = map[string][]byte{
				"domainName":                  []byte(domainName),
				"tenantName":                  []byte(tenantName),
				"applicationCredentialID":     []byte(`app-id`),
				"applicationCredentialSecret": []byte(`app-secret`),
				"authURL":                     []byte(authURL),
			}

			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(&secret2))

			expectedValues := utils.MergeMaps(configChartValues, map[string]any{
				"username":                    "",
				"password":                    "",
				"applicationCredentialID":     "app-id",
				"applicationCredentialSecret": "app-secret",
				"insecure":                    false,
			})
			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should configure cloud routes when not using overlay", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))
			expectedValues := utils.MergeMaps(configChartValues, map[string]any{
				"routerID": "routerID",
			})
			values, err := vp.GetConfigChartValues(ctx, cp, clusterNoOverlay)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})

		It("should not configure cloud routes when not using overlay but using BGP as backend", func() {
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret))
			net := &gardencorev1beta1.Networking{
				Type: ptr.To("calico"),
				ProviderConfig: &runtime.RawExtension{
					Raw: []byte(`{"backend":"bird","overlay":{"enabled": false}}`),
				},
				Pods: &cidr,
			}
			clusterNoOverlay.Shoot.Spec.Networking = net
			values, err := vp.GetConfigChartValues(ctx, cp, clusterNoOverlay)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(configChartValues))
		})

		It("should return correct config chart values with KeyStone CA Cert", func() {
			secret2 := cpSecret.DeepCopy()
			caCert := "custom-cert"
			secret2.Data["caCert"] = []byte(caCert)
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(secret2))
			expectedValues := utils.MergeMaps(configChartValues, map[string]any{
				"caCert": caCert,
			})
			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedValues))
		})
	})

	Describe("#GetControlPlaneChartValues", func() {
		ccmChartValues := utils.MergeMaps(enabledFalse, map[string]any{
			"replicas":          1,
			"kubernetesVersion": "1.29.0",
			"technicalID":       technicalID,
			"podNetwork":        cidr,
			"podAnnotations": map[string]any{
				"checksum/secret-" + v1beta1constants.SecretNameCloudProvider: checksums[v1beta1constants.SecretNameCloudProvider],
				"checksum/secret-" + openstack.CloudProviderConfigName:        checksums[openstack.CloudProviderConfigName],
			},
			"podLabels": map[string]any{
				"maintenance.gardener.cloud/restart": "true",
			},
			"featureGates": map[string]bool{
				"SomeKubernetesFeature": true,
			},
			"tlsCipherSuites": []string{
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				"TLS_AES_128_GCM_SHA256",
				"TLS_AES_256_GCM_SHA384",
				"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
				"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
				"TLS_CHACHA20_POLY1305_SHA256",
				"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
				"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
			},
			"secrets": map[string]any{
				"server": "cloud-controller-manager-server",
			},
		})

		stackitCcmChartValues := utils.MergeMaps(enabledTrue, map[string]any{
			"replicas": 1,
			"config": map[string]any{
				"stackitProjectID": "foo",
				"stackitNetworkID": "network-acbd1234",
				"stackitRegion":    "eu01",
				"extraLabels": map[string]string{
					STACKITLBClusterLabelKey: "shoot--dev--test",
					// Disabled as the load balancer API is currently not accepting `/` in the label
					// TODO: enable this as soon as the load balancer API supports this
					// "kubernetes.io/cluster":  "shoot--dev--test",
				},
				"customLabelDomain": "kubernetes.io",
			},
			"technicalID": technicalID,
			"podAnnotations": map[string]any{
				"checksum/secret-" + v1beta1constants.SecretNameCloudProvider: checksums[v1beta1constants.SecretNameCloudProvider],
				"checksum/config-" + openstack.STACKITCloudControllerManagerImageName: utils.ComputeChecksum(map[string]any{
					"stackitProjectID": "foo",
					"stackitNetworkID": "network-acbd1234",
					"stackitRegion":    "eu01",
					"extraLabels": map[string]string{
						STACKITLBClusterLabelKey: "shoot--dev--test",
						// Disabled as the load balancer API is currently not accepting `/` in the label
						// TODO: enable this as soon as the load balancer API supports this
						// "kubernetes.io/cluster":  "shoot--dev--test",
					},
					"customLabelDomain": "kubernetes.io",
				}),
			},
			"podLabels": map[string]any{
				"maintenance.gardener.cloud/restart": "true",
			},
			"featureGates": map[string]bool{
				"SomeKubernetesFeature": true,
			},
			"controllers": []string{"*"},
		})

		stackitAlbChartValues := utils.MergeMaps(enabledTrue, map[string]any{
			"replicas": 1,
			"config": map[string]any{
				"stackitProjectID": "foo",
				"stackitNetworkID": "network-acbd1234",
				"region":           "eu01",
			},
		})

		BeforeEach(func() {
			c.EXPECT().Get(ctx, cpConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpConfig))
			c.EXPECT().Delete(context.TODO(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-kube-apiserver-to-csi-snapshot-validation", Namespace: cp.Namespace}})

			// TODO: Remove once cleanup is completed (aka. next release)
			c.EXPECT().Delete(context.TODO(), &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-webhook-vpa", Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-validation", Namespace: namespace}})
			// STACKIT CSI Cleanup
			c.EXPECT().Delete(context.TODO(), &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CSISnapshotValidationName), Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CSISnapshotValidationName), Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-csi-snapshot-webhook-vpa", CSIStackitPrefix), Namespace: namespace}})
			c.EXPECT().Delete(context.TODO(), &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CSISnapshotValidationName), Namespace: namespace}})
			// STACKIT Cloud Provider Config Cleanup
			c.EXPECT().Delete(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CloudProviderConfigName), Namespace: namespace}})

			By("creating secrets managed outside of this package for whose secretsmanager.Get() will be called")
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca-provider-openstack-controlplane", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cloud-controller-manager-server", Namespace: namespace}})).To(Succeed())

			// This call is made for emergency Loadbalancer API access.
			// It will return a NotFound error by default to not interfere with existing tests.
			// Returning this error effectively disables the emergency access feature.
			c.EXPECT().Get(ctx, types.NamespacedName{Name: LoadBalancerEmergencyAccessSecretName, Namespace: namespace}, &corev1.Secret{}).Return(
				errors.NewNotFound(schema.GroupResource{Resource: "secret"}, LoadBalancerEmergencyAccessSecretName))
		})

		It("should return correct control plane chart values", func() {
			c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, fakeSecretsManager, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			val := values[openstack.CloudControllerManagerName]
			Expect(val).To(HaveKeyWithValue("enabled", false))
			Expect(values).To(BeComparableTo(map[string]any{
				"global": map[string]any{
					"genericTokenKubeconfigSecretName": genericTokenKubeconfigSecretName,
				},
				openstack.CSIControllerName: enabledFalse,
				openstack.CloudControllerManagerName: utils.MergeMaps(ccmChartValues, map[string]any{
					"userAgentHeaders":  []string{domainName, tenantName, technicalID},
					"kubernetesVersion": cluster.Shoot.Spec.Kubernetes.Version,
				}),
				openstack.STACKITCloudControllerManagerName: utils.MergeMaps(stackitCcmChartValues, map[string]any{
					"config": map[string]any{"stackitProjectID": string(cpSecret.Data[stackit.ProjectID])},
				}),
				openstack.CSISTACKITControllerName: utils.MergeMaps(enabledTrue, map[string]any{
					"replicas": 1,
					"podAnnotations": map[string]any{
						"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
					},
					"projectID":         string(cpSecret.Data[stackit.ProjectID]),
					"region":            "eu01",
					"stackitEndpoints":  map[string]string{},
					"userAgentHeaders":  []string{domainName, tenantName, technicalID},
					"customLabelDomain": "kubernetes.io",
					"csiSnapshotController": map[string]any{
						"replicas": 1,
					},
				}),
				openstack.STACKITALBControllerManagerName: empty(),
			}))
		})

		It("should return correct control plane chart values", func() {
			c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

			controlPlaneConfig := defaultControlPlaneConfig()
			controlPlaneConfig.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{
					Name: string(stackitv1alpha1.OPENSTACK),
				},
			}
			cp.Spec.ProviderConfig.Raw = encode(controlPlaneConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, fakeSecretsManager, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(BeComparableTo(map[string]any{
				"global": map[string]any{
					"genericTokenKubeconfigSecretName": genericTokenKubeconfigSecretName,
				},
				openstack.CSISTACKITControllerName: enabledFalse,
				openstack.CloudControllerManagerName: utils.MergeMaps(ccmChartValues, map[string]any{
					"enabled":           false,
					"userAgentHeaders":  []string{domainName, tenantName, technicalID},
					"kubernetesVersion": cluster.Shoot.Spec.Kubernetes.Version,
				}),
				openstack.STACKITCloudControllerManagerName: utils.MergeMaps(stackitCcmChartValues, map[string]any{
					"config": map[string]any{"stackitProjectID": string(cpSecret.Data[stackit.ProjectID])},
				}),
				openstack.CSIControllerName: utils.MergeMaps(enabledTrue, map[string]any{
					"replicas":          1,
					"maxEntries":        1000,
					"kubernetesVersion": cluster.Shoot.Spec.Kubernetes.Version,
					"podAnnotations": map[string]any{
						"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
					},
					"userAgentHeaders": []string{domainName, tenantName, technicalID},
					"csiSnapshotController": map[string]any{
						"replicas": 1,
					},
				}),
				openstack.STACKITALBControllerManagerName: empty(),
			}))
		})

		It("should return correct control plane chart values", func() {
			c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

			controlPlaneConfig := defaultControlPlaneConfig()
			controlPlaneConfig.CloudControllerManager = &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.OPENSTACK),
			}
			cp.Spec.ProviderConfig.Raw = encode(controlPlaneConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, fakeSecretsManager, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values[openstack.STACKITCloudControllerManagerName]).To(HaveKeyWithValue("enabled", true))
			Expect(values[openstack.STACKITCloudControllerManagerName]).To(HaveKeyWithValue("controllers", []string{STACKITCCMServiceLoadbalancerController}))
			Expect(values[openstack.CSIControllerName]).To(HaveKeyWithValue("enabled", false))
		})

		DescribeTable("stackit ccm config ",
			func(apiEndpoints *stackitv1alpha1.APIEndpoints, cpConfig *stackitv1alpha1.ControlPlaneConfig, expectedConfig map[string]any, expectedControllers []string) {
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

				controlPlaneConfig := defaultControlPlaneConfig()
				if cpConfig != nil {
					controlPlaneConfig = cpConfig
				}
				cp.Spec.ProviderConfig.Raw = encode(controlPlaneConfig)

				// Create CloudProfileConfig with custom API endpoints
				testCloudProfileConfig := &stackitv1alpha1.CloudProfileConfig{
					APIEndpoints: apiEndpoints,
				}
				testCloudProfileConfigJSON, _ := json.Marshal(testCloudProfileConfig)

				testCluster := *cluster

				cloudProfile := &gardencorev1beta1.CloudProfile{}
				if cluster.CloudProfile != nil {
					*cloudProfile = *cluster.CloudProfile
				}
				cloudProfile.Spec.ProviderConfig = &runtime.RawExtension{
					Raw: testCloudProfileConfigJSON,
				}

				testCluster.CloudProfile = cloudProfile

				mgr.EXPECT().GetClient().Return(c)
				mgr.EXPECT().GetScheme().Return(scheme)

				if expectedControllers == nil {
					stackitCCMDeletion(ctx, c)
				}

				vpStackitConf := NewValuesProvider(mgr, true, "kubernetes.io")
				values, err := vpStackitConf.GetControlPlaneChartValues(ctx, cp, &testCluster, fakeSecretsManager, checksums, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(HaveKey(openstack.STACKITCloudControllerManagerName))

				if expectedConfig != nil {
					Expect(values[openstack.STACKITCloudControllerManagerName]).To(HaveKeyWithValue("config", expectedConfig))
				} else {
					Expect(values[openstack.STACKITCloudControllerManagerName]).To(BeNil())
				}

				if expectedControllers != nil {
					Expect(values[openstack.STACKITCloudControllerManagerName]).To(HaveKeyWithValue("controllers", expectedControllers))
				} else {
					Expect(values[openstack.STACKITCloudControllerManagerName]).To(BeNil())
				}
			},
			Entry("default",
				nil,
				nil,
				map[string]any{
					"stackitProjectID": string(cpSecret.Data[stackit.ProjectID]),
					"stackitNetworkID": "network-acbd1234",
					"stackitRegion":    "eu01",
					"extraLabels": map[string]string{
						STACKITLBClusterLabelKey: "shoot--dev--test",
						// Disabled as the load balancer API is currently not accepting `/` in the label
						// TODO: enable this as soon as the load balancer API supports this
						// "kubernetes.io/cluster":  "shoot--dev--test",
					},
					"customLabelDomain": "kubernetes.io",
				},
				[]string{"*"},
			),
			Entry("custom LoadBalancer endpoint",
				&stackitv1alpha1.APIEndpoints{
					LoadBalancer: ptr.To("https://custom-lb.stackit.cloud"),
				},
				nil,
				map[string]any{
					"stackitProjectID": string(cpSecret.Data[stackit.ProjectID]),
					"stackitNetworkID": "network-acbd1234",
					"stackitRegion":    "eu01",
					"extraLabels": map[string]string{
						STACKITLBClusterLabelKey: "shoot--dev--test",
						// Disabled as the load balancer API is currently not accepting `/` in the label
						// TODO: enable this as soon as the load balancer API supports this
						// "kubernetes.io/cluster":  "shoot--dev--test",
					},
					"loadBalancerApiUrl": "https://custom-lb.stackit.cloud",
					"customLabelDomain":  "kubernetes.io",
				},
				[]string{"*"},
			),
			Entry("custom LoadBalancer and Token endpoints",
				&stackitv1alpha1.APIEndpoints{
					LoadBalancer:  ptr.To("https://custom-lb.stackit.cloud"),
					TokenEndpoint: ptr.To("https://custom-auth.stackit.cloud/token"),
				},
				nil,
				map[string]any{
					"stackitProjectID": string(cpSecret.Data[stackit.ProjectID]),
					"stackitNetworkID": "network-acbd1234",
					"stackitRegion":    "eu01",
					"extraLabels": map[string]string{
						STACKITLBClusterLabelKey: "shoot--dev--test",
						// Disabled as the load balancer API is currently not accepting `/` in the label
						// TODO: enable this as soon as the load balancer API supports this
						// "kubernetes.io/cluster":  "shoot--dev--test",
					},
					"loadBalancerApiUrl": "https://custom-lb.stackit.cloud",
					"tokenUrl":           "https://custom-auth.stackit.cloud/token",
					"customLabelDomain":  "kubernetes.io",
				},
				[]string{"*"},
			),
			Entry("Cloudprofile has OpenStack CCM configured",
				nil,
				&stackitv1alpha1.ControlPlaneConfig{
					CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
						Name: string(stackitv1alpha1.OPENSTACK),
					},
				},
				map[string]any{
					"stackitProjectID": string(cpSecret.Data[stackit.ProjectID]),
					"stackitNetworkID": "network-acbd1234",
					"stackitRegion":    "eu01",
					"extraLabels": map[string]string{
						STACKITLBClusterLabelKey: "shoot--dev--test",
						// Disabled as the load balancer API is currently not accepting `/` in the label
						// TODO: enable this as soon as the load balancer API supports this
						// "kubernetes.io/cluster":  "shoot--dev--test",
					},
					"customLabelDomain": "kubernetes.io",
				},
				[]string{STACKITCCMServiceLoadbalancerController},
			),
		)

		DescribeTable("customLabelDomain propagation to helm charts",
			func(customDomain string, expectedClusterLabelKey string) {
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

				testCluster := *cluster

				cloudProfile := &gardencorev1beta1.CloudProfile{}
				if cluster.CloudProfile != nil {
					*cloudProfile = *cluster.CloudProfile
				}
				testCluster.CloudProfile = cloudProfile

				mgr.EXPECT().GetClient().Return(c)
				mgr.EXPECT().GetScheme().Return(scheme)

				vpCustomDomain := NewValuesProvider(mgr, true, customDomain)
				values, err := vpCustomDomain.GetControlPlaneChartValues(ctx, cp, &testCluster, fakeSecretsManager, checksums, false)
				Expect(err).NotTo(HaveOccurred())

				// Verify STACKIT CCM has customLabelDomain in config
				stackitCCMValues, ok := values[openstack.STACKITCloudControllerManagerName].(map[string]any)
				Expect(ok).To(BeTrue(), "STACKIT CCM values should be a map")
				Expect(stackitCCMValues).To(HaveKey("config"))

				ccmConfig, ok := stackitCCMValues["config"].(map[string]any)
				Expect(ok).To(BeTrue(), "STACKIT CCM config should be a map")
				Expect(ccmConfig).To(HaveKeyWithValue("customLabelDomain", customDomain))

				// Verify extraLabels uses the customLabelDomain for cluster label key
				// Disabled as the load balancer API is currently not accepting `/` in the label
				// TODO: enable this as soon as the load balancer API supports this
				// extraLabels, ok := ccmConfig["extraLabels"].(map[string]string)
				// Expect(ok).To(BeTrue(), "extraLabels should be a string map")
				// Expect(extraLabels).To(HaveKey(expectedClusterLabelKey))
				// Expect(extraLabels[expectedClusterLabelKey]).To(Equal("shoot--dev--test"))

				// Verify STACKIT CSI Controller has customLabelDomain
				stackitCSIValues, ok := values[openstack.CSISTACKITControllerName].(map[string]any)
				Expect(ok).To(BeTrue(), "STACKIT CSI values should be a map")
				Expect(stackitCSIValues).To(HaveKeyWithValue("customLabelDomain", customDomain))

				// Verify OpenStack CSI Controller does NOT have customLabelDomain
				// (it should be disabled when STACKIT CSI is active, but if it were enabled, it shouldn't have customLabelDomain)
				openstackCSIValues, ok := values[openstack.CSIControllerName].(map[string]any)
				Expect(ok).To(BeTrue(), "OpenStack CSI values should be a map")
				Expect(openstackCSIValues).NotTo(HaveKey("customLabelDomain"), "OpenStack CSI should not have customLabelDomain")
			},
			Entry("default kubernetes.io domain",
				"kubernetes.io",
				"kubernetes.io/cluster",
			),
			Entry("custom ske.stackit.cloud domain",
				"ske.stackit.cloud",
				"ske.stackit.cloud/cluster",
			),
			Entry("custom example.com domain",
				"example.com",
				"example.com/cluster",
			),
		)

		DescribeTable("topologyAwareRoutingEnabled value",
			func(seedSettings *gardencorev1beta1.SeedSettings, shootControlPlane *gardencorev1beta1.ControlPlane) {
				c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

				cluster.Seed = &gardencorev1beta1.Seed{
					Spec: gardencorev1beta1.SeedSpec{
						Settings: seedSettings,
					},
				}
				cluster.Shoot.Spec.ControlPlane = shootControlPlane

				controlPlaneConfig := defaultControlPlaneConfig()
				cp.Spec.ProviderConfig.Raw = encode(controlPlaneConfig)

				values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, fakeSecretsManager, checksums, false)
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(HaveKey(openstack.CSIControllerName))
			},

			Entry("seed setting is nil, shoot control plane is not HA",
				nil,
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed setting is disabled, shoot control plane is not HA",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: false}},
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed setting is enabled, shoot control plane is not HA",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: true}},
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed setting is nil, shoot control plane is HA with failure tolerance type 'zone'",
				nil,
				&gardencorev1beta1.ControlPlane{HighAvailability: &gardencorev1beta1.HighAvailability{FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone}}},
			),
			Entry("seed setting is disabled, shoot control plane is HA with failure tolerance type 'zone'",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: false}},
				&gardencorev1beta1.ControlPlane{HighAvailability: &gardencorev1beta1.HighAvailability{FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone}}},
			),
			Entry("seed setting is enabled, shoot control plane is HA with failure tolerance type 'zone'",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: true}},
				&gardencorev1beta1.ControlPlane{HighAvailability: &gardencorev1beta1.HighAvailability{FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone}}},
			),
		)

		It("should return correct control plane ALB chart values if loadbalancerProvider is stackit", func() {
			c.EXPECT().Get(ctx, cpCSIDiskConfigKey, &corev1.Secret{}).DoAndReturn(clientGet(cpCSIDiskConfig))
			c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

			controlPlaneConfig := defaultControlPlaneConfig()
			controlPlaneConfig.ApplicationLoadBalancer = &stackitv1alpha1.ApplicationLoadBalancerConfig{
				Enabled: true,
			}
			cp.Spec.ProviderConfig.Raw = encode(controlPlaneConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, fakeSecretsManager, checksums, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values[openstack.STACKITALBControllerManagerName]).To(Equal(stackitAlbChartValues))
		})
	})

	Describe("#GetControlPlaneShootChartValues", func() {
		BeforeEach(func() {
			By("creating secrets managed outside of this package for whose secretsmanager.Get() will be called")
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ca-provider-openstack-controlplane", Namespace: namespace}})).To(Succeed())
			Expect(fakeClient.Create(context.TODO(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "cloud-controller-manager-server", Namespace: namespace}})).To(Succeed())
		})

		Context("shoot control plane chart values", func() {
			It("should return correct shoot control plane chart when ca is secret found", func() {
				// Refactoring led to retrieving it three times at a lower level
				// This is the vp.getCredentials() call
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

				expectCSICleanupinControlPlane(ctx, c, openstack.CSIControllerName)

				values, err := vp.GetControlPlaneShootChartValues(ctx, cp, cluster, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]any{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSISTACKITNodeName: utils.MergeMaps(enabledTrue, map[string]any{
						"rescanBlockStorageOnResize": rescanBlockStorageOnResize,
						"nodeVolumeAttachLimit":      ptr.To[int32](nodeVoluemAttachLimit),
						"userAgentHeaders":           []string{domainName, tenantName, technicalID},
					}),
					openstack.CSINodeName: enabledFalse,
				}))
			})

			It("should return correct shoot control plane chart if CSI STACKIT is enabled", func() {
				c.EXPECT().Get(ctx, cpSecretKey, &corev1.Secret{}).DoAndReturn(clientGet(cpSecret)).Times(2)

				expectCSICleanupinControlPlane(ctx, c, openstack.CSIControllerName)

				cpStackit := defaultControlPlaneWithSTACKIT()
				values, err := vp.GetControlPlaneShootChartValues(ctx, cpStackit, cluster, fakeSecretsManager, map[string]string{})
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(Equal(map[string]any{
					openstack.CloudControllerManagerName: enabledTrue,
					openstack.CSISTACKITControllerName: utils.MergeMaps(enabledTrue, map[string]any{
						"rescanBlockStorageOnResize": rescanBlockStorageOnResize,
						"nodeVolumeAttachLimit":      ptr.To[int32](nodeVoluemAttachLimit),
						"userAgentHeaders":           []string{domainName, tenantName, technicalID},
					}),
					openstack.CSINodeName: enabledFalse,
				}))
			})
		})
	})

	Describe("#GetStorageClassesChartValues", func() {
		It("should return correct storage class chart values", func() {
			values, err := vp.GetStorageClassesChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values["storageclasses"]).To(HaveLen(2))
			Expect(values["storageclasses"].([]map[string]any)[0]["provisioner"]).To(Equal(openstack.CSIStorageProvisioner))
			Expect(values["storageclasses"].([]map[string]any)[1]["provisioner"]).To(Equal(openstack.CSIStorageProvisioner))
		})
	})

	Describe("#checkEmergencyLoadBalancerAccess", func() {
		secretNamespacedName := types.NamespacedName{Name: LoadBalancerEmergencyAccessSecretName, Namespace: namespace}

		Context("emergency access disabled", func() {
			It("should not return an error if the secret does not exist", func() {
				c.EXPECT().Get(ctx, secretNamespacedName, &corev1.Secret{}).Return(
					errors.NewNotFound(schema.GroupResource{Resource: "secret"}, LoadBalancerEmergencyAccessSecretName))

				apiURL, apiToken, err := vp.(*valuesProvider).checkEmergencyLoadBalancerAccess(ctx, secretNamespacedName)
				Expect(apiURL).To(BeEmpty())
				Expect(apiToken).To(BeEmpty())
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return any error except NotFound from the get call", func() {
				expectedError := fmt.Errorf("something went wrong")
				c.EXPECT().Get(ctx, secretNamespacedName, &corev1.Secret{}).Return(expectedError)

				apiURL, apiToken, err := vp.(*valuesProvider).checkEmergencyLoadBalancerAccess(ctx, secretNamespacedName)
				Expect(apiURL).To(BeEmpty())
				Expect(apiToken).To(BeEmpty())
				Expect(err).To(Equal(expectedError))
			})
		})

		Context("emergency access enabled", func() {
			It("should return non-empty apiUrl and apiToken", func() {
				emergencySecret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      LoadBalancerEmergencyAccessSecretName,
						Namespace: namespace,
					},
					Data: map[string][]byte{
						LoadBalancerEmergencyAccessAPIURLKey:   []byte("foo"),
						LoadBalancerEmergencyAccessAPITokenKey: []byte("bar"),
					},
				}
				c.EXPECT().Get(ctx, secretNamespacedName, &corev1.Secret{}).DoAndReturn(clientGet(emergencySecret))

				apiURL, apiToken, err := vp.(*valuesProvider).checkEmergencyLoadBalancerAccess(ctx, secretNamespacedName)
				Expect(err).ToNot(HaveOccurred())
				Expect(apiURL).To(Equal("foo"))
				Expect(apiToken).To(Equal("bar"))
			})
		})
	})

	DescribeTable("#decodeLoadBalancerAPIEmergencySecret", func(url, token *string, errExpected error) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LoadBalancerEmergencyAccessSecretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{},
		}

		if url != nil {
			secret.Data[LoadBalancerEmergencyAccessAPIURLKey] = []byte(*url)
		}
		if token != nil {
			secret.Data[LoadBalancerEmergencyAccessAPITokenKey] = []byte(*token)
		}

		apiURL, apiToken, err := decodeLoadBalancerAPIEmergencySecret(secret)

		if errExpected != nil {
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(errExpected))
			Expect(apiURL).To(BeEmpty())
			Expect(apiToken).To(BeEmpty())
		} else {
			Expect(err).ToNot(HaveOccurred())
			Expect(apiURL).To(Equal(*url))
			Expect(apiToken).To(Equal(*token))
		}
	},

		Entry("missing url", nil, ptr.To("token"), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPIURLKey)),
		Entry("empty url", ptr.To(""), ptr.To("token"), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPIURLKey)),
		Entry("missing token", ptr.To("url"), nil, fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPITokenKey)),
		Entry("empty token", ptr.To("url"), ptr.To(""), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPITokenKey)),
		Entry("valid secret", ptr.To("url"), ptr.To("token"), nil),
	)
})

func expectCSICleanupinControlPlane(ctx context.Context, c *mockclient.MockClient, subChartName string) {
	for _, subchart := range controlPlaneChart.SubCharts {
		if subchart.Name == subChartName {
			for _, obj := range subchart.Objects {
				objToDelete := obj.Type
				objToDelete.SetNamespace(namespace)
				objToDelete.SetName(obj.Name)

				c.EXPECT().Delete(ctx, objToDelete)
			}
		}
	}
}

func encode(obj runtime.Object) []byte {
	data, _ := json.Marshal(obj)
	return data
}

func clientGet(result runtime.Object) any {
	return func(ctx context.Context, key client.ObjectKey, obj runtime.Object, _ ...client.GetOption) error {
		if _, ok := obj.(*corev1.Secret); ok {
			*obj.(*corev1.Secret) = *result.(*corev1.Secret)
		}
		return nil
	}
}

func stackitCCMDeletion(ctx context.Context, c *mockclient.MockClient) {
	c.EXPECT().Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: openstack.STACKITCloudControllerManagerName, Namespace: namespace}})
	c.EXPECT().Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: openstack.STACKITCloudControllerManagerName, Namespace: namespace}})
	c.EXPECT().Delete(ctx, &vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: openstack.STACKITCloudControllerManagerName + "-vpa", Namespace: namespace}})
}
