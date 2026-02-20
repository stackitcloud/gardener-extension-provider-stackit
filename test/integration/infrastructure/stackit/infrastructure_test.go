// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

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
	testutils "github.com/gardener/gardener/pkg/utils/test"
	"github.com/gardener/gardener/test/framework"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/uuid"
	schemev1 "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	infrastructure "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/utils"
)

const (
	// We only use the "flow" and "recover" variants, see the calls to "testInfrastructure"
	reconcilerUseFlow      string = "flow"
	reconcilerRecoverState string = "recover"
)

const (
	workerCIDR = "10.250.0.0/16"
	dnsServer  = "1.1.1.1"
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

	decoder    runtime.Decoder
	iaasClient stackitclient.IaaSClient
	endpoints  stackitv1alpha1.APIEndpoints

	testID  = string(uuid.NewUUID())
	encoder runtime.Encoder
)

func validateFlags() {
	if len(*region) == 0 {
		panic("--region flag is not specified")
	}
}

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
		IaaS: ptr.To("https://iaas.api.stackit.cloud"),
	}

	validateFlags()
	err = validateEnvs()
	Expect(err).NotTo(HaveOccurred())

	// Disable STACKIT LB Deletion featureGate as this test does not create any LB
	// TODO: Consider creating manual STACKIT NLB to ensure stackit NLB deletion works
	DeferCleanup(testutils.WithFeatureGate(feature.MutableGate, feature.EnsureSTACKITLBDeletion, false))

	iaasClient, err = stackitclient.NewIaaSClient(*region, endpoints, credentials)
	Expect(err).NotTo(HaveOccurred())

	repoRoot := filepath.Join("..", "..", "..", "..")

	// enable manager logs
	logf.SetLogger(logger.MustNewZapLogger(logger.DebugLevel, logger.FormatJSON, zap.WriteTo(GinkgoWriter)))

	log = logf.Log.WithName("infrastructure-test")

	DeferCleanup(func() {
		By("stopping manager")
		mgrCancel()

		By("running cleanup actions")
		framework.RunCleanupActions()

		By("stopping test environment")
		Expect(testEnv.Stop()).To(Succeed())
	})

	By("starting test environment")
	testEnv = &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Paths: []string{
				filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_clusters.yaml"),
				filepath.Join(repoRoot, "test", "integration", "testdata", "upstream-crds", "10-crd-extensions.gardener.cloud_infrastructures.yaml"),
			},
		},
	}

	restConfig, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(restConfig).ToNot(BeNil())

	httpClient, err := rest.HTTPClientFor(restConfig)
	Expect(err).NotTo(HaveOccurred())
	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, httpClient)
	Expect(err).NotTo(HaveOccurred())

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
			Mapper: mapper,
			ByObject: map[client.Object]cache.ByObject{
				&extensionsv1alpha1.Infrastructure{}: {
					Label: labels.SelectorFromSet(labels.Set{"test-id": testID}),
				},
			},
		},
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(infrastructure.AddToManagerWithOptions(ctx, mgr, infrastructure.AddOptions{
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
	decoder = serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder()
	encoder = serializer.NewCodecFactory(mgr.GetScheme()).EncoderForVersion(&json.Serializer{}, gv)

	priorityClass := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1beta1constants.PriorityClassNameShootControlPlane300,
		},
		Description:   "PriorityClass for Shoot control plane components",
		GlobalDefault: false,
		Value:         999998300,
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, priorityClass))).To(Succeed())
})

var _ = Describe("Infrastructure tests flow", func() {
	testInfrastructure(ptr.To(reconcilerUseFlow))
})

var _ = Describe("Infrastructure tests recover", func() {
	testInfrastructure(ptr.To(reconcilerRecoverState))
})

