package stackit

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
	// The resulting LB name will be: "<technicalID>-exposure-<exposureName>"
	exposureName = "apiserver"
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

	iaasClient, err = stackitclient.NewIaaSClient(*region, endpoints, credentials)
	Expect(err).NotTo(HaveOccurred())

	lbClient, err = stackitclient.NewLoadBalancingClient(ctx, *region, endpoints, credentials)
	Expect(err).NotTo(HaveOccurred())

	repoRoot := filepath.Join("..", "..", "..", "..")

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

var _ = Describe("SelfHostedShootExposure tests", func() {
	var (
		namespaceName string
		networkID     string
		lbName        string
	)

	BeforeEach(func() {
		suffix, err := gardenerutils.GenerateRandomStringFromCharset(5, "0123456789abcdefghijklmnopqrstuvwxyz")
		Expect(err).NotTo(HaveOccurred())
		namespaceName = "stackit--exp-it--" + suffix
		// ResourceName = "<technicalID>-exposure-<exposureName>"
		lbName = namespaceName + "-exposure-" + exposureName
	})

	AfterEach(func() {
		// Delete the SelfHostedShootExposure CR first so the controller stops reconciling and
		// recreating the LB. controller-runtime keeps retrying failed reconciles with exponential
		// backoff regardless of the operation-annotation predicate — without this CR-level delete,
		// the LB cleanup below races against the controller's recreate loop.
		exposure := &extensionsv1alpha1.SelfHostedShootExposure{
			ObjectMeta: metav1.ObjectMeta{Name: exposureName, Namespace: namespaceName},
		}
		Expect(client.IgnoreNotFound(c.Delete(ctx, exposure))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(exposure), exposure))
		}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "exposure CR was not deleted")

		// Safety-net: poll until the LB is fully deleted from STACKIT. Normally the controller's
		// Delete flow above already removed it; this catches orphans from prior runs that crashed
		// before AfterEach could finish.
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
		}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		// Network deletion only succeeds once the LB has released the network, so this naturally
		// chains after the LB-deletion poll above.
		if networkID != "" {
			log.Info("Cleaning up network", "id", networkID)
			Eventually(func() error {
				return iaasClient.DeleteNetwork(ctx, networkID)
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		}
	})

	It("should create, update, and delete a load balancer for a self-hosted shoot exposure", func() {
		By("create isolated network")
		networkName := namespaceName + "-network"
		network, err := iaasClient.CreateIsolatedNetwork(ctx, iaas.CreateIsolatedNetworkPayload{
			Name: networkName,
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
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
		}
		Expect(c.Create(ctx, namespace)).To(Succeed())

		By("create cloudprovider secret")
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cloudprovider",
				Namespace: namespaceName,
			},
			Data: map[string][]byte{
				stackit.SaKeyJSON: []byte(stackitServiceAccount),
				stackit.ProjectID: []byte(stackitProjectID),
			},
		}
		Expect(c.Create(ctx, secret)).To(Succeed())

		By("create cluster")
		shoot := &gardenerv1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shoot",
			},
			Spec: gardenerv1beta1.ShootSpec{
				Region: *region,
			},
			Status: gardenerv1beta1.ShootStatus{
				TechnicalID: namespaceName,
			},
		}

		shootBytes := new(bytes.Buffer)
		Expect(encoder.Encode(shoot, shootBytes)).To(Succeed())

		cluster := &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceName,
			},
			Spec: extensionsv1alpha1.ClusterSpec{
				CloudProfile: runtime.RawExtension{Raw: []byte("{}")},
				Seed:         runtime.RawExtension{Raw: []byte("{}")},
				Shoot:        runtime.RawExtension{Raw: shootBytes.Bytes()},
			},
		}
		Expect(c.Create(ctx, cluster)).To(Succeed())

		By("create infrastructure with status containing network ID")
		infraStatus := &stackitv1alpha1.InfrastructureStatus{
			Networks: stackitv1alpha1.NetworkStatus{
				ID: networkID,
			},
		}
		infraStatusBytes := new(bytes.Buffer)
		Expect(encoder.Encode(infraStatus, infraStatusBytes)).To(Succeed())

		infra := &extensionsv1alpha1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shoot.Name,
				Namespace: namespaceName,
			},
			Spec: extensionsv1alpha1.InfrastructureSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: "stackit",
				},
				SecretRef: corev1.SecretReference{
					Name:      "cloudprovider",
					Namespace: namespaceName,
				},
				Region: *region,
			},
		}
		Expect(c.Create(ctx, infra)).To(Succeed())

		// Patch infrastructure status to include network ID
		patch := client.MergeFrom(infra.DeepCopy())
		infra.Status = extensionsv1alpha1.InfrastructureStatus{
			DefaultStatus: extensionsv1alpha1.DefaultStatus{
				ProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes.Bytes()},
			},
		}
		Expect(c.Status().Patch(ctx, infra, patch)).To(Succeed())

		By("create SelfHostedShootExposure with two endpoints")
		exposure := &extensionsv1alpha1.SelfHostedShootExposure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      exposureName,
				Namespace: namespaceName,
				Labels: map[string]string{
					"test-id": testID,
				},
			},
			Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: "stackit",
				},
				Port: 6443,
				CredentialsRef: &corev1.ObjectReference{
					APIVersion: "v1",
					Kind:       "Secret",
					Name:       "cloudprovider",
					Namespace:  namespaceName,
				},
				Endpoints: []extensionsv1alpha1.ControlPlaneEndpoint{
					{
						NodeName: "node-1",
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.250.0.10"},
						},
					},
					{
						NodeName: "node-2",
						Addresses: []corev1.NodeAddress{
							{Type: corev1.NodeInternalIP, Address: "10.250.0.11"},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, exposure)).To(Succeed())

		// We do not wait for the SelfHostedShootExposure to become Ready: the endpoint IPs
		// above do not point at real VMs, so the STACKIT LB's own target health probe drives
		// the LB into STATUS_ERROR/TYPE_TARGET_NOT_ACTIVE and checkLoadBalancerReady requeues
		// indefinitely. We instead assert on the LB spec via the STACKIT API — LB readiness
		// classification is covered by unit tests.
		By("verify load balancer was created with the expected spec via STACKIT API")
		var lb *loadbalancer.LoadBalancer
		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb).NotTo(BeNil())

			// Identity
			g.Expect(lb.Name).NotTo(BeNil())
			g.Expect(*lb.Name).To(Equal(lbName))
			g.Expect(lb.Version).NotTo(BeNil())

			// Labels — validates the '/'-rejection workaround (flat dot-separated keys)
			g.Expect(lb.Labels).NotTo(BeNil())
			g.Expect(*lb.Labels).To(HaveKeyWithValue("cluster.stackit.cloud", namespaceName))
			g.Expect(*lb.Labels).To(HaveKeyWithValue("exposure.stackit.cloud", exposureName))

			// Network
			g.Expect(lb.Networks).To(HaveLen(1))
			g.Expect(lb.Networks[0].NetworkId).NotTo(BeNil())
			g.Expect(*lb.Networks[0].NetworkId).To(Equal(networkID))
			g.Expect(lb.Networks[0].Role).NotTo(BeNil())
			g.Expect(*lb.Networks[0].Role).To(Equal("ROLE_LISTENERS_AND_TARGETS"))

			// Listener
			g.Expect(lb.Listeners).To(HaveLen(1))
			g.Expect(lb.Listeners[0].DisplayName).NotTo(BeNil())
			g.Expect(*lb.Listeners[0].DisplayName).To(Equal("control-plane"))
			g.Expect(lb.Listeners[0].Port).NotTo(BeNil())
			g.Expect(*lb.Listeners[0].Port).To(BeEquivalentTo(6443))
			g.Expect(lb.Listeners[0].Protocol).NotTo(BeNil())
			g.Expect(*lb.Listeners[0].Protocol).To(Equal("PROTOCOL_TCP"))
			g.Expect(lb.Listeners[0].TargetPool).NotTo(BeNil())
			g.Expect(*lb.Listeners[0].TargetPool).To(Equal("control-plane"))

			// Target pool
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Name).NotTo(BeNil())
			g.Expect(*lb.TargetPools[0].Name).To(Equal("control-plane"))
			g.Expect(lb.TargetPools[0].TargetPort).NotTo(BeNil())
			g.Expect(*lb.TargetPools[0].TargetPort).To(BeEquivalentTo(6443))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(2))

			targetIPs := make([]string, 0, len(lb.TargetPools[0].Targets))
			for _, t := range lb.TargetPools[0].Targets {
				g.Expect(t.Ip).NotTo(BeNil())
				g.Expect(t.DisplayName).NotTo(BeNil())
				targetIPs = append(targetIPs, *t.Ip)
			}
			g.Expect(targetIPs).To(ConsistOf("10.250.0.10", "10.250.0.11"))

			// Plan — default since no providerConfig.LoadBalancer.PlanId set
			g.Expect(lb.PlanId).NotTo(BeNil())
			g.Expect(*lb.PlanId).To(Equal("p10"))

			// Options — EphemeralAddress is set on create
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.EphemeralAddress).NotTo(BeNil())
			g.Expect(*lb.Options.EphemeralAddress).To(BeTrue())

			// External VIP — STACKIT assigns the ephemeral address even while the LB is
			// stuck in STATUS_ERROR (the API reports it even when not shown in the portal).
			g.Expect(lb.ExternalAddress).NotTo(BeNil())
			g.Expect(*lb.ExternalAddress).NotTo(BeEmpty())

			// Status — we don't require READY, but TERMINATING would be wrong
			g.Expect(lb.Status).NotTo(BeNil())
			g.Expect(*lb.Status).NotTo(Equal("STATUS_TERMINATING"))

			// If STACKIT reports STATUS_ERROR, the only expected cause given fake target IPs is
			// TYPE_TARGET_NOT_ACTIVE. Any other error type would mean the extension produced an
			// unexpected LB spec (and is a test failure worth investigating).
			if *lb.Status == "STATUS_ERROR" {
				g.Expect(lb.Errors).NotTo(BeEmpty())
				for _, e := range lb.Errors {
					g.Expect(e.Type).NotTo(BeNil())
					g.Expect(*e.Type).To(Equal("TYPE_TARGET_NOT_ACTIVE"))
				}
			}
		}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("verify SelfHostedShootExposure CR is being reconciled")
		// Finalizer proves Gardener bound the controller to this resource. The cache may take a
		// brief moment to observe it, so use Eventually instead of a plain Get.
		Eventually(func(g Gomega) {
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(exposure), exposure)).To(Succeed())
			g.Expect(exposure.Finalizers).NotTo(BeEmpty())
		}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
		// Status.Ingress is only populated once the LB reaches READY in the actuator; with
		// fake targets we never get there, so it stays empty.
		Expect(exposure.Status.Ingress).To(BeEmpty())

		// Snapshot the LB version so we can assert the update actually hit the API.
		initialVersion := *lb.Version

		By("update endpoints (add a third node)")
		patchExposureReconcile(exposure, func() {
			exposure.Spec.Endpoints = append(exposure.Spec.Endpoints, extensionsv1alpha1.ControlPlaneEndpoint{
				NodeName: "node-3",
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "10.250.0.12"},
				},
			})
		})

		By("verify load balancer targets were updated via STACKIT API")
		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.11", "10.250.0.12"))

			// Version must change on write (Version is opaque — equality comparison only, per SDK).
			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(initialVersion))

			// Invariants that must not have shifted during the target-only update.
			g.Expect(lb.Listeners).To(HaveLen(1))
			g.Expect(*lb.Listeners[0].Port).To(BeEquivalentTo(6443))
			g.Expect(lb.Networks).To(HaveLen(1))
			g.Expect(*lb.Networks[0].NetworkId).To(Equal(networkID))
			g.Expect(*lb.PlanId).To(Equal("p10"))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("no-op reconcile must not write to the LB")
		versionBeforeNoOp := *lb.Version
		patchExposureReconcile(exposure, func() {})
		Consistently(func(g Gomega) {
			lbCheck, err := lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lbCheck.Version).NotTo(BeNil())
			g.Expect(*lbCheck.Version).To(Equal(versionBeforeNoOp))
		}).WithTimeout(30 * time.Second).WithPolling(5 * time.Second).Should(Succeed())

		By("remove an endpoint")
		versionBeforeRemove := versionBeforeNoOp
		patchExposureReconcile(exposure, func() {
			// Drop node-2 (10.250.0.11), leaving node-1 and node-3.
			exposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{NodeName: "node-1", Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.250.0.10"}}},
				{NodeName: "node-3", Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.250.0.12"}}},
			}
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(2))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.12"))

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforeRemove))
			// Plan unchanged by a pure target update.
			g.Expect(*lb.PlanId).To(Equal("p10"))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("change plan only (full PUT update)")
		versionBeforePlanChange := *lb.Version
		patchExposureReconcile(exposure, func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new("p50")})
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.PlanId).NotTo(BeNil())
			g.Expect(*lb.PlanId).To(Equal("p50"))

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforePlanChange))

			// Targets untouched by a plan-only change.
			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(2))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.12"))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("change plan and endpoints together (full PUT update)")
		versionBeforeCombined := *lb.Version
		patchExposureReconcile(exposure, func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new("p250")})
			// Add node-2 back, so we're changing targets *and* plan in the same reconcile.
			exposure.Spec.Endpoints = append(exposure.Spec.Endpoints, extensionsv1alpha1.ControlPlaneEndpoint{
				NodeName:  "node-2",
				Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.250.0.11"}},
			})
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.PlanId).NotTo(BeNil())
			g.Expect(*lb.PlanId).To(Equal("p250"))

			g.Expect(lb.TargetPools).To(HaveLen(1))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
			g.Expect(targetIPs(lb)).To(ConsistOf("10.250.0.10", "10.250.0.11", "10.250.0.12"))

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforeCombined))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("add AccessControl.AllowedSourceRanges (full PUT update)")
		versionBeforeACLAdd := *lb.Version
		patchExposureReconcile(exposure, func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{
				PlanID: new("p250"),
				AccessControl: &stackitv1alpha1.AccessControl{
					AllowedSourceRanges: []string{"0.0.0.0/0"},
				},
			})
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(ConsistOf("0.0.0.0/0"))

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforeACLAdd))

			// Plan + targets unchanged by AccessControl-only addition.
			g.Expect(*lb.PlanId).To(Equal("p250"))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("update AllowedSourceRanges to a different set (order-independent)")
		versionBeforeACLUpdate := *lb.Version
		patchExposureReconcile(exposure, func() {
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{
				PlanID: new("p250"),
				AccessControl: &stackitv1alpha1.AccessControl{
					AllowedSourceRanges: []string{"192.168.0.0/16", "10.0.0.0/8"},
				},
			})
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(lb.Options).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl).NotTo(BeNil())
			g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(ConsistOf("10.0.0.0/8", "192.168.0.0/16"))

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforeACLUpdate))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("remove AccessControl (full PUT update)")
		versionBeforeACLRemove := *lb.Version
		patchExposureReconcile(exposure, func() {
			// Drop AccessControl entirely from the providerConfig.
			setExposureLBConfig(exposure, &stackitv1alpha1.LoadBalancer{PlanID: new("p250")})
		})

		Eventually(func(g Gomega) {
			var err error
			lb, err = lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(err).NotTo(HaveOccurred())
			// After removal, the LB API may report Options.AccessControl as nil OR with an empty
			// AllowedSourceRanges slice; both mean "unrestricted".
			if lb.Options != nil && lb.Options.AccessControl != nil {
				g.Expect(lb.Options.AccessControl.AllowedSourceRanges).To(BeEmpty())
			}

			g.Expect(lb.Version).NotTo(BeNil())
			g.Expect(*lb.Version).NotTo(Equal(versionBeforeACLRemove))

			// Plan + targets unchanged.
			g.Expect(*lb.PlanId).To(Equal("p250"))
			g.Expect(lb.TargetPools[0].Targets).To(HaveLen(3))
		}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

		By("delete SelfHostedShootExposure")
		Expect(client.IgnoreNotFound(c.Delete(ctx, exposure))).To(Succeed())

		By("wait until SelfHostedShootExposure is deleted")
		Expect(extensions.WaitUntilExtensionObjectDeleted(
			ctx,
			c,
			log,
			exposure,
			"SelfHostedShootExposure",
			2*time.Second,
			10*time.Minute,
		)).To(Succeed())

		By("verify load balancer was deleted via STACKIT API")
		Eventually(func(g Gomega) {
			lb, err := lbClient.GetLoadBalancer(ctx, lbName)
			g.Expect(stackitclient.IgnoreNotFoundError(err)).NotTo(HaveOccurred())
			g.Expect(lb).To(BeNil())
		}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	})
})

// patchExposureReconcile re-reads the exposure, applies mutate, sets the gardener-operation
// reconcile annotation, and patches. Used to nudge the controller into re-reconciling after
// a spec change (or with no spec change to exercise the no-op path).
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
