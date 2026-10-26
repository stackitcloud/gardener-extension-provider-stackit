package selfhostedshootexposure

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/extensions"
	"github.com/gardener/gardener/pkg/logger"
	gardenerutils "github.com/gardener/gardener/pkg/utils"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/uuid"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/selfhostedshootexposure"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

const (
	dnsServer  = "1.1.1.1"
	workerCIDR = "10.250.0.0/16"

	// exposureName is the name of the SelfHostedShootExposure resource.
	// The resulting LB name will be: "<technicalID>-exp-<exposureName>"
	// or "<technicalID>-exp-<exposureName-short>-<exposureName-hash> if the name is too long."
	exposureName = "apiserver"

	defaultPlanID     = "p10"
	alternativePlanID = "p250"

	updateTimeout = 2 * time.Minute
	pollInterval  = 5 * time.Second
)

var (
	stackitProjectID      string
	stackitServiceAccount string
	region                = flag.String("region", "eu01", "Region")
)

var (
	ctx = context.Background()
	log logr.Logger

	testEnv   *envtest.Environment
	mgrCancel context.CancelFunc
	c         client.Client

	encoder    runtime.Encoder
	iaasClient stackitclient.IaaSClient
	lbClient   stackitclient.LoadBalancingClient
	endpoints  stackitv1alpha1.APIEndpoints

	testID = string(uuid.NewUUID())
)

func validateEnvs() error {
	requiredVars := []string{
		"STACKIT_PROJECT_ID",
		"STACKIT_SERVICE_ACCOUNT_KEY",
	}

	for _, varName := range requiredVars {
		if os.Getenv(varName) == "" {
			return fmt.Errorf("error: environment variable '%s' is not set", varName)
		}
	}

	return nil
}

