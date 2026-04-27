package validation_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

var _ = Describe("SelfHostedShootExposureConfig validation", func() {
	var (
		nilPath *field.Path
		config  *stackitv1alpha1.SelfHostedShootExposureConfig
	)

	BeforeEach(func() {
		config = &stackitv1alpha1.SelfHostedShootExposureConfig{
			LoadBalancer: &stackitv1alpha1.LoadBalancer{
				AccessControl: &stackitv1alpha1.AccessControl{},
			},
		}
	})

	Describe("#ValidateSelfHostedShootExposureConfig", func() {
		It("should accept a nil config", func() {
			Expect(ValidateSelfHostedShootExposureConfig(nil, nilPath)).To(BeEmpty())
		})

		It("should accept a config without a load balancer section", func() {
			Expect(ValidateSelfHostedShootExposureConfig(&stackitv1alpha1.SelfHostedShootExposureConfig{}, nilPath)).To(BeEmpty())
		})

		It("should accept a config without an access control section", func() {
			config.LoadBalancer.AccessControl = nil
			Expect(ValidateSelfHostedShootExposureConfig(config, nilPath)).To(BeEmpty())
		})

		It("should accept valid canonical CIDRs", func() {
			config.LoadBalancer.AccessControl.AllowedSourceRanges = []string{"10.0.0.0/8", "192.168.1.0/24", "2001:db8::/32"}
			Expect(ValidateSelfHostedShootExposureConfig(config, nilPath)).To(BeEmpty())
		})

		It("should reject a malformed CIDR", func() {
			config.LoadBalancer.AccessControl.AllowedSourceRanges = []string{"not-a-cidr"}
			errs := ValidateSelfHostedShootExposureConfig(config, nilPath)
			Expect(errs).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("loadBalancer.accessControl.allowedSourceRanges[0]"),
			}))))
		})

		It("should reject a non-canonical CIDR", func() {
			config.LoadBalancer.AccessControl.AllowedSourceRanges = []string{"10.1.2.3/8"}
			errs := ValidateSelfHostedShootExposureConfig(config, nilPath)
			Expect(errs).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":  Equal(field.ErrorTypeInvalid),
				"Field": Equal("loadBalancer.accessControl.allowedSourceRanges[0]"),
			}))))
		})

		It("should flag each invalid entry with its index", func() {
			config.LoadBalancer.AccessControl.AllowedSourceRanges = []string{"10.0.0.0/8", "bad", "also-bad"}
			errs := ValidateSelfHostedShootExposureConfig(config, nilPath)
			Expect(errs).To(HaveLen(2))
			Expect(errs[0].Field).To(Equal("loadBalancer.accessControl.allowedSourceRanges[1]"))
			Expect(errs[1].Field).To(Equal("loadBalancer.accessControl.allowedSourceRanges[2]"))
		})
	})
})