func testInfrastructure(reconciler *string) {
	AfterEach(func() {
		framework.RunCleanupActions()
	})

	It("minimum configuration infrastructure", func() {
		providerConfig := newProviderConfig(nil)
		cloudProfileConfig := newCloudProfileConfig()
		namespace, err := generateNamespaceName()
		Expect(err).NotTo(HaveOccurred())

		err = runTest(ctx, log, c, namespace, false, providerConfig, decoder, cloudProfileConfig, reconciler)

		Expect(err).NotTo(HaveOccurred())
	})

	It("with infrastructure that uses existing network", func() {
		namespace, err := generateNamespaceName()
		Expect(err).NotTo(HaveOccurred())

		networkName := namespace + "-network"

		networkID, err := prepareIsolatedNetwork(log, networkName)
		Expect(err).NotTo(HaveOccurred())

		var cleanupHandle framework.CleanupActionHandle
		cleanupHandle = framework.AddCleanupAction(func() {
			err := teardownNetwork(log, *networkID)
			Expect(err).NotTo(HaveOccurred())

			framework.RemoveCleanupAction(cleanupHandle)
		})

		providerConfig := newProviderConfig(networkID)
		cloudProfileConfig := newCloudProfileConfig()

		err = runTest(ctx, log, c, namespace, false, providerConfig, decoder, cloudProfileConfig, reconciler)

		Expect(err).NotTo(HaveOccurred())
	})

	It("with fake SNA infrastructure", func() {
		namespace, err := generateNamespaceName()
		Expect(err).NotTo(HaveOccurred())

		networkName := namespace + "-network"

		networkID, err := prepareIsolatedNetwork(log, networkName)
		Expect(err).NotTo(HaveOccurred())

		var cleanupHandle framework.CleanupActionHandle
		cleanupHandle = framework.AddCleanupAction(func() {
			By("Tearing down network")
			err = teardownNetwork(log, *networkID)
			Expect(err).NotTo(HaveOccurred())

			framework.RemoveCleanupAction(cleanupHandle)
		})

		providerConfig := newProviderConfig(networkID)
		cloudProfileConfig := newCloudProfileConfig()

		err = runTest(ctx, log, c, namespace, true, providerConfig, decoder, cloudProfileConfig, reconciler)
		Expect(err).NotTo(HaveOccurred())
	})
}