var _ = BeforeSuite(func() {
	flag.Parse()

	var err error
	stackitProjectID = os.Getenv("STACKIT_PROJECT_ID")
	stackitServiceAccount = os.Getenv("STACKIT_SERVICE_ACCOUNT_KEY")

	credentials := &stackit.Credentials{
		ProjectID: stackitProjectID,
		SaKeyJSON: stackitServiceAccount,
	}
	endpoints = stackitv1alpha1.APIEndpoints{
		IaaS: new("https://iaas.api.stackit.cloud"),
	}

	Expect(*region).NotTo(BeEmpty())
	Expect(validateEnvs()).To(Succeed())

	iaasClient, err = stackitclient.NewIaaSClient(*region, endpoints, credentials, "")
	Expect(err).NotTo(HaveOccurred())

	lbClient, err = stackitclient.NewLoadBalancingClient(ctx, *region, endpoints, credentials, "")
	Expect(err).NotTo(HaveOccurred())

	repoRoot := filepath.Join("..", "..", "..")

	logf.SetLogger(logger.MustNewZapLogger(logger.DebugLevel, logger.FormatJSON, zap.WriteTo(GinkgoWriter)))
	log = logf.Log.WithName("selfhostedshootexposure-test")

	DeferCleanup(func() {
		By("stopping manager")
		mgrCancel()

		By("stopping test environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("starting test environment")
	testEnv = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths: []string{
				filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_clusters.yaml"),
				filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_infrastructures.yaml"),
				filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_selfhostedshootexposures.yaml"),
			},
		},
	}

	restConfig, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(restConfig).ToNot(BeNil())

	scheme := runtime.NewScheme()
	Expect(schemev1.AddToScheme(scheme)).To(Succeed())
	Expect(extensionsv1alpha1.AddToScheme(scheme)).To(Succeed())
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
				&extensionsv1alpha1.SelfHostedShootExposure{}: {
					Label: labels.SelectorFromSet(labels.Set{"test-id": testID}),
				},
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(selfhostedshootexposure.AddToManagerWithOptions(mgr, selfhostedshootexposure.AddOptions{
		Controller: controller.Options{
			MaxConcurrentReconciles: 5,
		},
	})).To(Succeed())

	var mgrContext context.Context
	mgrContext, mgrCancel = context.WithCancel(ctx)

	By("start manager")
	go func() {
		defer GinkgoRecover()
		err := mgr.Start(mgrContext)
		Expect(err).NotTo(HaveOccurred())
	}()

	c = mgr.GetClient()
	Expect(c).NotTo(BeNil())

	gv := schema.GroupVersions{
		stackitv1alpha1.SchemeGroupVersion,
		gardenerv1beta1.SchemeGroupVersion,
	}
	encoder = serializer.NewCodecFactory(mgr.GetScheme()).EncoderForVersion(&json.Serializer{}, gv)
})

// Reuse a single STACKIT LB, run the tests in order to ensure a consistent state.
var _ = Describe("SelfHostedShootExposure", Serial, Ordered, func() {
	var (
		namespaceName string
		networkID     string
		lbName        string
		exposure      *extensionsv1alpha1.SelfHostedShootExposure

		// externalAddress is recorded while the LB exists, to verify the IP is released on deletion.
		externalAddress string
	)

	// getLB fetches the current LB and asserts it exists and carries a version.
	getLB := func(g Gomega) *loadbalancer.LoadBalancer {
		GinkgoHelper()
		lb, err := lbClient.GetLoadBalancer(ctx, lbName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(lb).NotTo(BeNil())
		g.Expect(lb.Version).NotTo(BeNil())
		return lb
	}

	// updateExposure applies mutate to the exposure, triggers a reconcile, and waits until the LB
	// applied the changes.
	updateExposure := func(mutate func(), verify func(g Gomega, lb *loadbalancer.LoadBalancer)) {
		GinkgoHelper()
		var previousVersion string
		Eventually(func(g Gomega) {
			previousVersion = *getLB(g).Version
		}).WithTimeout(updateTimeout).WithPolling(pollInterval).Should(Succeed())

		patchExposureReconcile(exposure, mutate)

		Eventually(func(g Gomega) {
			lb := getLB(g)
			g.Expect(*lb.Version).NotTo(Equal(previousVersion))
			verify(g, lb)
		}).WithTimeout(updateTimeout).WithPolling(pollInterval).Should(Succeed())
	}

	BeforeAll(func() {
		suffix, err := gardenerutils.GenerateRandomStringFromCharset(5, "0123456789abcdefghijklmnopqrstuvwxyz")
		Expect(err).NotTo(HaveOccurred())
		namespaceName = "stackit--exp-it--" + suffix
		// ResourceName = "<technicalID>-exp-<exposureName>"
		lbName = namespaceName + "-exp-" + exposureName

		By("create isolated network")
		network, err := iaasClient.CreateIsolatedNetwork(ctx, iaas.CreateIsolatedNetworkPayload{
			Name: namespaceName + "-network",
			Dhcp: new(true),
			Ipv4: &iaas.CreateNetworkIPv4{
				CreateNetworkIPv4WithPrefix: &iaas.CreateNetworkIPv4WithPrefix{
					Nameservers: []string{dnsServer},
					Prefix:      workerCIDR,
				},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		networkID = network.Id

		By("create namespace")
		Expect(c.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
		})).To(Succeed())

		By("create cloudprovider secret")
		Expect(c.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "cloudprovider", Namespace: namespaceName},
			Data: map[string][]byte{
				stackit.SaKeyJSON: []byte(stackitServiceAccount),
				stackit.ProjectID: []byte(stackitProjectID),
			},
		})).To(Succeed())

		By("create cluster")
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{Name: "shoot"},
			Spec:       gardenerv1beta1.ShootSpec{Region: *region},
			Status:     gardenerv1beta1.ShootStatus{TechnicalID: namespaceName},
		}
		shootBytes := new(bytes.Buffer)
		Expect(encoder.Encode(shoot, shootBytes)).To(Succeed())

		Expect(c.Create(ctx, &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: namespaceName},
			Spec: extensionsv1alpha1.ClusterSpec{
				CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
				Seed:         &runtime.RawExtension{Raw: []byte("{}")},
				Shoot:        runtime.RawExtension{Raw: shootBytes.Bytes()},
			},
		})).To(Succeed())

		By("create infrastructure with status containing network ID")
		infraStatusBytes := new(bytes.Buffer)
		Expect(encoder.Encode(&stackitv1alpha1.InfrastructureStatus{
			Networks: stackitv1alpha1.NetworkStatus{ID: networkID},
		}, infraStatusBytes)).To(Succeed())

		infra := &extensionsv1alpha1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{Name: shoot.Name, Namespace: namespaceName},
			Spec: extensionsv1alpha1.InfrastructureSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{Type: "stackit"},
				SecretRef:   corev1.SecretReference{Name: "cloudprovider", Namespace: namespaceName},
				Region:      *region,
			},
		}
		Expect(c.Create(ctx, infra)).To(Succeed())

		patch := client.MergeFrom(infra.DeepCopy())
		infra.Status = extensionsv1alpha1.InfrastructureStatus{
			DefaultStatus: extensionsv1alpha1.DefaultStatus{
				ProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes.Bytes()},
			},
		}
		Expect(c.Status().Patch(ctx, infra, patch)).To(Succeed())

		By("create SelfHostedShootExposure with two endpoints")
		exposure = &extensionsv1alpha1.SelfHostedShootExposure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exposureName,
				Namespace: namespaceName,
				Labels:    map[string]string{"test-id": testID},
			},
			Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{Type: "stackit"},
				Port:        6443,
				CredentialsRef: &corev1.ObjectReference{
					APIVersion: "v1",
					Kind:       "Secret",
					Name:       "cloudprovider",
					Namespace:  namespaceName,
				},
				Endpoints: []extensionsv1alpha1.ControlPlaneEndpoint{
					endpoint("node-1", "10.250.0.10"),
					endpoint("node-2", "10.250.0.11"),
				},
			},
		}
		Expect(c.Create(ctx, exposure)).To(Succeed())
	})

	AfterAll(func() {
		if namespaceName == "" {
			return
		}

		// Delete the SelfHostedShootExposure CR first so the controller stops reconciling and
		// recreating the LB.
		cr := &extensionsv1alpha1.SelfHostedShootExposure{
			ObjectMeta: metav1.ObjectMeta{Name: exposureName, Namespace: namespaceName},
		}
		Expect(client.IgnoreNotFound(c.Delete(ctx, cr))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(cr), cr))
		}).WithTimeout(5*time.Minute).WithPolling(pollInterval).Should(BeTrue(), "exposure CR was not deleted")

		// Safety-net: poll until the LB is fully deleted from STACKIT. Normally the controller's
		// Delete flow already removed it; this catches orphans from failed runs.
		Eventually(func() error {
			lb, err := lbClient.GetLoadBalancer(ctx, lbName)
			if stackitclient.IsNotFound(err) || lb == nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("getting load balancer: %w", err)
			}
			log.Info("Cleaning up leftover load balancer", "name", lbName)
			if delErr := lbClient.DeleteLoadBalancer(ctx, lbName); delErr != nil && !stackitclient.IsNotFound(delErr) {
				return fmt.Errorf("deleting load balancer: %w", delErr)
			}
			return fmt.Errorf("load balancer still exists, waiting for deletion")
		}).WithTimeout(10 * time.Minute).WithPolling(pollInterval).Should(Succeed())

		if networkID != "" {
			log.Info("Cleaning up network", "id", networkID)
			Eventually(func() error {
				return iaasClient.DeleteNetwork(ctx, networkID)
			}).WithTimeout(2 * time.Minute).WithPolling(pollInterval).Should(Succeed())
		}
	})

	// We never wait for the SelfHostedShootExposure to become Ready: the endpoint IPs do not point at
	// real VMs, so the STACKIT LB's own target health probe drives the LB into
	// STATUS_ERROR/TYPE_TARGET_NOT_ACTIVE and checkLoadBalancerReady requeues indefinitely. We
	// instead assert on the LB via the STACKIT API — readiness classification is covered by unit tests.
	It("creates the load balancer with the expected spec", func() {
		Eventually(func(g Gomega) {
			lb := getLB(g)

			g.Expect(lb.Name).To(HaveValue(Equal(lbName)))

			// Labels — validates the '/'-rejection workaround (flat dot-separated keys)
			g.Expect(lb.Labels).To(HaveValue(HaveKeyWithValue("cluster.stackit.cloud", namespaceName)))
			g.Expect(lb.Labels).To(HaveValue(HaveKeyWithValue("exposure.stackit.cloud", exposureName)))

			g.Expect(lb.Networks).To(HaveLen(1))
			g.Expect(lb.Networks[0].NetworkId).To(HaveValue(Equal(networkID)))
			g.Expect(lb.Networks[0].Role).To(HaveValue(Equal(loadbalancer.NETWORKROLE_ROLE_LISTENERS_AND_TARGETS)))

			g.Expect(lb.Listeners).To(HaveLen(1))
			g.Expect(lb.Listeners[0].DisplayName).To(HaveValue(Equal("control-plane")))
			g.Expect(lb.Listeners[0].Port).To(HaveValue(BeEquivalentTo(6443)))
			g.Expect(lb.Listeners[0].Protocol).To(HaveValue(Equal(loadbalancer.LISTENERPROTOCOL_PROTOCOL_TCP)))
			g.Expect(lb.Listeners[0].TargetPool).To(HaveValue(Equal("control-plane")))

			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Name).To(HaveValue(Equal("control-plane")))
			g.Expect(lb.TargetPools[0].TargetPort).To(HaveValue(BeEquivalentTo(6443)))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(2))
			for _, t := range lb.TargetPools[0].Targets {
				g.Expect(t.DisplayName).NotTo(BeNil())
			}
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.11"))

			// Default plan, since no providerConfig.LoadBalancer.PlanId is set
			g.Expect(lb.PlanId).To(HaveValue(Equal(defaultPlanID)))

			// EphemeralAddress is set on create
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.EphemeralAddress).To(HaveValue(BeTrue()))

			// STACKIT assigns the ephemeral address even while the LB is stuck in STATUS_ERROR.
			g.Expect(lb.ExternalAddress).To(HaveValue(Not(BeEmpty())))

			// We don't require READY, but TERMINATING would be wrong.
			g.Expect(lb.Status).NotTo(BeNil())
			g.Expect(*lb.Status).NotTo(Equal(loadbalancer.LOADBALANCERSTATUS_STATUS_TERMINATING))

			// Given the fake target IPs, the only expected error is TYPE_TARGET_NOT_ACTIVE. Any other
			// type means the extension produced an unexpected LB spec.
			if *lb.Status == loadbalancer.LOADBALANCERSTATUS_STATUS_ERROR {
				g.Expect(lb.Errors).NotTo(BeEmpty())
				for _, e := range lb.Errors {
					g.Expect(e.Type).To(HaveValue(Equal(loadbalancer.LOADBALANCERERRORTYPE_TYPE_TARGET_NOT_ACTIVE)))
				}
			}
		}).WithTimeout(5 * time.Minute).WithPolling(pollInterval).Should(Succeed())
	})

	It("is being reconciled by the controller", func() {
		// The finalizer proves Gardener bound the controller to this resource, may take a while for
		// the cache to update.
		Eventually(func(g Gomega) {
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(exposure), exposure)).To(Succeed())
			g.Expect(exposure.Finalizers).NotTo(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

		// Status.Ingress is only populated once the LB reaches READY; with fake targets we never
		// get there, so it stays empty.
		Expect(exposure.Status.Ingress).To(BeEmpty())
	})

	It("adds a target when an endpoint is added", func() {
		updateExposure(func() {
			exposure.Spec.Endpoints = append(exposure.Spec.Endpoints, endpoint("node-3", "10.250.0.12"))
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.11", "10.250.0.12"))

			// Invariants that must not shift during a target-only update.
			g.Expect(lb.Listeners).To(HaveLen(1))
			g.Expect(lb.Listeners[0].Port).To(HaveValue(BeEquivalentTo(6443)))
			g.Expect(lb.Networks).To(HaveLen(1))
			g.Expect(lb.Networks[0].NetworkId).To(HaveValue(Equal(networkID)))
			g.Expect(lb.PlanId).To(HaveValue(Equal(defaultPlanID)))
		})
	})

	It("does not write to the LB on a no-op reconcile", func() {
		var version string
		Eventually(func(g Gomega) {
			version = *getLB(g).Version
		}).WithTimeout(updateTimeout).WithPolling(pollInterval).Should(Succeed())

		patchExposureReconcile(exposure, func() {})

		Consistently(func(g Gomega) {
			g.Expect(*getLB(g).Version).To(Equal(version))
		}).WithTimeout(30 * time.Second).WithPolling(pollInterval).Should(Succeed())
	})

	It("changes the port on listener and target pool (mutable per SHSE contract)", func() {
		updateExposure(func() {
			exposure.Spec.Port = 8443
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.Listeners).To(HaveLen(1))
			g.Expect(lb.Listeners[0].Port).To(HaveValue(BeEquivalentTo(8443)))
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].TargetPort).To(HaveValue(BeEquivalentTo(8443)))

			// Targets + plan untouched by a port-only change.
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
			g.Expect(lb.PlanId).To(HaveValue(Equal(defaultPlanID)))
		})
	})

	It("removes a target when an endpoint is removed", func() {
		updateExposure(func() {
			// Drop node-2 (10.250.0.11), leaving node-1 and node-3.
			exposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				endpoint("node-1", "10.250.0.10"),
				endpoint("node-3", "10.250.0.12"),
			}
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.12"))
			// Plan unchanged by a pure target update.
			g.Expect(lb.PlanId).To(HaveValue(Equal(defaultPlanID)))
		})
	})

	It("changes the plan only (full PUT update)", func() {
		updateExposure(func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new("p50")})
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.PlanId).To(HaveValue(Equal("p50")))

			// Targets untouched by a plan-only change.
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.12"))
		})
	})

	It("changes plan and endpoints together (full PUT update)", func() {
		updateExposure(func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new(alternativePlanID)})
			// Add node-2 back, so targets *and* plan change in the same reconcile.
			exposure.Spec.Endpoints = append(exposure.Spec.Endpoints, endpoint("node-2", "10.250.0.11"))
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.PlanId).To(HaveValue(Equal(alternativePlanID)))
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.11", "10.250.0.12"))
		})
	})

	It("adds AccessControl.AllowedSourceRanges (full PUT update)", func() {
		updateExposure(func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{
				PlanID: new(alternativePlanID),
				AccessControl: &stackitv1alpha1.AccessControl{
					AllowedSourceRanges: []string{"0.0.0.0/0"},
				},
			})
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(ConsistOf("0.0.0.0/0"))

			// Plan + targets unchanged by an AccessControl-only addition.
			g.Expect(lb.PlanId).To(HaveValue(Equal(alternativePlanID)))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
		})
	})

	It("updates AllowedSourceRanges to a different set (order-independent)", func() {
		updateExposure(func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{
				PlanID: new(alternativePlanID),
				AccessControl: &stackitv1alpha1.AccessControl{
					AllowedSourceRanges: []string{"192.168.0.0/16", "10.0.0.0/8"},
				},
			})
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(ConsistOf("10.0.0.0/8", "192.168.0.0/16"))
		})
	})

	It("removes AccessControl (full PUT update)", func() {
		updateExposure(func() {
			// Drop AccessControl entirely from the providerConfig.
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new(alternativePlanID)})
		}, func(g Gomega, lb *loadbalancer.LoadBalancer) {
			// After removal, the LB API may report Options.AccessControl as nil OR with an empty
			// AllowedSourceRanges slice; both mean "unrestricted".
			if lb.Options != nil && lb.Options.AccessControl != nil {
				g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(BeEmpty())
			}

			// Plan + targets unchanged.
			g.Expect(lb.PlanId).To(HaveValue(Equal(alternativePlanID)))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
		})
	})

	It("keeps the external IP ephemeral after the updates", func() {
		// Sending the address of an ephemeral LB on update would promote its IP to a static one,
		// which STACKIT does not delete together with the LB.
		var lb *loadbalancer.LoadBalancer
		Eventually(func(g Gomega) {
			lb = getLB(g)
		}).WithTimeout(updateTimeout).WithPolling(pollInterval).Should(Succeed())

		Expect(lb.Options).NotTo(BeNil())
		Expect(lb.Options.EphemeralAddress).To(HaveValue(BeTrue()))
		Expect(lb.ExternalAddress).To(HaveValue(Not(BeEmpty())))
		externalAddress = *lb.ExternalAddress
	})

	It("deletes the SelfHostedShootExposure", func() {
		Expect(client.IgnoreNotFound(c.Delete(ctx, exposure))).To(Succeed())
		Expect(extensions.WaitUntilExtensionObjectDeleted(
			ctx,
			c,
			log,
			exposure,
			"SelfHostedShootExposure",
			2*time.Second,
			10*time.Minute,
		)).To(Succeed())
	})

	It("deletes the load balancer", func() {
		Eventually(func(g Gomega) {
			lb, err := lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(stackitclient.IgnoreNotFoundError(err)).NotTo(HaveOccurred())
			g.Expect(lb).To(BeNil())
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	})

	It("releases the external IP together with the load balancer", func() {
		Expect(externalAddress).NotTo(BeEmpty())
		Eventually(func(g Gomega) {
			publicIPs, err := iaasClient.GetPublicIpByLabels(ctx, stackit.LabelSelector{})
			g.Expect(err).NotTo(HaveOccurred())
			for _, ip := range publicIPs {
				g.Expect(ip.GetIp()).NotTo(Equal(externalAddress), "public IP %s was not released", ip.GetId())
			}
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	})
})

// endpoint builds a control plane endpoint with a single internal IP.
func endpoint(nodeName, ip string) extensionsv1alpha1.ControlPlaneEndpoint {
	return extensionsv1alpha1.ControlPlaneEndpoint{
		NodeName:  nodeName,
		Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: ip}},
	}
}

