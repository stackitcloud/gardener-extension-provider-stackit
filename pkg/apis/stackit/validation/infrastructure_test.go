// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/validation"
)

var _ = Describe("InfrastructureConfig validation", func() {
	var (
		nilPath *field.Path

		floatingPoolName1 = "foo"

		infrastructureConfig *stackitv1alpha1.InfrastructureConfig

		nodes       = "10.250.0.0/16"
		invalidCIDR = "invalid-cidr"
	)

	BeforeEach(func() {
		infrastructureConfig = &stackitv1alpha1.InfrastructureConfig{
			FloatingPoolName: floatingPoolName1,
			Networks: stackitv1alpha1.Networks{
				Router: &stackitv1alpha1.Router{
					ID: "hugo",
				},
				Workers: "10.250.0.0/16",
			},
		}
	})

	Describe("#ValidateInfrastructureConfig", func() {
		It("should forbid invalid floating pool name configuration", func() {
			infrastructureConfig.FloatingPoolName = ""

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":  Equal(field.ErrorTypeRequired),
				"Field": Equal("floatingPoolName"),
			}))
		})

		It("should forbid invalid router id configuration", func() {
			infrastructureConfig.Networks.Router = &stackitv1alpha1.Router{ID: ""}

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("networks.router.id"),
			}))
		})

		It("should forbid floating ip subnet when router is specified", func() {
			infrastructureConfig.Networks.Router = &stackitv1alpha1.Router{ID: "sample-router-id"}
			infrastructureConfig.FloatingPoolSubnetName = ptr.To("sample-floating-pool-subnet-id")

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("floatingPoolSubnetName"),
			}))
		})

		It("should forbid subnet id when network id is unspecified", func() {
			infrastructureConfig.Networks.SubnetID = ptr.To(uuid.NewString())

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("networks.subnetId"),
				"Detail": Equal("if subnet ID is provided a networkID must be provided"),
			}))
		})

		It("should forbid an invalid subnet id", func() {
			infrastructureConfig.Networks.ID = ptr.To(uuid.NewString())
			infrastructureConfig.Networks.SubnetID = ptr.To("thisiswrong")

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("networks.subnetId"),
				"Detail": Equal("if subnet ID is provided it must be a valid OpenStack UUID"),
			}))
		})

		It("should allow an valid OpenStack UUID as subnet ID", func() {
			infrastructureConfig.Networks.ID = ptr.To(uuid.NewString())
			infrastructureConfig.Networks.SubnetID = ptr.To(uuid.NewString())

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(BeEmpty())
		})
	})

	Context("CIDR", func() {
		It("should forbid empty workers CIDR", func() {
			infrastructureConfig.Networks.Workers = ""

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeRequired),
				"Field":  Equal("networks.workers"),
				"Detail": Equal("must specify the network range for the worker network"),
			}))
		})

		It("should forbid invalid workers CIDR", func() {
			infrastructureConfig.Networks.Workers = invalidCIDR

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("networks.workers"),
				"Detail": Equal("invalid CIDR address: invalid-cidr"),
			}))
		})

		It("should forbid workers CIDR which are not in Nodes CIDR", func() {
			infrastructureConfig.Networks.Workers = "1.1.1.1/32"

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("networks.workers"),
				"Detail": Equal(`must be a subset of "networking.nodes" ("10.250.0.0/16")`),
			}))
		})

		It("should forbid non canonical CIDRs", func() {
			nodeCIDR := "10.250.0.3/16"

			infrastructureConfig.Networks.Workers = "10.250.3.8/24"

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodeCIDR, nilPath)
			Expect(errorList).To(HaveLen(1))

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("networks.workers"),
				"Detail": Equal("must be valid canonical CIDR"),
			}))
		})

		It("should forbid an invalid network id configuration", func() {
			invalidID := "thisiswrong"
			infrastructureConfig.Networks.ID = &invalidID

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("networks.id"),
			}))
		})

		It("should allow an valid OpenStack UUID as network ID", func() {
			id, err := uuid.NewUUID()
			Expect(err).NotTo(HaveOccurred())
			infrastructureConfig.Networks.ID = ptr.To(id.String())

			errorList := ValidateInfrastructureConfig(infrastructureConfig, &nodes, nilPath)

			Expect(errorList).To(BeEmpty())
		})
	})

	Describe("#ValidateInfrastructureConfigUpdate", func() {
		It("should return no errors for an unchanged config", func() {
			Expect(ValidateInfrastructureConfigUpdate(infrastructureConfig, infrastructureConfig, nilPath)).To(BeEmpty())
		})

		It("should forbid changing the network section", func() {
			newInfrastructureConfig := infrastructureConfig.DeepCopy()
			newInfrastructureConfig.Networks.Router = &stackitv1alpha1.Router{ID: "name"}

			errorList := ValidateInfrastructureConfigUpdate(infrastructureConfig, newInfrastructureConfig, nilPath)

			Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("networks"),
			}))))
		})

		It("should forbid changing the floating pool", func() {
			newInfrastructureConfig := infrastructureConfig.DeepCopy()
			newInfrastructureConfig.FloatingPoolName = "test"

			errorList := ValidateInfrastructureConfigUpdate(infrastructureConfig, newInfrastructureConfig, nilPath)

			Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("floatingPoolName"),
			}))))
		})

		It("should forbid changing the floating pool subnet", func() {
			newInfrastructureConfig := infrastructureConfig.DeepCopy()
			newInfrastructureConfig.FloatingPoolSubnetName = ptr.To("test")

			errorList := ValidateInfrastructureConfigUpdate(infrastructureConfig, newInfrastructureConfig, nilPath)

			Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("floatingPoolSubnetName"),
			}))))
		})
	})

	Describe("#ValidateInfrastructureConfigAgainstCloudProfile", func() {
		var cloudProfileConfig *stackitv1alpha1.CloudProfileConfig

		BeforeEach(func() {
			cloudProfileConfig = &stackitv1alpha1.CloudProfileConfig{
				Constraints: stackitv1alpha1.Constraints{
					FloatingPools: []stackitv1alpha1.FloatingPool{
						{
							Name: floatingPoolName1,
						},
					},
				},
			}
		})

		It("should validate that the floating pool name exists in the cloud profile", func() {
			oldInfrastructureConfig := infrastructureConfig.DeepCopy()
			infrastructureConfig.FloatingPoolName = "does-for-sure-not-exist-in-cloudprofile"

			errorList := ValidateInfrastructureConfigAgainstCloudProfile(oldInfrastructureConfig, infrastructureConfig, cloudProfileConfig, nilPath)
			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":     Equal(field.ErrorTypeNotSupported),
				"Field":    Equal("floatingPoolName"),
				"BadValue": Equal("does-for-sure-not-exist-in-cloudprofile"),
			}))
		})

		It("should not validate anything if the floating pool name was not changed", func() {
			infrastructureConfig.FloatingPoolName = "does-for-sure-not-exist-in-cloudprofile"
			oldInfrastructureConfig := infrastructureConfig.DeepCopy()

			errorList := ValidateInfrastructureConfigAgainstCloudProfile(oldInfrastructureConfig, infrastructureConfig, cloudProfileConfig, nilPath)
			Expect(errorList).To(BeEmpty())
		})

	})
})