func runTest(
	ctx context.Context,
	log logr.Logger,
	c client.Client,
	namespaceName string,
	snaShoot bool,
	providerConfig *stackitv1alpha1.InfrastructureConfig,
	decoder runtime.Decoder,
	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig,
	reconciler *string,
) error {
	var (
		namespace        *corev1.Namespace
		cluster          *extensionsv1alpha1.Cluster
		infra            *extensionsv1alpha1.Infrastructure
		infraIdentifiers infrastructureIdentifiers
	)

	var cleanupHandle framework.CleanupActionHandle
	cleanupHandle = framework.AddCleanupAction(func() {
		By("delete infrastructure")
		Expect(client.IgnoreNotFound(c.Delete(ctx, infra))).To(Succeed())

		By("wait until infrastructure is deleted")
		err := extensions.WaitUntilExtensionObjectDeleted(
			ctx,
			c,
			log,
			infra,
			"Infrastructure",
			2*time.Second,
			16*time.Minute,
		)
		Expect(err).NotTo(HaveOccurred())

		By("verify infrastructure deletion")
		verifyDeletion(infraIdentifiers, providerConfig)

		Expect(client.IgnoreNotFound(c.Delete(ctx, namespace))).To(Succeed())
		Expect(client.IgnoreNotFound(c.Delete(ctx, cluster))).To(Succeed())

		framework.RemoveCleanupAction(cleanupHandle)
	})

	By("create namespace for test execution")
	namespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	err := c.Create(ctx, namespace)
	Expect(err).NotTo(HaveOccurred())

	cloudProfileConfigJSON := new(bytes.Buffer)
	err = encoder.Encode(cloudProfileConfig, cloudProfileConfigJSON)
	Expect(err).NotTo(HaveOccurred())

	cloudprofile := gardenerv1beta1.CloudProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gardenerv1beta1.SchemeGroupVersion.String(),
			Kind:       "CloudProfile",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
		Spec: gardenerv1beta1.CloudProfileSpec{
			ProviderConfig: &runtime.RawExtension{
				Raw: cloudProfileConfigJSON.Bytes(),
			},
		},
	}

	cloudProfileJSON := new(bytes.Buffer)
	err = encoder.Encode(&cloudprofile, cloudProfileJSON)
	Expect(err).NotTo(HaveOccurred())

	By("create cluster")
	shoot := &gardenerv1beta1.Shoot{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				feature.ShootUseSTACKITAPIInfrastructureController: "true",
			},
		},
		Spec: gardenerv1beta1.ShootSpec{
			Region: *region,
			Networking: &gardenerv1beta1.Networking{
				Pods: ptr.To("10.123.0.0/24"),
			},
		},
		Status: gardenerv1beta1.ShootStatus{
			TechnicalID: namespaceName,
		},
	}
	Expect(shoot.Spec.Region).ToNot(BeEmpty())

	if snaShoot {
		shoot.Labels = map[string]string{"stackit.cloud/area-id": "area"}
	}

	shootBytes := new(bytes.Buffer)
	err = encoder.Encode(shoot, shootBytes)
	Expect(err).NotTo(HaveOccurred())

	cluster = &extensionsv1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
		Spec: extensionsv1alpha1.ClusterSpec{
			CloudProfile: runtime.RawExtension{
				Raw: cloudProfileJSON.Bytes(),
			},
			Seed: runtime.RawExtension{
				Raw: []byte("{}"),
			},
			Shoot: runtime.RawExtension{
				Raw: shootBytes.Bytes(),
			},
		},
	}

	err = c.Create(ctx, cluster)
	Expect(err).NotTo(HaveOccurred())

	By("deploy cloudprovider secret into namespace")
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
	if err := c.Create(ctx, secret); err != nil {
		return err
	}

	By("create infrastructure")
	infra, err = newInfrastructure(namespaceName, providerConfig)
	if err != nil {
		return err
	}

	if err := c.Create(ctx, infra); err != nil {
		return err
	}

	By("wait until infrastructure is created")
	Expect(extensions.WaitUntilExtensionObjectReady(
		ctx,
		c,
		log,
		infra,
		"Infrastucture",
		2*time.Second,
		6*time.Minute,
		16*time.Minute,
		nil,
	)).To(Succeed())

	// Update the infra resource to trigger a migration.
	oldState := infra.Status.State.DeepCopy()
	if *reconciler == reconcilerRecoverState {
		By("drop state for testing recovery")

		patch := client.MergeFrom(infra.DeepCopy())
		infra.Status.LastOperation = nil
		infra.Status.ProviderStatus = nil
		infra.Status.State = nil
		Expect(c.Status().Patch(ctx, infra, patch)).To(Succeed())

		Expect(c.Get(ctx, client.ObjectKey{Namespace: infra.Namespace, Name: infra.Name}, infra)).To(Succeed())

		patch = client.MergeFrom(infra.DeepCopy())
		metav1.SetMetaDataAnnotation(&infra.ObjectMeta, v1beta1constants.GardenerOperation, v1beta1constants.GardenerOperationReconcile)
		err = c.Patch(ctx, infra, patch)
		Expect(err).To(Succeed())
	}

	By("wait until infrastructure is reconciled")
	Expect(extensions.WaitUntilExtensionObjectReady(
		ctx,
		c,
		log,
		infra,
		"Infrastucture",
		2*time.Second,
		2*time.Minute,
		16*time.Minute,
		nil,
	)).To(Succeed())

	infraIdentifiers, providerStatus := verifyCreation(infra.Status, providerConfig)
	if snaShoot {
		Expect(infra.Status.NodesCIDR).To(HaveValue(Equal(workerCIDR)))
	}

	if *reconciler == reconcilerRecoverState {
		By("check state recovery")
		Expect(infra.Status.State).To(Equal(oldState))
		newProviderStatus := stackitv1alpha1.InfrastructureStatus{}
		if _, _, err := decoder.Decode(infra.Status.ProviderStatus.Raw, nil, &newProviderStatus); err != nil {
			return err
		}
		Expect(newProviderStatus).To(Equal(providerStatus))
	}

	return nil
}

