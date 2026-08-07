package mutator

import (
	"context"
	"time"

	configv1alpha1 "github.com/gardener/gardener-extension-os-coreos/pkg/controller/config/v1alpha1"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	testutils "github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
			mgr          *testutils.FakeManager
		)

		BeforeEach(func() {
			scheme := runtime.NewScheme()
			Expect(gardencorev1beta1.AddToScheme(scheme)).To(Succeed())
			Expect(configv1alpha1.AddToScheme(scheme)).To(Succeed())

			mgr = &testutils.FakeManager{Scheme: scheme}

			shootMutator = NewShootMutator(mgr)

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
					SeedName: new("stackit"),
					Provider: gardencorev1beta1.Provider{
						Type: stackit.Type,
						Workers: []gardencorev1beta1.Worker{
							{
								Name: "worker1",
								Machine: gardencorev1beta1.Machine{
									Type: "c1.2",
									Image: &gardencorev1beta1.ShootMachineImage{
										Name:    "coreos",
										Version: new("4152.2.3"),
									},
								},
							},
							{
								Name: "worker2",
								Machine: gardencorev1beta1.Machine{
									Type: "c1.2",
									Image: &gardencorev1beta1.ShootMachineImage{
										Name:    "ubuntu", // Non-coreos
										Version: new("22.04"),
									},
								},
							},
						},
					},
					Region: "eu01",
					Networking: &gardencorev1beta1.Networking{
						Nodes:      new("10.250.0.0/16"),
						Type:       new("calico"),
						IPFamilies: []gardencorev1beta1.IPFamily{gardencorev1beta1.IPFamilyIPv4},
					},
				},
			}

			// oldShoot should typically mirror initial shoot state for updates
			oldShoot = shoot.DeepCopy()
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
				shoot.Status.SeedName = new("gcp-new") // Different from Spec.SeedName
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

	})
})
