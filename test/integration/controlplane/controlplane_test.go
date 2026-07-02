// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/logger"
	gardenerenvtest "github.com/gardener/gardener/test/envtest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	runtimejson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var (
	ctx = context.Background()

	testEnv   *gardenerenvtest.GardenerTestEnvironment
	mgrCancel context.CancelFunc
	k8sclient client.Client
	encoder   runtime.Encoder

	testID = string(uuid.NewUUID())
)

var _ = BeforeSuite(func() {
	logf.SetLogger(logger.MustNewZapLogger(logger.DebugLevel, logger.FormatJSON, zap.WriteTo(GinkgoWriter)))

	repoRoot := filepath.Join("..", "..", "..")

	DeferCleanup(func() {
		By("stopping manager")
		mgrCancel()

		By("stopping test environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("starting test environment")
	testEnv = &gardenerenvtest.GardenerTestEnvironment{
		Environment: &envtest.Environment{
			CRDInstallOptions: envtest.CRDInstallOptions{
				Paths: []string{
					filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_clusters.yaml"),
					filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_controlplanes.yaml"),
					filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-resources.gardener.cloud_managedresources.yaml"),
					filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-autoscaling.k8s.io_verticalpodautoscalers.yaml"),
				},
			},
		},
		GardenerAPIServer: &gardenerenvtest.GardenerAPIServer{},
	}

	restConfig, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(restConfig).ToNot(BeNil())

	scheme := runtime.NewScheme()
	Expect(kubernetes.AddSeedSchemeToScheme(scheme)).To(Succeed())
	Expect(stackitv1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(gardenerv1beta1.AddToScheme(scheme)).To(Succeed())

	By("setup manager")
	mgr, err := manager.New(restConfig, manager.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&extensionsv1alpha1.ControlPlane{}: {
					Label: labels.SelectorFromSet(labels.Set{"test-id": testID}),
				},
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(controlplane.AddToManagerWithOptions(ctx, mgr, controlplane.AddOptions{
		Controller: controller.Options{
			MaxConcurrentReconciles: 1,
		},
	})).To(Succeed())

	var mgrCtx context.Context
	mgrCtx, mgrCancel = context.WithCancel(ctx)

	By("start manager")
	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(mgrCtx)).To(Succeed())
	}()

	k8sclient = mgr.GetClient()

	gv := schema.GroupVersions{
		stackitv1alpha1.SchemeGroupVersion,
		gardenerv1beta1.SchemeGroupVersion,
	}
	encoder = serializer.NewCodecFactory(scheme).EncoderForVersion(&runtimejson.Serializer{}, gv)
})

var _ = Describe("ControlPlane CSI compatibility mode", func() {
	var namespace string

	BeforeEach(func() {
		namespace = "shoot--test--" + testID[:8]

		By("create namespace")
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(k8sclient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, ns))).To(Succeed()) })

		By("create cluster")
		shoot := &gardenerv1beta1.Shoot{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "core.gardener.cloud/v1beta1",
				Kind:       "Shoot",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: gardenerv1beta1.ShootSpec{
				Kubernetes: gardenerv1beta1.Kubernetes{Version: "1.33.5"},
			},
			Status: gardenerv1beta1.ShootStatus{TechnicalID: namespace},
		}
		shootBytes := new(bytes.Buffer)
		Expect(encoder.Encode(shoot, shootBytes)).To(Succeed())

		cluster := &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
			Spec: extensionsv1alpha1.ClusterSpec{
				CloudProfile: runtime.RawExtension{Raw: []byte(`{}`)},
				Seed:         runtime.RawExtension{Raw: []byte(`{}`)},
				Shoot:        runtime.RawExtension{Raw: shootBytes.Bytes()},
			},
		}
		Expect(k8sclient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, cluster))).To(Succeed()) })

		By("create cloudprovider secret")
		cloudproviderSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cloudprovider", Namespace: namespace},
			Data: map[string][]byte{
				stackit.ProjectID: []byte("test-project-id"),
				stackit.SaKeyJSON: []byte(`{"key":"value"}`),
			},
		}
		Expect(k8sclient.Create(ctx, cloudproviderSecret)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, cloudproviderSecret))).To(Succeed()) })

		By("create cloud-provider-config secret")
		cpConfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: openstack.CloudProviderConfigName, Namespace: namespace},
			Data:       map[string][]byte{"config": []byte("placeholder")},
		}
		Expect(k8sclient.Create(ctx, cpConfigSecret)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, cpConfigSecret))).To(Succeed()) })

		By("create cloud-provider-disk-config-csi secret")
		diskConfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: openstack.CloudProviderCSIDiskConfigName, Namespace: namespace},
			Data:       map[string][]byte{"config": []byte("placeholder")},
		}
		Expect(k8sclient.Create(ctx, diskConfigSecret)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, diskConfigSecret))).To(Succeed()) })
	})

	It("should create both ManagedResources when compatibilityMode=compat", func() {
		cpConfig := &stackitv1alpha1.ControlPlaneConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "ControlPlaneConfig",
			},
			CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.STACKIT),
			},
			Storage: &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{
					Name:              string(stackitv1alpha1.STACKIT),
					CompatibilityMode: string(stackitv1alpha1.COMPAT),
				},
			},
		}
		cpConfigBytes, err := json.Marshal(cpConfig)
		Expect(err).NotTo(HaveOccurred())

		infraStatus := &stackitv1alpha1.InfrastructureStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "InfrastructureStatus",
			},
			Networks: stackitv1alpha1.NetworkStatus{
				ID:   "test-network-id",
				Name: "test-network-name",
				Router: stackitv1alpha1.RouterStatus{
					ID: "test-router-id",
				},
			},
			Node: stackitv1alpha1.NodeStatus{KeyName: "test-key"},
		}
		infraStatusBytes, err := json.Marshal(infraStatus)
		Expect(err).NotTo(HaveOccurred())

		By("create ControlPlane CR")
		cp := &extensionsv1alpha1.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controlplane",
				Namespace: namespace,
				Labels:    map[string]string{"test-id": testID},
			},
			Spec: extensionsv1alpha1.ControlPlaneSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type:           stackit.Type,
					ProviderConfig: &runtime.RawExtension{Raw: cpConfigBytes},
				},
				Region: "eu01",
				SecretRef: corev1.SecretReference{
					Name:      "cloudprovider",
					Namespace: namespace,
				},
				InfrastructureProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes},
			},
		}
		Expect(k8sclient.Create(ctx, cp)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, cp))).To(Succeed()) })

		By("wait for seed ManagedResource")
		seedMR := &resourcesv1alpha1.ManagedResource{}
		Eventually(func() error {
			return k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-chart",
				Namespace: namespace,
			}, seedMR)
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("wait for shoot ManagedResource")
		shootMR := &resourcesv1alpha1.ManagedResource{}
		Eventually(func() error {
			return k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-shoot-chart",
				Namespace: namespace,
			}, shootMR)
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
	})

	It("should delete both ManagedResources when transitioning from compat to default", func() {
		cpConfigCompat := &stackitv1alpha1.ControlPlaneConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "ControlPlaneConfig",
			},
			CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.STACKIT),
			},
			Storage: &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{
					Name:              string(stackitv1alpha1.STACKIT),
					CompatibilityMode: string(stackitv1alpha1.COMPAT),
				},
			},
		}
		cpConfigCompatBytes, err := json.Marshal(cpConfigCompat)
		Expect(err).NotTo(HaveOccurred())

		infraStatus := &stackitv1alpha1.InfrastructureStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "InfrastructureStatus",
			},
			Networks: stackitv1alpha1.NetworkStatus{
				ID:   "test-network-id",
				Name: "test-network-name",
				Router: stackitv1alpha1.RouterStatus{
					ID: "test-router-id",
				},
			},
			Node: stackitv1alpha1.NodeStatus{KeyName: "test-key"},
		}
		infraStatusBytes, err := json.Marshal(infraStatus)
		Expect(err).NotTo(HaveOccurred())

		By("create ControlPlane CR with compat mode")
		cp := &extensionsv1alpha1.ControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "controlplane",
				Namespace: namespace,
				Labels:    map[string]string{"test-id": testID},
			},
			Spec: extensionsv1alpha1.ControlPlaneSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type:           stackit.Type,
					ProviderConfig: &runtime.RawExtension{Raw: cpConfigCompatBytes},
				},
				Region: "eu01",
				SecretRef: corev1.SecretReference{
					Name:      "cloudprovider",
					Namespace: namespace,
				},
				InfrastructureProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes},
			},
		}
		Expect(k8sclient.Create(ctx, cp)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sclient.Delete(ctx, cp))).To(Succeed()) })

		By("wait for seed ManagedResource to be created")
		Eventually(func() error {
			return k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-chart",
				Namespace: namespace,
			}, &resourcesv1alpha1.ManagedResource{})
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("wait for shoot ManagedResource to be created")
		Eventually(func() error {
			return k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-shoot-chart",
				Namespace: namespace,
			}, &resourcesv1alpha1.ManagedResource{})
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())

		By("update ControlPlane to default compatibility mode")
		cpConfigDefault := &stackitv1alpha1.ControlPlaneConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "ControlPlaneConfig",
			},
			CloudControllerManager: &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.STACKIT),
			},
			Storage: &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{
					Name:              string(stackitv1alpha1.STACKIT),
					CompatibilityMode: string(stackitv1alpha1.DEFAULT),
				},
			},
		}
		cpConfigDefaultBytes, err := json.Marshal(cpConfigDefault)
		Expect(err).NotTo(HaveOccurred())

		By("trigger reconciliation")
		Expect(k8sclient.Get(ctx, types.NamespacedName{Name: cp.Name, Namespace: cp.Namespace}, cp)).To(Succeed())
		cp.Spec.ProviderConfig = &runtime.RawExtension{Raw: cpConfigDefaultBytes}
		metav1.SetMetaDataAnnotation(&cp.ObjectMeta, "gardener.cloud/operation", "reconcile")
		Expect(k8sclient.Update(ctx, cp)).To(Succeed())

		By("wait for seed ManagedResource to be deleted")
		Eventually(func() bool {
			err := k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-chart",
				Namespace: namespace,
			}, &resourcesv1alpha1.ManagedResource{})
			return apierrors.IsNotFound(err)
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())

		By("wait for shoot ManagedResource to be deleted")
		Eventually(func() bool {
			err := k8sclient.Get(ctx, types.NamespacedName{
				Name:      "stackit-compatibility-shoot-chart",
				Namespace: namespace,
			}, &resourcesv1alpha1.ManagedResource{})
			return apierrors.IsNotFound(err)
		}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(BeTrue())
	})
})