// newProviderConfig creates a providerConfig with the network and router details.
// If routerID is set to "", it requests a new router creation.
// Else it reuses the supplied routerID.
func newProviderConfig(networkID *string) *stackitv1alpha1.InfrastructureConfig {
	return &stackitv1alpha1.InfrastructureConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "InfrastructureConfig",
		},
		Networks: stackitv1alpha1.Networks{
			ID:      networkID,
			Workers: workerCIDR,
		},
	}
}

func newCloudProfileConfig() *stackitv1alpha1.CloudProfileConfig {
	return &stackitv1alpha1.CloudProfileConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "CloudProfileConfig",
		},
		APIEndpoints: &endpoints,
		DNSServers:   []string{dnsServer},
	}
}

func newInfrastructure(namespace string, providerConfig *stackitv1alpha1.InfrastructureConfig) (*extensionsv1alpha1.Infrastructure, error) {
	const sshPublicKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDcSZKq0lM9w+ElLp9I9jFvqEFbOV1+iOBX7WEe66GvPLOWl9ul03ecjhOf06+FhPsWFac1yaxo2xj+SJ+FVZ3DdSn4fjTpS9NGyQVPInSZveetRw0TV0rbYCFBTJuVqUFu6yPEgdcWq8dlUjLqnRNwlelHRcJeBfACBZDLNSxjj0oUz7ANRNCEne1ecySwuJUAz3IlNLPXFexRT0alV7Nl9hmJke3dD73nbeGbQtwvtu8GNFEoO4Eu3xOCKsLw6ILLo4FBiFcYQOZqvYZgCb4ncKM52bnABagG54upgBMZBRzOJvWp0ol+jK3Em7Vb6ufDTTVNiQY78U6BAlNZ8Xg+LUVeyk1C6vWjzAQf02eRvMdfnRCFvmwUpzbHWaVMsQm8gf3AgnTUuDR0ev1nQH/5892wZA86uLYW/wLiiSbvQsqtY1jSn9BAGFGdhXgWLAkGsd/E1vOT+vDcor6/6KjHBm0rG697A3TDBRkbXQ/1oFxcM9m17RteCaXuTiAYWMqGKDoJvTMDc4L+Uvy544pEfbOH39zfkIYE76WLAFPFsUWX6lXFjQrX3O7vEV73bCHoJnwzaNd03PSdJOw+LCzrTmxVezwli3F9wUDiBRB0HkQxIXQmncc1HSecCKALkogIK+1e1OumoWh6gPdkF4PlTMUxRitrwPWSaiUIlPfCpQ== your_email@example.com"

	providerConfigJSON := new(bytes.Buffer)
	err := encoder.Encode(providerConfig, providerConfigJSON)
	if err != nil {
		return nil, err
	}

	infra := &extensionsv1alpha1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "infrastructure",
			Namespace: namespace,
			Labels: map[string]string{
				"test-id": testID,
			},
		},
		Spec: extensionsv1alpha1.InfrastructureSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: stackit.Type,
				ProviderConfig: &runtime.RawExtension{
					Raw: providerConfigJSON.Bytes(),
				},
			},
			SecretRef: corev1.SecretReference{
				Name:      "cloudprovider",
				Namespace: namespace,
			},
			Region:       *region,
			SSHPublicKey: []byte(sshPublicKey),
		},
	}
	return infra, nil
}

func generateNamespaceName() (string, error) {
	suffix, err := gardenerutils.GenerateRandomStringFromCharset(5, "0123456789abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		return "", err
	}

	return "stackit--infra-it--" + suffix, nil
}

func prepareIsolatedNetwork(log logr.Logger, networkName string) (*string, error) {
	log.Info("Waiting until network is created", "networkName", networkName)

	createOpts := iaas.CreateIsolatedNetworkPayload{
		Name: ptr.To(networkName),
		Ipv4: &iaas.CreateNetworkIPv4{
			CreateNetworkIPv4WithPrefix: &iaas.CreateNetworkIPv4WithPrefix{
				Nameservers: ptr.To([]string{dnsServer}),
				Prefix:      ptr.To(workerCIDR),
			},
		},
	}
	network, err := iaasClient.CreateIsolatedNetwork(ctx, createOpts)
	if err != nil {
		return nil, err
	}

	log.Info("Network is created", "networkName", networkName)
	return network.Id, nil
}

