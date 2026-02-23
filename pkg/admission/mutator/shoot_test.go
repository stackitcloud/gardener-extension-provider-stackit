package mutator

import (
	"bytes"
	"context"
	"time"

	configv1alpha1 "github.com/gardener/gardener-extension-os-coreos/pkg/controller/config/v1alpha1"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	mockmanager "github.com/gardener/gardener/third_party/mock/controller-runtime/manager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var _ = Describe("Shoot mutator", func() {
	Describe("#Mutate", func() {
		const namespace = "garden-dev"

		var (
			shootMutator extensionswebhook.Mutator
			shoot        *gardencorev1beta1.Shoot
			oldShoot     *gardencorev1beta1.Shoot
			ctx          = context.TODO()
			now          = metav1.Now()
			ctrl         *gomock.Controller
			mgr          *mockmanager.MockManager

			// Define the expected ProviderConfig RawExtension for PTP disabled
			expectedPTPDisabledProviderConfig *runtime.RawExtension
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())

			scheme := runtime.NewScheme()
			Expect(gardencorev1beta1.AddToScheme(scheme)).To(Succeed())
			Expect(configv1alpha1.AddToScheme(scheme)).To(Succeed())

			mgr = mockmanager.NewMockManager(ctrl)
			mgr.EXPECT().GetScheme().Return(scheme).AnyTimes()

			shootMutator = NewShootMutator(mgr)

			// Prepare the expected RawExtension for ProviderConfig
			ptpOverride := configv1alpha1.ExtensionConfig{NTP: &configv1alpha1.NTPConfig{
				Enabled: ptr.To(false),
			}}
			buffer := new(bytes.Buffer)

			encoder := serializer.NewCodecFactory(scheme).EncoderForVersion(&json.Serializer{}, configv1alpha1.SchemeGroupVersion)
			Expect(encoder.Encode(&ptpOverride, buffer)).To(Succeed())
			expectedPTPDisabledProviderConfig = &runtime.RawExtension{Raw: buffer.Bytes()}

			// Default shoot for tests
			shoot = &gardencorev1beta1.Shoot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: namespace,
				},
				Spec: gardencorev1beta1.ShootSpec{
					Kubernetes: gardencorev1beta1.Kubernetes{
						Version: "1.28.2",
					},
					SeedName: ptr.To("stackit"),
					Provider: gardencorev1beta1.Provider{
						Type: stackit.Type,
						Workers: []gardencorev1beta1.Worker{
							{
								Name: "worker1",
								Machine: gardencorev1beta1.Machine{
									Type: "c1.2",
									Image: &gardencorev1beta1.ShootMachineImage{
										Name:    "coreos",
										Version: ptr.To("4152.2.3"),
									},
								},
							},
							{
								Name: "worker2",
								Machine: gardencorev1beta1.Machine{
									Type: "c1.2",
									Image: &gardencorev1beta1.ShootMachineImage{
										Name:    "ubuntu", // Non-coreos
										Version: ptr.To("22.04"),
									},
								},
							},
						},
					},
					Region: "eu01",
					Networking: &gardencorev1beta1.Networking{
						Nodes:      ptr.To("10.250.0.0/16"),
						Type:       ptr.To("calico"),
						IPFamilies: []gardencorev1beta1.IPFamily{gardencorev1beta1.IPFamilyIPv4},
					},
				},
			}

			// oldShoot should typically mirror initial shoot state for updates
			oldShoot = shoot.DeepCopy()
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		Context("General Shoot Mutator Conditions", func() {
			It("should return without mutation if shoot is in scheduled to new seed phase", func() {
				shoot.Status.LastOperation = &gardencorev1beta1.LastOperation{
					Description:    "test",
					LastUpdateTime: metav1.Time{Time: metav1.Now().Add(time.Second * -1000)},
					Progress:       0,
					Type:           gardencorev1beta1.LastOperationTypeReconcile,
					State:          gardencorev1beta1.LastOperationStateProcessing,
				}
				shoot.Status.SeedName = ptr.To("gcp-new") // Different from Spec.SeedName
				shootExpected := shoot.DeepCopy()

				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())

				if shoot.Annotations != nil {
					delete(shoot.Annotations, "extensions.gardener.cloud/processed-by")
				}
				Expect(shoot).To(DeepEqual(shootExpected))
			})

			It("should return without mutation if shoot is in migration or restore phase", func() {
				shoot.Status.LastOperation = &gardencorev1beta1.LastOperation{
					Description:    "test",
					LastUpdateTime: metav1.Time{Time: metav1.Now().Add(time.Second * -1000)},
					Progress:       0,
					Type:           gardencorev1beta1.LastOperationTypeMigrate,
					State:          gardencorev1beta1.LastOperationStateProcessing,
				}
				shootExpected := shoot.DeepCopy()

				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())

				if shoot.Annotations != nil {
					delete(shoot.Annotations, "extensions.gardener.cloud/processed-by")
				}
				Expect(shoot).To(DeepEqual(shootExpected))
			})

			It("should return without mutation if shoot is in deletion phase", func() {
				shoot.DeletionTimestamp = &now
				shootExpected := shoot.DeepCopy()

				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())
				Expect(shoot).To(DeepEqual(shootExpected))
			})

			It("should return without mutation if it's a workerless Shoot", func() {
				shoot.Spec.Provider.Workers = nil
				shootExpected := shoot.DeepCopy()

				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())

				if shoot.Annotations != nil {
					delete(shoot.Annotations, "extensions.gardener.cloud/processed-by")
				}
				Expect(shoot).To(DeepEqual(shootExpected))
			})

			It("should return without mutation when shoot specs have not changed (update operation)", func() {
				shootWithAnnotations := shoot.DeepCopy()
				shootWithAnnotations.Annotations = map[string]string{"foo": "bar"}

				shootExpected := shootWithAnnotations.DeepCopy()

				err := shootMutator.Mutate(ctx, shootWithAnnotations, shoot)
				Expect(err).NotTo(HaveOccurred())

				if shootWithAnnotations.Annotations != nil {
					delete(shootWithAnnotations.Annotations, "extensions.gardener.cloud/processed-by")
				}
				Expect(shootWithAnnotations).To(DeepEqual(shootExpected))
				Expect(shootWithAnnotations.Spec).To(DeepEqual(shoot.Spec))
			})
		})

		Context("Mutate Flatcar Machine Image Version and ProviderConfig", func() {
			It("should not mutate image version or ProviderConfig for non-coreos workers", func() {
				// Shoot already has worker2 with ubuntu image
				shootExpected := shoot.DeepCopy() // Capture initial state

				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())

				// worker1 (coreos 4152.2.3) - should not get ProviderConfig because version < 4230.2.1
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.Version).To(Equal(ptr.To("4152.2.3")))
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.ProviderConfig).To(BeNil())

				// worker2 (ubuntu 22.04) - should be untouched
				Expect(shoot.Spec.Provider.Workers[1]).To(DeepEqual(shootExpected.Spec.Provider.Workers[1]))
			})

			It("should not mutate image version but should set ProviderConfig for coreos worker with exact target version", func() {
				shoot.Spec.Provider.Workers[0].Machine.Image.Version = ptr.To(FlatcarImageVersion) // Set to exact target

				err := shootMutator.Mutate(ctx, shoot, nil)
				Expect(err).NotTo(HaveOccurred())

				// Version should remain FlatcarImageVersion
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.Version).To(Equal(ptr.To(FlatcarImageVersion)))
				// ProviderConfig should be set (because version >= FlatcarImageVersion)
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.ProviderConfig).To(DeepEqual(expectedPTPDisabledProviderConfig))

				// worker2 (ubuntu) should be untouched
				Expect(shoot.Spec.Provider.Workers[1]).To(DeepEqual(oldShoot.Spec.Provider.Workers[1]))
			})

			It("should not mutate image version but should set ProviderConfig for coreos worker with newer version", func() {
				shoot.Spec.Provider.Workers[0].Machine.Image.Version = ptr.To("4300.0.0") // Newer version

				err := shootMutator.Mutate(ctx, shoot, nil)
				Expect(err).NotTo(HaveOccurred())

				// Version should remain 4300.0.0
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.Version).To(Equal(ptr.To("4300.0.0")))
				// ProviderConfig should be set (because version >= FlatcarImageVersion)
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.ProviderConfig).To(DeepEqual(expectedPTPDisabledProviderConfig))
			})

			It("should not mutate image version or ProviderConfig for coreos worker with older version", func() {
				err := shootMutator.Mutate(ctx, shoot, oldShoot)
				Expect(err).NotTo(HaveOccurred())

				// Version should remain 4152.2.3 (not mutated)
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.Version).To(Equal(ptr.To("4152.2.3")))
				// ProviderConfig should be nil (because version < FlatcarImageVersion)
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.ProviderConfig).To(BeNil())
			})

			It("should handle multiple coreos workers with mixed versions correctly", func() {
				shoot.Spec.Provider.Workers = []gardencorev1beta1.Worker{
					{
						Name: "old-coreos",
						Machine: gardencorev1beta1.Machine{
							Image: &gardencorev1beta1.ShootMachineImage{
								Name:    "coreos",
								Version: ptr.To("4100.0.0"), // Older
							},
						},
					},
					{
						Name: "new-coreos",
						Machine: gardencorev1beta1.Machine{
							Image: &gardencorev1beta1.ShootMachineImage{
								Name:    "coreos",
								Version: ptr.To("4230.2.1"), // Exact target
							},
						},
					},
					{
						Name: "newer-coreos",
						Machine: gardencorev1beta1.Machine{
							Image: &gardencorev1beta1.ShootMachineImage{
								Name:    "coreos",
								Version: ptr.To("4500.0.0"), // Newer
							},
						},
					},
					{
						Name: "other-os",
						Machine: gardencorev1beta1.Machine{
							Image: &gardencorev1beta1.ShootMachineImage{
								Name:    "suse-jeos",
								Version: ptr.To("15.5"),
							},
						},
					},
				}
				oldShoot = shoot.DeepCopy()

				FlatcarImageVersion = "4230.2.1"
				err := shootMutator.Mutate(ctx, shoot, nil)
				Expect(err).NotTo(HaveOccurred())

				// old-coreos: version unchanged, ProviderConfig nil
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.Version).To(Equal(ptr.To("4100.0.0")))
				Expect(shoot.Spec.Provider.Workers[0].Machine.Image.ProviderConfig).To(BeNil())

				// new-coreos: version unchanged, ProviderConfig set
				Expect(shoot.Spec.Provider.Workers[1].Machine.Image.Version).To(Equal(ptr.To("4230.2.1")))
				Expect(shoot.Spec.Provider.Workers[1].Machine.Image.ProviderConfig).To(DeepEqual(expectedPTPDisabledProviderConfig))

				// newer-coreos: version unchanged, ProviderConfig set
				Expect(shoot.Spec.Provider.Workers[2].Machine.Image.Version).To(Equal(ptr.To("4500.0.0")))
				Expect(shoot.Spec.Provider.Workers[2].Machine.Image.ProviderConfig).To(DeepEqual(expectedPTPDisabledProviderConfig))

				// other-os: untouched
				Expect(shoot.Spec.Provider.Workers[3]).To(DeepEqual(oldShoot.Spec.Provider.Workers[3]))
			})
		})
	})
})
