package selfhostedshootexposure_test

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/selfhostedshootexposure"
)

var _ = Describe("Actuator", func() {
	var (
		ctx      context.Context
		logger   logr.Logger
		actuator *selfhostedshootexposure.Actuator
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = logr.Discard()
		actuator = &selfhostedshootexposure.Actuator{
			Client: fake.NewClientBuilder().Build(),
		}
	})

	Describe("#Reconcile", func() {
		It("should return error when getResources fails (no client configured)", func() {
			exposure := &extensionsv1alpha1.SelfHostedShootExposure{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "kube-system",
				},
				Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
					DefaultSpec: extensionsv1alpha1.DefaultSpec{
						Type: "stackit",
					},
					Port: 443,
				},
			}
			cluster := &extensionscontroller.Cluster{
				Shoot: &gardencorev1beta1.Shoot{
					Spec: gardencorev1beta1.ShootSpec{Region: "eu01"},
				},
			}

			ingress, err := actuator.Reconcile(ctx, logger, exposure, cluster)

			Expect(err).To(HaveOccurred())
			Expect(ingress).To(BeNil())
		})
	})

	Describe("#Delete", func() {
		It("should return error when getResources fails (no client configured)", func() {
			exposure := &extensionsv1alpha1.SelfHostedShootExposure{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "kube-system",
				},
			}
			cluster := &extensionscontroller.Cluster{
				Shoot: &gardencorev1beta1.Shoot{
					Spec: gardencorev1beta1.ShootSpec{Region: "eu01"},
				},
			}

			err := actuator.Delete(ctx, logger, exposure, cluster)

			Expect(err).To(HaveOccurred())
		})
	})

	Describe("#ForceDelete", func() {
		It("should delegate to Delete", func() {
			exposure := &extensionsv1alpha1.SelfHostedShootExposure{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "kube-system",
				},
			}
			cluster := &extensionscontroller.Cluster{
				Shoot: &gardencorev1beta1.Shoot{
					Spec: gardencorev1beta1.ShootSpec{Region: "eu01"},
				},
			}

			err := actuator.ForceDelete(ctx, logger, exposure, cluster)

			// Should fail with same error as Delete (no client configured)
			Expect(err).To(HaveOccurred())
		})
	})
})