// patchExposureReconcile re-reads the exposure, applies mutate, sets the gardener-operation
// reconcile annotation, and patches (force reconcile).
func patchExposureReconcile(exposure *extensionsv1alpha1.SelfHostedShootExposure, mutate func()) {
	GinkgoHelper()
	Expect(c.Get(ctx, client.ObjectKeyFromObject(exposure), exposure)).To(Succeed())
	patch := client.MergeFrom(exposure.DeepCopy())
	mutate()
	metav1.SetMetaDataAnnotation(&exposure.ObjectMeta, v1beta1constants.GardenerOperation, v1beta1constants.GardenerOperationReconcile)
	Expect(c.Patch(ctx, exposure, patch)).To(Succeed())
}

// targetIPs extracts the target IP strings from the LB's single target pool.
func targetIPs(lb *loadbalancer.LoadBalancer) []string {
	ips := make([]string, 0, len(lb.TargetPools[0].Targets))
	for _, t := range lb.TargetPools[0].Targets {
		if t.Ip != nil {
			ips = append(ips, *t.Ip)
		}
	}
	return ips
}

// setExposureLBConfig encodes a SelfHostedShootExposureConfig wrapping lbConfig and sets it
// as the exposure's ProviderConfig. Fully replaces any previous ProviderConfig.
func setExposureLBConfig(exposure *extensionsv1alpha1.SelfHostedShootExposure, lbConfig *stackitv1alpha1.LoadBalancer) {
	GinkgoHelper()
	buf := new(bytes.Buffer)
	Expect(encoder.Encode(&stackitv1alpha1.SelfHostedShootExposureConfig{
		LoadBalancer: lbConfig,
	}, buf)).To(Succeed())
	exposure.Spec.ProviderConfig = &runtime.RawExtension{Raw: buf.Bytes()}
}