func teardownNetwork(log logr.Logger, networkID string) error {
	log.Info("Waiting until network is deleted", "networkID", networkID)

	err := iaasClient.DeleteNetwork(ctx, networkID)
	if err != nil {
		return err
	}

	log.Info("Network is deleted", "networkID", networkID)
	return nil
}

type infrastructureIdentifiers struct {
	networkID  *string
	keyPair    *string
	secGroupID *string
}

func verifyCreation(infraStatus extensionsv1alpha1.InfrastructureStatus, providerConfig *stackitv1alpha1.InfrastructureConfig) (infrastructureIdentifier infrastructureIdentifiers, providerStatus stackitv1alpha1.InfrastructureStatus) {
	_, _, err := decoder.Decode(infraStatus.ProviderStatus.Raw, nil, &providerStatus)
	Expect(err).NotTo(HaveOccurred())

	net, err := iaasClient.GetNetworkById(ctx, providerStatus.Networks.ID)
	Expect(err).NotTo(HaveOccurred())

	var externalFixedIPs []string
	ip, ok := net.Ipv4.GetPublicIpOk()
	if ok {
		externalFixedIPs = append(externalFixedIPs, ip)
	}

	// verify router ip in status
	Expect(ip).NotTo(BeEmpty())
	Expect(providerStatus.Networks.Router.ExternalFixedIPs).To(ContainElements(externalFixedIPs))

	// network is created
	Expect(err).NotTo(HaveOccurred())
	Expect(net).NotTo(BeNil())

	if providerConfig.Networks.ID != nil {
		Expect(net.GetId()).To(Equal(*providerConfig.Networks.ID))
	}
	infrastructureIdentifier.networkID = ptr.To(net.GetId())

	// security group is created
	secGroup, err := iaasClient.GetSecurityGroupById(ctx, providerStatus.SecurityGroups[0].ID)
	Expect(err).NotTo(HaveOccurred())
	Expect(secGroup.GetName()).To(Equal(providerStatus.SecurityGroups[0].Name))
	infrastructureIdentifier.secGroupID = ptr.To(secGroup.GetId())

	// keypair is created
	keyPair, err := iaasClient.GetKeypair(ctx, providerStatus.Node.KeyName)
	Expect(err).NotTo(HaveOccurred())
	infrastructureIdentifier.keyPair = ptr.To(keyPair.GetName())

	// verify egressCIDRs
	Expect(infraStatus.EgressCIDRs).To(ContainElements(utils.ComputeEgressCIDRs(providerStatus.Networks.Router.ExternalFixedIPs)))

	return infrastructureIdentifier, providerStatus
}

func verifyDeletion(infrastructureIdentifier infrastructureIdentifiers, providerConfig *stackitv1alpha1.InfrastructureConfig) {
	Eventually(func(g Gomega) {
		// keypair doesn't exist
		keyPair, _ := iaasClient.GetKeypair(ctx, ptr.Deref(infrastructureIdentifier.keyPair, ""))
		g.Expect(keyPair).To(BeNil())

		if infrastructureIdentifier.networkID != nil {
			if providerConfig.Networks.ID == nil {
				// make sure network doesn't exist, if it wasn't present before
				_, err := iaasClient.GetNetworkById(ctx, *infrastructureIdentifier.networkID)
				g.Expect(stackitclient.IgnoreNotFoundError(err)).NotTo(HaveOccurred())
				g.Expect(stackitclient.IsNotFound(err)).To(BeTrue())
			}
		}

		if infrastructureIdentifier.secGroupID != nil {
			// security group doesn't exist
			_, err := iaasClient.GetSecurityGroupById(ctx, *infrastructureIdentifier.secGroupID)
			g.Expect(stackitclient.IgnoreNotFoundError(err)).NotTo(HaveOccurred())
			g.Expect(stackitclient.IsNotFound(err)).To(BeTrue())
		}
	}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}
