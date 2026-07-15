// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

var _ = Describe("ControlPlaneConfig validation", func() {
	var (
		nilPath      *field.Path
		controlPlane *stackitv1alpha1.ControlPlaneConfig
	)

	BeforeEach(func() {
		controlPlane = &stackitv1alpha1.ControlPlaneConfig{}
	})

	Describe("#ValidateControlPlaneConfig", func() {
		It("should return no errors for a valid configuration", func() {
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath)).To(BeEmpty())
		})

		It("should fail with invalid CCM feature gates", func() {
			controlPlane.CloudControllerManager = &stackitv1alpha1.CloudControllerManagerConfig{
				FeatureGates: map[string]bool{
					"AnyVolumeDataSource": true,
					"Foo":                 true,
				},
			}

			errorList := ValidateControlPlaneConfig(controlPlane, "1.28.2", nilPath)

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("cloudControllerManager.featureGates.Foo"),
				})),
			))
		})

		It("should succeed with stackit CCM", func() {
			controlPlane.CloudControllerManager = &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.STACKIT),
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath).ToAggregate()).To(Succeed())
		})

		It("should succeed with openstack CCM", func() {
			controlPlane.CloudControllerManager = &stackitv1alpha1.CloudControllerManagerConfig{
				Name: string(stackitv1alpha1.OPENSTACK),
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath).ToAggregate()).To(Succeed())
		})

		It("should succeed with stackit CSI driver", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: string(stackitv1alpha1.STACKIT)},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath).ToAggregate()).To(Succeed())
		})

		It("should succeed with openstack CSI driver", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: string(stackitv1alpha1.OPENSTACK)},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath).ToAggregate()).To(Succeed())
		})

		It("should succeed with supported CSI compatibility mode", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: string(stackitv1alpha1.STACKIT), CompatibilityMode: string(stackitv1alpha1.COMPAT)},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath).ToAggregate()).To(Succeed())
		})

		It("should fail with an unsupported CSI compatibility mode", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: string(stackitv1alpha1.STACKIT), CompatibilityMode: "bogus"},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath)).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("storage.csi.compatibilityMode"),
				})),
			))
		})

		It("should fail with an CSI compatibility mode and openstack CSI", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: string(stackitv1alpha1.OPENSTACK), CompatibilityMode: string(stackitv1alpha1.COMPAT)},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath)).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("storage.csi.compatibilityMode"),
				})),
			))
		})

		It("should fail with an unsupported CSI driver", func() {
			controlPlane.Storage = &stackitv1alpha1.Storage{
				CSI: &stackitv1alpha1.CSI{Name: "foobar"},
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath)).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("storage.csi.name"),
				})),
			))
		})

		It("should fail with an unsupported ccm", func() {
			controlPlane.CloudControllerManager = &stackitv1alpha1.CloudControllerManagerConfig{
				Name: "foobar",
			}
			Expect(ValidateControlPlaneConfig(controlPlane, "", nilPath)).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("cloudControllerManager.name"),
				})),
			))
		})

	})

	Describe("#ValidateControlPlaneConfigUpdate", func() {
		It("should return no errors for an unchanged config", func() {
			Expect(ValidateControlPlaneConfigUpdate(controlPlane, controlPlane, nilPath)).To(BeEmpty())
		})
	})
})
