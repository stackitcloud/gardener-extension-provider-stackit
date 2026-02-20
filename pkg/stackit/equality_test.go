package stackit_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"k8s.io/utils/ptr"

	. "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

var _ = Describe("Equality", func() {
	Context("iaas.Protocol", func() {
		It("should match values by name", func() {
			Expect(Equality.DeepEqual(iaas.Protocol{Name: ptr.To("tcp")}, iaas.Protocol{Name: ptr.To("tcp")})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{Name: ptr.To("tcp")}, iaas.Protocol{Name: ptr.To("udp")})).To(BeFalse())
		})

		It("should ignore other fields", func() {
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](2),
			})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name: ptr.To("tcp"),
			})).To(BeTrue())

			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name:   ptr.To("udp"),
				Number: ptr.To[int64](1),
			})).To(BeFalse())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name:   ptr.To("udp"),
				Number: ptr.To[int64](2),
			})).To(BeFalse())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   ptr.To("tcp"),
				Number: ptr.To[int64](1),
			}, iaas.Protocol{
				Name: ptr.To("udp"),
			})).To(BeFalse())
		})

		It("should work in SecurityGroupRule", func() {
			a := iaas.SecurityGroupRule{
				Direction: ptr.To(DirectionEgress),
				Protocol:  &iaas.Protocol{Name: ptr.To("tcp")},
			}
			b := iaas.SecurityGroupRule{
				Direction: ptr.To(DirectionEgress),
				Protocol:  &iaas.Protocol{Name: ptr.To("tcp")},
			}
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())

			a.Protocol.SetNumber(1)
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())

			b.Protocol.SetNumber(2)
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())
		})
	})
})
