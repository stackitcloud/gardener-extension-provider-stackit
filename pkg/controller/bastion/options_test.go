package bastion_test

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/bastion"
)

var _ = Describe("Options", func() {
	const (
		projectID = "garden-project-uuid"
	)

	var (
		ctx = context.Background()

		fakeClient client.Client

		a *Actuator

		bastion      *extensionsv1alpha1.Bastion
		shoot        *gardencorev1beta1.Shoot
		cloudProfile *gardencorev1beta1.CloudProfile
		cluster      *extensionscontroller.Cluster

		cloudProfileConfig *stackitv1alpha1.CloudProfileConfig
		infraStatus        *stackitv1alpha1.InfrastructureStatus
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(extensionscontroller.AddToScheme(scheme))
		utilruntime.Must(stackitv1alpha1.AddToScheme(scheme))

		fakeClient = fakeclient.NewClientBuilder().WithScheme(scheme).Build()

		a = &Actuator{
			Client:            fakeClient,
			CustomLabelDomain: "kubernetes.io",
		}

		bastion = &extensionsv1alpha1.Bastion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "control-plane-namespace",
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

		cloudProfile = &gardencorev1beta1.CloudProfile{
			Spec: gardencorev1beta1.CloudProfileSpec{
				Regions: []gardencorev1beta1.Region{
					{
						Name: "eu01",
						Zones: []gardencorev1beta1.AvailabilityZone{
							{Name: "eu01-m"},
							{Name: "eu01-1"},
							{Name: "eu01-2"},
							{Name: "eu01-3"},
						},
					},
				},
				Bastion: &gardencorev1beta1.Bastion{
					MachineType: &gardencorev1beta1.BastionMachineType{
						Name: "c1i.2",
					},
					MachineImage: &gardencorev1beta1.BastionMachineImage{
						Name: "flatcar",
					},
				},
				MachineTypes: []gardencorev1beta1.MachineType{
					{
						Name:         "c1i.1",
						CPU:          resource.MustParse("1"),
						Memory:       resource.MustParse("2Gi"),
						Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
					},
					{
						Name:         "c1i.2",
						CPU:          resource.MustParse("2"),
						Memory:       resource.MustParse("4Gi"),
						Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
					},
					{
						Name:         "c1r.1",
						CPU:          resource.MustParse("1"),
						Memory:       resource.MustParse("2Gi"),
						Architecture: ptr.To(v1beta1constants.ArchitectureARM64),
					},
					{
						Name:         "c1r.2",
						CPU:          resource.MustParse("2"),
						Memory:       resource.MustParse("4Gi"),
						Architecture: ptr.To(v1beta1constants.ArchitectureARM64),
					},
				},
				MachineImages: []gardencorev1beta1.MachineImage{
					{
						Name: "flatcar",
						Versions: []gardencorev1beta1.MachineImageVersion{
							{
								ExpirableVersion: gardencorev1beta1.ExpirableVersion{
									Version:        "1.0.0",
									Classification: ptr.To(gardencorev1beta1.ClassificationDeprecated),
								},
								Architectures: []string{v1beta1constants.ArchitectureAMD64, v1beta1constants.ArchitectureARM64},
							},
							{
								ExpirableVersion: gardencorev1beta1.ExpirableVersion{
									Version:        "1.1.0",
									Classification: ptr.To(gardencorev1beta1.ClassificationSupported),
								},
								Architectures: []string{v1beta1constants.ArchitectureAMD64, v1beta1constants.ArchitectureARM64},
							},
							{
								ExpirableVersion: gardencorev1beta1.ExpirableVersion{
									Version:        "2.0.0",
									Classification: ptr.To(gardencorev1beta1.ClassificationPreview),
								},
								Architectures: []string{v1beta1constants.ArchitectureAMD64, v1beta1constants.ArchitectureARM64},
							},
						},
					},
				},
			},
		}

		cloudProfileConfig = &stackitv1alpha1.CloudProfileConfig{
			MachineImages: []stackitv1alpha1.MachineImages{
				{
					Name: "flatcar",
					Versions: []stackitv1alpha1.MachineImageVersion{
						{
							Version: "1.1.0",
							Regions: []stackitv1alpha1.RegionIDMapping{
								{
									Name:         "eu01",
									ID:           "eu01-flatcar-1.1.0",
									Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
								},
								{
									Name:         "RegionOne",
									ID:           "eu01-flatcar-1.1.0",
									Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
								},
							},
						},
					},
				},
			},
		}

		cluster = &extensionscontroller.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: bastion.Namespace,
			},
			Shoot:        shoot,
			CloudProfile: cloudProfile,
		}

		infraStatus = &stackitv1alpha1.InfrastructureStatus{
			Networks: stackitv1alpha1.NetworkStatus{
				ID: "network-id",
			},
			SecurityGroups: []stackitv1alpha1.SecurityGroup{
				{
					ID:      "security-group-id-nodes",
					Purpose: "nodes",
				},
				{
					ID:      "security-group-id-other",
					Purpose: "other",
				},
			},
		}
	})

	JustBeforeEach(func() {
		encoder := serializer.NewCodecFactory(fakeClient.Scheme()).EncoderForVersion(&json.Serializer{}, stackitv1alpha1.SchemeGroupVersion)

		cloudProfileConfigBytes, err := runtime.Encode(encoder, cloudProfileConfig)
		Expect(err).NotTo(HaveOccurred())
		cloudProfile.Spec.ProviderConfig = &runtime.RawExtension{Raw: cloudProfileConfigBytes}

		infraStatusBytes, err := runtime.Encode(encoder, infraStatus)
		Expect(err).NotTo(HaveOccurred())
		infra := &extensionsv1alpha1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name:      shoot.Name,
				Namespace: bastion.Namespace,
			},
			Status: extensionsv1alpha1.InfrastructureStatus{
				DefaultStatus: extensionsv1alpha1.DefaultStatus{
					ProviderStatus: &runtime.RawExtension{Raw: infraStatusBytes},
				},
			},
		}
		Expect(fakeClient.Create(ctx, infra)).To(Succeed())
	})

	It("should handle the RegionOne value", func() {
		shoot.Spec.Region = "RegionOne"
		cloudProfile.Spec.Regions[0].Name = "RegionOne"
		Expect(a.DetermineOptions(ctx, bastion, cluster, projectID)).To(HaveField("Region", "eu01"))
	})

	It("should correctly determine the options", func() {
		Expect(a.DetermineOptions(ctx, bastion, cluster, projectID)).To(Equal(&Options{
			Bastion:      bastion,
			ProjectID:    projectID,
			ResourceName: "shoot--garden--hops-bastion-foo",
			Labels: map[string]string{
				"kubernetes.io/cluster": "shoot--garden--hops",
				"kubernetes.io/bastion": "foo",
			},
			Region:                "eu01",
			AvailabilityZone:      "eu01-1",
			MachineType:           "c1i.2",
			ImageID:               "eu01-flatcar-1.1.0",
			NetworkID:             "network-id",
			WorkerSecurityGroupID: "security-group-id-nodes",
		}))
	})

	DescribeTable("customLabelDomain for bastion labels",
		func(customDomain string, expectedClusterLabelKey string, expectedBastionLabelKey string) {
			actuatorWithCustomDomain := &Actuator{
				Client:            fakeClient,
				CustomLabelDomain: customDomain,
			}

			options, err := actuatorWithCustomDomain.DetermineOptions(ctx, bastion, cluster, projectID)
			Expect(err).NotTo(HaveOccurred())

			// Verify the labels use the custom domain
			Expect(options.Labels).To(HaveKey(expectedClusterLabelKey))
			Expect(options.Labels[expectedClusterLabelKey]).To(Equal("shoot--garden--hops"))

			Expect(options.Labels).To(HaveKey(expectedBastionLabelKey))
			Expect(options.Labels[expectedBastionLabelKey]).To(Equal("foo"))
		},
		Entry("default kubernetes.io domain",
			"kubernetes.io",
			"kubernetes.io/cluster",
			"kubernetes.io/bastion",
		),
		Entry("custom ske.stackit.cloud domain",
			"ske.stackit.cloud",
			"ske.stackit.cloud/cluster",
			"ske.stackit.cloud/bastion",
		),
		Entry("custom example.com domain",
			"example.com",
			"example.com/cluster",
			"example.com/bastion",
		),
	)
})
