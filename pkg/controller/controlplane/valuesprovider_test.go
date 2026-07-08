// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	calicov1alpha1 "github.com/gardener/gardener-extension-networking-calico/pkg/apis/calico/v1alpha1"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/chart"
	secretutils "github.com/gardener/gardener/pkg/utils/secrets"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	fakesecretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager/fake"
	testutils "github.com/gardener/gardener/pkg/utils/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

var testRequestTimeout = &metav1.Duration{Duration: 5 * time.Minute}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(calicov1alpha1.AddToScheme(scheme))
	utilruntime.Must(extensionsv1alpha1.AddToScheme(scheme))
	utilruntime.Must(stackitv1alpha1.AddToScheme(scheme))
	utilruntime.Must(vpaautoscalingv1.AddToScheme(scheme))
	return scheme
}

func newTestValuesProvider(cl client.Client, scheme *runtime.Scheme, deployALB bool, customLabelDomain string) *valuesProvider {
	mgr := &testutils.FakeManager{Scheme: scheme, Client: cl}
	return NewValuesProvider(mgr, deployALB, customLabelDomain).(*valuesProvider)
}

func baseControlPlaneConfig() *stackitv1alpha1.ControlPlaneConfig {
	return &stackitv1alpha1.ControlPlaneConfig{
		CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
			FeatureGates: map[string]bool{
				"SomeKubernetesFeature": true,
			},
		},
		Storage: &stackitv1alpha1.Storage{
			CSI: &stackitv1alpha1.CSI{
				Name: string(stackitv1alpha1.STACKIT),
			},
		},
	}
}

func baseControlPlane() *extensionsv1alpha1.ControlPlane {
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
					Raw: encode(baseControlPlaneConfig()),
				},
			},
			Region: region,
			InfrastructureProviderStatus: &runtime.RawExtension{
				Raw: encode(&stackitv1alpha1.InfrastructureStatus{
					Networks: stackitv1alpha1.NetworkStatus{
						Name: technicalID,
						ID:   "network-acbd1234",
						FloatingPool: stackitv1alpha1.FloatingPoolStatus{
							ID: "floating-network-id",
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

func baseCloudProfileConfig() *stackitv1alpha1.CloudProfileConfig {
	return &stackitv1alpha1.CloudProfileConfig{
		KeyStoneURL:                authURL,
		RequestTimeout:             testRequestTimeout,
		RescanBlockStorageOnResize: new(true),
		IgnoreVolumeAZ:             new(true),
		NodeVolumeAttachLimit:      new(int32(25)),
	}
}

func baseCluster() *extensionscontroller.Cluster {
	return &extensionscontroller.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"generic-token-kubeconfig.secret.gardener.cloud/name": genericTokenKubeconfigSecretName,
			},
		},
		CloudProfile: &gardencorev1beta1.CloudProfile{
			Spec: gardencorev1beta1.CloudProfileSpec{
				ProviderConfig: &runtime.RawExtension{
					Raw: encode(baseCloudProfileConfig()),
				},
			},
		},
		Seed: &gardencorev1beta1.Seed{},
		Shoot: &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "test-shoot"},
			Spec: gardencorev1beta1.ShootSpec{
				Region: "RegionOne",
				Networking: &gardencorev1beta1.Networking{
					Pods: new("10.250.0.0/19"),
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
}

func clusterWithoutOverlay() *extensionscontroller.Cluster {
	cluster := baseCluster()
	cluster.Shoot.Spec.Networking = &gardencorev1beta1.Networking{
		Type: new("calico"),
		ProviderConfig: &runtime.RawExtension{
			Raw: []byte(`{"overlay":{"enabled":false}}`),
		},
		Pods: cluster.Shoot.Spec.Networking.Pods,
	}
	return cluster
}

func baseProviderSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v1beta1constants.SecretNameCloudProvider,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"domainName":      []byte("domain-name"),
			"tenantName":      []byte("tenant-name"),
			"username":        []byte("username"),
			"password":        []byte("password"),
			"authURL":         []byte(authURL),
			stackit.ProjectID: []byte("foo"),
			stackit.SaKeyJSON: []byte("{}"),
		},
	}
}

func baseCloudProviderConfigSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      openstack.CloudProviderConfigName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			openstack.CloudProviderConfigDataKey: []byte("some data"),
		},
	}
}

func baseCSIDiskConfigSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      openstack.CloudProviderCSIDiskConfigName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			openstack.CloudProviderConfigDataKey: []byte("foo"),
		},
	}
}

func managedSecrets() []client.Object {
	return []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      caNameControlPlane,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				secretutils.DataKeyCertificateBundle: []byte("fake-ca-cert"),
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cloudControllerManagerServerName,
				Namespace: namespace,
			},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stackitPodIdentityWebhookServerName,
				Namespace: namespace,
			},
		},
	}
}

func legacyCleanupObjects() []client.Object {
	stackitSnapshotName := fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CSISnapshotValidationName)

	return []client.Object{
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-kube-apiserver-to-csi-snapshot-validation", Namespace: namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
		&vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-webhook-vpa", Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapshotName, Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapshotName, Namespace: namespace}},
		&vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-csi-snapshot-webhook-vpa", CSIStackitPrefix), Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapshotName, Namespace: namespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CloudProviderConfigName), Namespace: namespace}},
	}
}

func emergencyLBSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LoadBalancerEmergencyAccessSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			LoadBalancerEmergencyAccessAPIURLKey:   []byte("foo"),
			LoadBalancerEmergencyAccessAPITokenKey: []byte("bar"),
		},
	}
}

func encode(obj any) []byte {
	data, err := json.Marshal(obj)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func createObjects(ctx context.Context, cl client.Client, objects ...client.Object) {
	for _, obj := range objects {
		ExpectWithOffset(1, cl.Create(ctx, obj)).To(Succeed())
	}
}

func checksumsFor(secrets ...*corev1.Secret) map[string]string {
	checksums := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		checksums[secret.Name] = gardenerutils.ComputeChecksum(secret.Data)
	}
	return checksums
}

func chartValues(values map[string]any, chartName string) map[string]any {
	ExpectWithOffset(1, values).To(HaveKey(chartName))
	if values[chartName] == nil {
		return nil
	}

	chartValues, ok := values[chartName].(map[string]any)
	ExpectWithOffset(1, ok).To(BeTrue(), "expected chart %q values to be a map", chartName)
	return chartValues
}

func instantiateChartObject(template client.Object, name, namespace string) client.Object {
	obj := template.DeepCopyObject().(client.Object)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	return obj
}

func subChartObjects(subCharts []*chart.Chart, subChartName, namespace string) []client.Object {
	var objects []client.Object
	for _, subChart := range subCharts {
		if subChart.Name != subChartName {
			continue
		}
		for _, obj := range subChart.Objects {
			objects = append(objects, instantiateChartObject(obj.Type, obj.Name, namespace))
		}
	}
	ExpectWithOffset(1, objects).NotTo(BeEmpty(), "expected subchart %q to exist", subChartName)
	return objects
}

func seedUnusedControlPlaneCSIObjects(ctx context.Context, cl client.Client, subChartName string) []client.Object {
	objects := subChartObjects(controlPlaneChart.SubCharts, subChartName, namespace)
	createObjects(ctx, cl, objects...)
	return objects
}

func seedReadyControlPlane(ctx context.Context, cl client.Client) (*extensionsv1alpha1.ControlPlane, *extensionscontroller.Cluster, *corev1.Secret, *corev1.Secret) {
	cp := baseControlPlane()
	cluster := baseCluster()
	providerSecret := baseProviderSecret()
	configSecret := baseCloudProviderConfigSecret()
	diskSecret := baseCSIDiskConfigSecret()

	//nolint:prealloc // this is just a test
	var objects []client.Object
	objects = append(objects, providerSecret, configSecret, diskSecret)
	objects = append(objects, managedSecrets()...)
	createObjects(ctx, cl, objects...)
	return cp, cluster, providerSecret, diskSecret
}

func seedReadyShoot(ctx context.Context, cl client.Client) (*extensionsv1alpha1.ControlPlane, *extensionscontroller.Cluster) {
	cp := baseControlPlane()
	cluster := baseCluster()
	createObjects(ctx, cl, baseProviderSecret())
	createObjects(ctx, cl, managedSecrets()...)
	return cp, cluster
}

