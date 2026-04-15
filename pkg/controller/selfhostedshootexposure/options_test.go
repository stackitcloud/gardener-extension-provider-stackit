package selfhostedshootexposure_test

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/selfhostedshootexposure"
)

var _ = Describe("Options", func() {
	const (
		projectID = "garden-project-uuid"
	)

	var (
		ctx = context.Background()

		fakeClient client.Client

		a *Actuator

		exposure    *extensionsv1alpha1.SelfHostedShootExposure
		shoot       *gardencorev1beta1.Shoot
		cluster     *extensionscontroller.Cluster
		infraStatus *stackitv1alpha1.InfrastructureStatus
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(extensionscontroller.AddToScheme(scheme))
		utilruntime.Must(stackitv1alpha1.AddToScheme(scheme))

		fakeClient = fakeclient.NewClientBuilder().WithScheme(scheme).Build()

		a = &Actuator{
			Client:  fakeClient,
			Decoder: serializer.NewCodecFactory(fakeClient.Scheme(), serializer.EnableStrict).UniversalDecoder(),
		}

		exposure = &extensionsv1alpha1.SelfHostedShootExposure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-exposure",
				Namespace: "control-plane-namespace",
			},
			Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: "stackit",
				},
				Port: 443,
			},
		}

		shoot = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "hops",
				Namespace: "garden",
			},
			Spec: gardencorev1beta1.ShootSpec{
				Region: "eu01",
			},
			Status: gardencorev1beta1.ShootStatus{
				TechnicalID: "shoot--garden--hops",
			},
		}

		cluster = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: exposure.Namespace,
			},
			Shoot: shoot,
		}

		infraStatus = &stackitv1alpha1.InfrastructureStatus{
			Networks: stackitv1alpha1.NetworkStatus{
				ID: "network-id",
			},
		}
	})

	JustBeforeEach(func() {
		encoder := serializer.NewCodecFactory(fakeClient.Scheme()).EncoderForVersion(&json.Serializer{}, stackitv1alpha1.SchemeGroupVersion)

		infraStatusBytes, err := runtime.Encode(encoder, infraStatus)
		Expect(err).NotTo(HaveOccurred())
		infra := &extensionsv1alpha1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shoot.Name,
				Namespace: exposure.Namespace,
			},
			Status: extensionsv1alpha1.InfrastructureStatus{
				DefaultStatus: extensionsv1alpha1.DefaultStatus{
					ProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes},
				},
			},
		}
		Expect(fakeClient.Create(ctx, infra)).To(Succeed())
	})

	It("should correctly determine the options with default plan", func() {
		opts, err := a.DetermineOptions(ctx, exposure, cluster, projectID)

		Expect(err).NotTo(HaveOccurred())
		Expect(opts).To(Equal(&Options{
			SelfHostedShootExposure: exposure,
			ProjectID:               projectID,
			ResourceName:            "shoot--garden--hops-exposure-test-exposure",
			Labels: map[string]string{
				"cluster.stackit.cloud":  "shoot--garden--hops",
				"exposure.stackit.cloud": "test-exposure",
			},
			Region:    "eu01",
			NetworkID: "network-id",
			PlanId:    "p10",
		}))
	})

	It("should use PlanId from providerConfig", func() {
		encoder := serializer.NewCodecFactory(fakeClient.Scheme()).EncoderForVersion(&json.Serializer{}, stackitv1alpha1.SchemeGroupVersion)
		providerConfig := &stackitv1alpha1.SelfHostedShootExposureConfig{
			LoadBalancer: &stackitv1alpha1.LoadBalancerConfig{
				PlanId: new("p250"),
			},
		}
		providerConfigBytes, err := runtime.Encode(encoder, providerConfig)
		Expect(err).NotTo(HaveOccurred())
		exposure.Spec.ProviderConfig = &runtime.RawExtension{Raw: providerConfigBytes}

		opts, err := a.DetermineOptions(ctx, exposure, cluster, projectID)

		Expect(err).NotTo(HaveOccurred())
		Expect(opts.PlanId).To(Equal("p250"))
	})

	It("should handle the RegionOne value", func() {
		shoot.Spec.Region = "RegionOne"

		opts, err := a.DetermineOptions(ctx, exposure, cluster, projectID)

		Expect(err).NotTo(HaveOccurred())
		Expect(opts.Region).To(Equal("eu01"))
	})

	It("should set flat STACKIT LB label keys (no '/' — rejected by STACKIT LB API)", func() {
		options, err := a.DetermineOptions(ctx, exposure, cluster, projectID)
		Expect(err).NotTo(HaveOccurred())

		Expect(options.Labels).To(HaveKeyWithValue("cluster.stackit.cloud", "shoot--garden--hops"))
		Expect(options.Labels).To(HaveKeyWithValue("exposure.stackit.cloud", "test-exposure"))
	})
})
