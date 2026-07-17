// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validator_test

import (
	"context"
	"encoding/json"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/utils/test"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/admission/validator"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/install"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var _ = Describe("NamespacedCloudProfile Validator", func() {
	var (
		fakeClient  client.Client
		fakeManager manager.Manager
		ctx         = context.Background()

		shootValidator extensionswebhook.Validator
		shoot          *core.Shoot

		infrastructureConfig v1alpha1.InfrastructureConfig
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		utilruntime.Must(install.AddToScheme(scheme))
		utilruntime.Must(v1beta1.AddToScheme(scheme))
		fakeClient = fakeclient.NewClientBuilder().WithScheme(scheme).Build()
		fakeManager = &test.FakeManager{
			Client: fakeClient,
			Scheme: scheme,
		}
		shootValidator = validator.NewShootValidator(fakeManager, true)

		infrastructureConfig = v1alpha1.InfrastructureConfig{
			FloatingPoolName: "floating-pool",
			Networks: v1alpha1.Networks{
				Workers: "10.0.0.0/24",
			},
		}

		shoot = &core.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shoot",
			},
			Spec: core.ShootSpec{
				Provider: core.Provider{
					Type: stackit.Type,
					InfrastructureConfig: &runtime.RawExtension{
						Raw: encode(&infrastructureConfig),
					},
				},
				Networking: &core.Networking{Nodes: new("10.0.0.0/24")},
			},
		}
	})

	Describe("#Validate", func() {
		It("should succeed for shoot with minimal config", func() {
			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Succeed())
		})
		It("should succeed for shoot with networkID instead of worker CIDR", func() {
			infrastructureConfig.Networks.Workers = ""
			infrastructureConfig.Networks.ID = new(uuid.NewString())
			shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Raw: encode(&infrastructureConfig)}

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Succeed())
		})
		It("should succeed when spec.networking is nil", func() {
			shoot.Spec.Networking = nil

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Succeed())
		})
		It("should fail for without worker cird and networkID", func() {
			infrastructureConfig.Networks.Workers = ""
			shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Raw: encode(&infrastructureConfig)}

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Not(Succeed()))
		})
		It("should fail for with invalid ControlPlaneConfig", func() {
			shoot.Spec.Provider.ControlPlaneConfig = &runtime.RawExtension{Raw: []byte(`{"foo": "bar"}`)}

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Not(Succeed()))
		})
		It("should fail for with invalid InfrastructureConfig", func() {
			shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Raw: []byte(`{"foo": "bar"}`)}

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Not(Succeed()))
		})
		It("should fail for with invalid invalid CSI driver", func() {
			controlPlaneConfig := v1alpha1.ControlPlaneConfig{
				Storage: &v1alpha1.Storage{
					CSI: &v1alpha1.CSI{
						Name: "foobar",
					},
				},
			}

			shoot.Spec.Provider.ControlPlaneConfig = &runtime.RawExtension{Raw: encode(&controlPlaneConfig)}

			Expect(shootValidator.Validate(ctx, shoot, nil)).To(Not(Succeed()))
		})

		It("should fail for immutable field", func() {
			infrastructureConfig.Networks.Workers = "10.0.1.0/24"
			newShoot := shoot.DeepCopy()
			newShoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{Raw: encode(&infrastructureConfig)}

			Expect(shootValidator.Validate(ctx, newShoot, shoot)).To(Not(Succeed()))
		})
	})
})

func encode(object runtime.Object) []byte {
	GinkgoHelper()
	data, err := json.Marshal(object)
	Expect(err).NotTo(HaveOccurred())
	return data
}