func expectObjectsDeleted(ctx context.Context, cl client.Client, objects ...client.Object) {
	for _, obj := range objects {
		err := cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		ExpectWithOffset(1, errors.IsNotFound(err)).To(BeTrue(), "expected %T %s to be deleted", obj, client.ObjectKeyFromObject(obj).String())
	}
}

func expectedUserAgentHeaders() []string {
	return []string{"domain-name", "tenant-name", technicalID}
}

func expectedSTACKITCCMConfig(customLabelDomain string, apiEndpoints *stackitv1alpha1.APIEndpoints) map[string]any {
	config := map[string]any{
		"stackitProjectID": "foo",
		"stackitNetworkID": "network-acbd1234",
		"stackitRegion":    "eu01",
		"extraLabels": map[string]string{
			STACKITLBClusterLabelKey: technicalID,
		},
		"customLabelDomain": customLabelDomain,
	}

	if apiEndpoints != nil {
		if apiEndpoints.LoadBalancer != nil {
			config["loadBalancerApiUrl"] = *apiEndpoints.LoadBalancer
		}
		if apiEndpoints.IaaS != nil {
			config["iaasApiUrl"] = *apiEndpoints.IaaS
		}
		if apiEndpoints.TokenEndpoint != nil {
			config["tokenUrl"] = *apiEndpoints.TokenEndpoint
		}
	}

	return config
}

var _ = Describe("ValuesProvider fake client", func() {
	var (
		ctx            context.Context
		scheme         *runtime.Scheme
		c              client.Client
		secretsManager secretsmanager.Interface
		vp             *valuesProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = newTestScheme()
		c = fake.NewClientBuilder().WithScheme(scheme).Build()
		secretsManager = fakesecretsmanager.New(c, namespace)
		vp = newTestValuesProvider(c, scheme, true, "kubernetes.io")
	})

	Describe("#GetConfigChartValues", func() {
		BeforeEach(func() {
			DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.UseSTACKITAPIInfrastructureController, false))
			DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.UseSTACKITMachineControllerManager, false))
		})

		expectedConfigChartValues := func() map[string]any {
			return map[string]any{
				"domainName":                  "domain-name",
				"tenantName":                  "tenant-name",
				"username":                    "username",
				"password":                    "password",
				"region":                      region,
				"insecure":                    false,
				"authUrl":                     authURL,
				"requestTimeout":              testRequestTimeout,
				"ignoreVolumeAZ":              true,
				"applicationCredentialID":     "",
				"applicationCredentialSecret": "",
				"applicationCredentialName":   "",
				"internalNetworkName":         technicalID,
			}
		}

		It("returns config values for username and password credentials", func() {
			cp := baseControlPlane()
			cluster := baseCluster()
			providerSecret := baseProviderSecret()
			createObjects(ctx, c, providerSecret)

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedConfigChartValues()))
		})

		It("returns config values for application credentials", func() {
			cp := baseControlPlane()
			cluster := baseCluster()
			providerSecret := baseProviderSecret()
			providerSecret.Data = map[string][]byte{
				"domainName":                  []byte("domain-name"),
				"tenantName":                  []byte("tenant-name"),
				"applicationCredentialID":     []byte("app-id"),
				"applicationCredentialSecret": []byte("app-secret"),
				"authURL":                     []byte(authURL),
				stackit.ProjectID:             []byte("foo"),
				stackit.SaKeyJSON:             []byte("{}"),
			}
			createObjects(ctx, c, providerSecret)

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			expectedValues := expectedConfigChartValues()
			expectedValues["username"] = ""
			expectedValues["password"] = ""
			expectedValues["applicationCredentialID"] = "app-id"
			expectedValues["applicationCredentialSecret"] = "app-secret"
			Expect(values).To(Equal(expectedValues))
		})

		It("enables route controller when overlay is disabled", func() {
			cp := baseControlPlane()
			cluster := clusterWithoutOverlay()
			createObjects(ctx, c, baseProviderSecret())

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			expectedValues := expectedConfigChartValues()
			expectedValues["routerID"] = "routerID"
			Expect(values).To(Equal(expectedValues))
		})

		It("disables route controller when overlay is disabled but BGP backend is active", func() {
			cp := baseControlPlane()
			cluster := clusterWithoutOverlay()
			cluster.Shoot.Spec.Networking.ProviderConfig = &runtime.RawExtension{
				Raw: []byte(`{"backend":"bird","overlay":{"enabled":false}}`),
			}
			createObjects(ctx, c, baseProviderSecret())

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(values).To(Equal(expectedConfigChartValues()))
		})

		It("propagates a custom keystone CA certificate", func() {
			cp := baseControlPlane()
			cluster := baseCluster()
			providerSecret := baseProviderSecret()
			providerSecret.Data["caCert"] = []byte("custom-cert")
			createObjects(ctx, c, providerSecret)

			values, err := vp.GetConfigChartValues(ctx, cp, cluster)
			Expect(err).NotTo(HaveOccurred())
			expectedValues := expectedConfigChartValues()
			expectedValues["caCert"] = "custom-cert"
			Expect(values).To(Equal(expectedValues))
		})
	})

	Describe("#GetControlPlaneChartValues", func() {
		It("returns default control plane values with STACKIT CSI active", func() {
			cp, cluster, providerSecret, diskSecret := seedReadyControlPlane(ctx, c)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())

			Expect(values).To(HaveKeyWithValue("global", map[string]any{
				"genericTokenKubeconfigSecretName": genericTokenKubeconfigSecretName,
			}))

			ccmValues := chartValues(values, openstack.CloudControllerManagerName)
			Expect(ccmValues).To(HaveKeyWithValue("enabled", false))

			expectedSTACKITCCMConfig := expectedSTACKITCCMConfig("kubernetes.io", nil)
			stackitCCMValues := chartValues(values, openstack.STACKITCloudControllerManagerName)
			Expect(stackitCCMValues).To(BeComparableTo(map[string]any{
				"enabled":     true,
				"replicas":    1,
				"technicalID": technicalID,
				"config":      expectedSTACKITCCMConfig,
				"controllers": []string{"*"},
				"podAnnotations": map[string]any{
					"checksum/secret-" + v1beta1constants.SecretNameCloudProvider:         gardenerutils.ComputeChecksum(providerSecret.Data),
					"checksum/config-" + openstack.STACKITCloudControllerManagerImageName: gardenerutils.ComputeChecksum(expectedSTACKITCCMConfig),
				},
				"podLabels": map[string]any{
					v1beta1constants.LabelPodMaintenanceRestart: "true",
				},
				"featureGates": map[string]bool{
					"SomeKubernetesFeature": true,
				},
			}))

			Expect(chartValues(values, openstack.CSIControllerName)).To(Equal(map[string]any{
				"enabled": false,
			}))

			stackitCSIValues := chartValues(values, openstack.CSISTACKITControllerName)
			Expect(stackitCSIValues).To(BeComparableTo(map[string]any{
				"enabled":   true,
				"projectID": "foo",
				"region":    "eu01",
				"replicas":  1,
				"podAnnotations": map[string]any{
					"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: gardenerutils.ComputeChecksum(diskSecret.Data),
					"checksum/secret-" + v1beta1constants.SecretNameCloudProvider: gardenerutils.ComputeChecksum(providerSecret.Data),
				},
				"csiSnapshotController": map[string]any{
					"replicas": 1,
				},
				"stackitEndpoints":  map[string]string{},
				"customLabelDomain": "kubernetes.io",
				"userAgentHeaders":  expectedUserAgentHeaders(),
			}))

			Expect(chartValues(values, stackit.PodIdentityWebhookName)).To(BeComparableTo(map[string]any{
				"enabled":      false,
				"replicaCount": 1,
				"webhook": map[string]any{
					"tlsSecretName": stackitPodIdentityWebhookServerName,
				},
			}))
			Expect(values[openstack.STACKITALBControllerManagerName]).To(BeNil())
		})

		It("returns OpenStack CSI values when selected", func() {
			cp, cluster, providerSecret, diskSecret := seedReadyControlPlane(ctx, c)
			cpConfig := baseControlPlaneConfig()
			cpConfig.Storage.CSI.Name = string(stackitv1alpha1.OPENSTACK)
			cp.Spec.ProviderConfig.Raw = encode(cpConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())

			Expect(chartValues(values, openstack.CSISTACKITControllerName)).To(Equal(map[string]any{
				"enabled": false,
			}))
			Expect(chartValues(values, openstack.CSIControllerName)).To(BeComparableTo(map[string]any{
				"enabled":           true,
				"replicas":          1,
				"maxEntries":        1000,
				"kubernetesVersion": "1.29.0",
				"podAnnotations": map[string]any{
					"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: gardenerutils.ComputeChecksum(diskSecret.Data),
				},
				"csiSnapshotController": map[string]any{
					"replicas": 1,
				},
				"userAgentHeaders": expectedUserAgentHeaders(),
			}))
		})

		It("enables OpenStack CCM while reducing STACKIT CCM controllers", func() {
			cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
			cpConfig := baseControlPlaneConfig()
			cpConfig.CloudControllerManager.Name = string(stackitv1alpha1.OPENSTACK)
			cp.Spec.ProviderConfig.Raw = encode(cpConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())

			Expect(chartValues(values, openstack.CloudControllerManagerName)).To(HaveKeyWithValue("enabled", true))
			Expect(chartValues(values, openstack.STACKITCloudControllerManagerName)).To(HaveKeyWithValue("controllers", []string{STACKITCCMServiceLoadbalancerController}))
			Expect(chartValues(values, openstack.CSIControllerName)).To(HaveKeyWithValue("enabled", false))
		})

		DescribeTable("renders STACKIT CCM config variants",
			func(apiEndpoints *stackitv1alpha1.APIEndpoints, cpConfig *stackitv1alpha1.ControlPlaneConfig, expectedControllers []string) {
				cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
				if cpConfig != nil {
					cp.Spec.ProviderConfig.Raw = encode(cpConfig)
				}

				cloudProfileConfig := baseCloudProfileConfig()
				cloudProfileConfig.APIEndpoints = apiEndpoints
				cluster.CloudProfile.Spec.ProviderConfig = &runtime.RawExtension{Raw: encode(cloudProfileConfig)}

				values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
				Expect(err).NotTo(HaveOccurred())

				expectedConfig := expectedSTACKITCCMConfig("kubernetes.io", apiEndpoints)
				stackitCCMValues := chartValues(values, openstack.STACKITCloudControllerManagerName)
				Expect(stackitCCMValues).To(HaveKeyWithValue("config", BeComparableTo(expectedConfig)))
				Expect(stackitCCMValues).To(HaveKeyWithValue("controllers", expectedControllers))
			},
			Entry("default endpoints", nil, nil, []string{"*"}),
			Entry("custom load balancer endpoint",
				&stackitv1alpha1.APIEndpoints{LoadBalancer: new("https://custom-lb.stackit.cloud")},
				nil,
				[]string{"*"},
			),
			Entry("custom load balancer and token endpoint",
				&stackitv1alpha1.APIEndpoints{
					LoadBalancer:  new("https://custom-lb.stackit.cloud"),
					TokenEndpoint: new("https://custom-auth.stackit.cloud/token"),
				},
				nil,
				[]string{"*"},
			),
			Entry("OpenStack CCM still uses service-lb-only controllers",
				nil,
				&stackitv1alpha1.ControlPlaneConfig{
					CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
						Name: string(stackitv1alpha1.OPENSTACK),
					},
				},
				[]string{STACKITCCMServiceLoadbalancerController},
			),
		)

		DescribeTable("propagates custom label domains",
			func(customLabelDomain string) {
				vp = newTestValuesProvider(c, scheme, true, customLabelDomain)
				cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)

				values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
				Expect(err).NotTo(HaveOccurred())

				stackitCCMConfig := chartValues(values, openstack.STACKITCloudControllerManagerName)["config"].(map[string]any)
				Expect(stackitCCMConfig).To(HaveKeyWithValue("customLabelDomain", customLabelDomain))
				Expect(chartValues(values, openstack.CSISTACKITControllerName)).To(HaveKeyWithValue("customLabelDomain", customLabelDomain))
				Expect(chartValues(values, openstack.CSIControllerName)).NotTo(HaveKey("customLabelDomain"))
			},
			Entry("default kubernetes.io domain", "kubernetes.io"),
			Entry("custom ske.stackit.cloud domain", "ske.stackit.cloud"),
			Entry("custom example.com domain", "example.com"),
		)

		DescribeTable("supports topology-aware-routing compatibility combinations",
			func(seedSettings *gardencorev1beta1.SeedSettings, shootControlPlane *gardencorev1beta1.ControlPlane) {
				cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
				cluster.Seed = &gardencorev1beta1.Seed{
					Spec: gardencorev1beta1.SeedSpec{
						Settings: seedSettings,
					},
				}
				cluster.Shoot.Spec.ControlPlane = shootControlPlane

				values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
				Expect(err).NotTo(HaveOccurred())
				Expect(values).To(HaveKey(openstack.CSISTACKITControllerName))
			},
			Entry("seed settings nil and shoot control plane not HA",
				nil,
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed setting disabled and shoot control plane not HA",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: false}},
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed setting enabled and shoot control plane not HA",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: true}},
				&gardencorev1beta1.ControlPlane{HighAvailability: nil},
			),
			Entry("seed settings nil and zonal HA shoot control plane",
				nil,
				&gardencorev1beta1.ControlPlane{
					HighAvailability: &gardencorev1beta1.HighAvailability{
						FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone},
					},
				},
			),
			Entry("seed setting disabled and zonal HA shoot control plane",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: false}},
				&gardencorev1beta1.ControlPlane{
					HighAvailability: &gardencorev1beta1.HighAvailability{
						FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone},
					},
				},
			),
			Entry("seed setting enabled and zonal HA shoot control plane",
				&gardencorev1beta1.SeedSettings{TopologyAwareRouting: &gardencorev1beta1.SeedSettingTopologyAwareRouting{Enabled: true}},
				&gardencorev1beta1.ControlPlane{
					HighAvailability: &gardencorev1beta1.HighAvailability{
						FailureTolerance: gardencorev1beta1.FailureTolerance{Type: gardencorev1beta1.FailureToleranceTypeZone},
					},
				},
			),
		)

		It("returns ALB controller values when enabled", func() {
			cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
			cpConfig := baseControlPlaneConfig()
			cpConfig.ApplicationLoadBalancer = &stackitv1alpha1.ApplicationLoadBalancerConfig{Enabled: true}
			cp.Spec.ProviderConfig.Raw = encode(cpConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())
			Expect(chartValues(values, openstack.STACKITALBControllerManagerName)).To(BeComparableTo(map[string]any{
				"enabled":  true,
				"replicas": 1,
				"config": map[string]any{
					"region":           "eu01",
					"stackitProjectID": "foo",
					"stackitNetworkID": "network-acbd1234",
				},
			}))
		})

		It("omits ALB controller values when the config disables ALB deployment", func() {
			vp = newTestValuesProvider(c, scheme, false, "kubernetes.io")
			cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
			cpConfig := baseControlPlaneConfig()
			cpConfig.ApplicationLoadBalancer = &stackitv1alpha1.ApplicationLoadBalancerConfig{Enabled: true}
			cp.Spec.ProviderConfig.Raw = encode(cpConfig)

			values, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())
			Expect(values[openstack.STACKITALBControllerManagerName]).To(BeNil())
		})

		It("deletes legacy cleanup objects from the fake client", func() {
			cp, cluster, providerSecret, _ := seedReadyControlPlane(ctx, c)
			legacyObjects := legacyCleanupObjects()
			createObjects(ctx, c, legacyObjects...)

			_, err := vp.GetControlPlaneChartValues(ctx, cp, cluster, secretsManager, checksumsFor(providerSecret), false)
			Expect(err).NotTo(HaveOccurred())
			expectObjectsDeleted(ctx, c, legacyObjects...)
		})
	})

	Describe("#GetControlPlaneShootChartValues", func() {
		It("returns OpenStack shoot chart values and deletes unused STACKIT CSI control-plane objects", func() {
			cp, cluster := seedReadyShoot(ctx, c)
			cpConfig := baseControlPlaneConfig()
			cpConfig.Storage.CSI.Name = string(stackitv1alpha1.OPENSTACK)
			cp.Spec.ProviderConfig.Raw = encode(cpConfig)
			unusedObjects := seedUnusedControlPlaneCSIObjects(ctx, c, openstack.CSISTACKITControllerName)

			values, err := vp.GetControlPlaneShootChartValues(ctx, cp, cluster, secretsManager, map[string]string{})
			Expect(err).NotTo(HaveOccurred())

			Expect(chartValues(values, openstack.CloudControllerManagerName)).To(Equal(map[string]any{"enabled": true}))
			Expect(chartValues(values, openstack.CSINodeName)).To(BeComparableTo(map[string]any{
				"enabled":                    true,
				"rescanBlockStorageOnResize": true,
				"nodeVolumeAttachLimit":      new(int32(25)),
				"userAgentHeaders":           expectedUserAgentHeaders(),
			}))
			Expect(chartValues(values, openstack.CSISTACKITNodeName)).To(Equal(map[string]any{"enabled": false}))
			Expect(chartValues(values, stackit.PodIdentityWebhookName)).To(BeComparableTo(map[string]any{
				"enabled": false,
				"webhook": map[string]any{
					"caBundle": []byte("fake-ca-cert"),
					"url":      fmt.Sprintf("https://%s.%s:443/mutate--v1-pod", stackit.PodIdentityWebhookName, namespace),
				},
			}))
			expectObjectsDeleted(ctx, c, unusedObjects...)
		})

		It("returns STACKIT CSI shoot chart values and deletes unused OpenStack CSI control-plane objects", func() {
			cp, cluster := seedReadyShoot(ctx, c)
			unusedObjects := seedUnusedControlPlaneCSIObjects(ctx, c, openstack.CSIControllerName)

			values, err := vp.GetControlPlaneShootChartValues(ctx, cp, cluster, secretsManager, map[string]string{})
			Expect(err).NotTo(HaveOccurred())

			Expect(chartValues(values, openstack.CloudControllerManagerName)).To(Equal(map[string]any{"enabled": true}))
			Expect(chartValues(values, openstack.CSISTACKITNodeName)).To(BeComparableTo(map[string]any{
				"enabled":                    true,
				"rescanBlockStorageOnResize": true,
				"userAgentHeaders":           expectedUserAgentHeaders(),
			}))
			Expect(chartValues(values, openstack.CSINodeName)).To(Equal(map[string]any{"enabled": false}))
			Expect(chartValues(values, stackit.PodIdentityWebhookName)).To(BeComparableTo(map[string]any{
				"enabled": false,
				"webhook": map[string]any{
					"caBundle": []byte("fake-ca-cert"),
					"url":      fmt.Sprintf("https://%s.%s:443/mutate--v1-pod", stackit.PodIdentityWebhookName, namespace),
				},
			}))
			expectObjectsDeleted(ctx, c, unusedObjects...)
		})
	})

	Describe("#GetStorageClassesChartValues", func() {
		It("returns the default storage classes with the OpenStack provisioner", func() {
			values, err := vp.GetStorageClassesChartValues(ctx, baseControlPlane(), baseCluster())
			Expect(err).NotTo(HaveOccurred())

			storageClasses, ok := values["storageclasses"].([]map[string]any)
			Expect(ok).To(BeTrue())
			Expect(storageClasses).To(HaveLen(2))
			Expect(storageClasses[0]).To(HaveKeyWithValue("name", "default"))
			Expect(storageClasses[0]).To(HaveKeyWithValue("default", true))
			Expect(storageClasses[0]).To(HaveKeyWithValue("provisioner", openstack.CSIStorageProvisioner))
			Expect(storageClasses[1]).To(HaveKeyWithValue("name", "default-class"))
			Expect(storageClasses[1]).To(HaveKeyWithValue("provisioner", openstack.CSIStorageProvisioner))
		})
	})

	Describe("#checkEmergencyLoadBalancerAccess", func() {
		secretKey := client.ObjectKey{Name: LoadBalancerEmergencyAccessSecretName, Namespace: namespace}

		It("returns empty values when the secret is missing", func() {
			apiURL, apiToken, err := vp.checkEmergencyLoadBalancerAccess(ctx, secretKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiURL).To(BeEmpty())
			Expect(apiToken).To(BeEmpty())
		})

		It("returns non-NotFound client errors from the fake interceptor", func() {
			expectedErr := fmt.Errorf("something went wrong")
			interceptedClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if key == secretKey {
							return expectedErr
						}
						return cl.Get(ctx, key, obj, opts...)
					},
				}).
				Build()
			interceptedProvider := newTestValuesProvider(interceptedClient, scheme, true, "kubernetes.io")

			apiURL, apiToken, err := interceptedProvider.checkEmergencyLoadBalancerAccess(ctx, secretKey)
			Expect(err).To(MatchError(expectedErr))
			Expect(apiURL).To(BeEmpty())
			Expect(apiToken).To(BeEmpty())
		})

		It("returns decoded emergency access values for a valid secret", func() {
			createObjects(ctx, c, emergencyLBSecret())

			apiURL, apiToken, err := vp.checkEmergencyLoadBalancerAccess(ctx, secretKey)
			Expect(err).NotTo(HaveOccurred())
			Expect(apiURL).To(Equal("foo"))
			Expect(apiToken).To(Equal("bar"))
		})
	})

	DescribeTable("#decodeLoadBalancerAPIEmergencySecret",
		func(url, token *string, expectedErr error) {
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
			if expectedErr != nil {
				Expect(err).To(MatchError(expectedErr))
				Expect(apiURL).To(BeEmpty())
				Expect(apiToken).To(BeEmpty())
				return
			}

			Expect(err).NotTo(HaveOccurred())
			Expect(apiURL).To(Equal(*url))
			Expect(apiToken).To(Equal(*token))
		},
		Entry("missing URL", nil, new("token"), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPIURLKey)),
		Entry("empty URL", new(""), new("token"), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPIURLKey)),
		Entry("missing token", new("url"), nil, fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPITokenKey)),
		Entry("empty token", new("url"), new(""), fmt.Errorf("missing or empty secret key %s", LoadBalancerEmergencyAccessAPITokenKey)),
		Entry("valid secret", new("url"), new("token"), nil),
	)

	DescribeTable("#shouldEnablePodIdentityWebhook",
		func(featureGateEnabled bool, configureCluster func(*extensionscontroller.Cluster), expected bool) {
			DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.EnableSTACKITWorkloadIdentity, featureGateEnabled))

			cluster := baseCluster()
			configureCluster(cluster)
			Expect(shouldEnablePodIdentityWebhook(cluster)).To(Equal(expected))
		},
		Entry("returns false when the feature gate is disabled",
			false,
			func(cluster *extensionscontroller.Cluster) {},
			false,
		),
		Entry("returns false without annotations and without a custom issuer",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = nil
				cluster.Shoot.Spec.Kubernetes.KubeAPIServer = nil
			},
			false,
		),
		Entry("returns true without annotations when a custom issuer is set",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = nil
				cluster.Shoot.Spec.Kubernetes.KubeAPIServer = &gardencorev1beta1.KubeAPIServerConfig{
					ServiceAccountConfig: &gardencorev1beta1.ServiceAccountConfig{
						Issuer: new("foo"),
					},
				}
			},
			true,
		),
		Entry("returns false when the issuer annotation is missing and there is no custom issuer",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = map[string]string{}
				cluster.Shoot.Spec.Kubernetes.KubeAPIServer = nil
			},
			false,
		),
		Entry("returns false when the issuer annotation is not managed",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = map[string]string{
					v1beta1constants.AnnotationAuthenticationIssuer: "foo",
				}
			},
			false,
		),
		Entry("returns true when the issuer annotation is managed",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = map[string]string{
					v1beta1constants.AnnotationAuthenticationIssuer: v1beta1constants.AnnotationAuthenticationIssuerManaged,
				}
			},
			true,
		),
		Entry("returns false when the service account issuer is explicitly nil",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = map[string]string{}
				cluster.Shoot.Spec.Kubernetes.KubeAPIServer = &gardencorev1beta1.KubeAPIServerConfig{
					ServiceAccountConfig: &gardencorev1beta1.ServiceAccountConfig{},
				}
			},
			false,
		),
		Entry("returns true when the service account issuer is set",
			true,
			func(cluster *extensionscontroller.Cluster) {
				cluster.Shoot.Annotations = map[string]string{}
				cluster.Shoot.Spec.Kubernetes.KubeAPIServer = &gardencorev1beta1.KubeAPIServerConfig{
					ServiceAccountConfig: &gardencorev1beta1.ServiceAccountConfig{
						Issuer: new("foo"),
					},
				}
			},
			true,
		),
	)
})
