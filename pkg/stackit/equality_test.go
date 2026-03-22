package stackit_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"

	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var _ = Describe("Equality", func() {
	Context("iaas.Protocol", func() {
		It("should match values by name", func() {
			Expect(Equality.DeepEqual(iaas.Protocol{Name: new("tcp")}, iaas.Protocol{Name: new("tcp")})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{Name: new("tcp")}, iaas.Protocol{Name: new("udp")})).To(BeFalse())
		})

		It("should ignore other fields", func() {
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(2)),
			})).To(BeTrue())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name: new("tcp"),
			})).To(BeTrue())

			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name:   new("udp"),
				Number: new(int64(1)),
			})).To(BeFalse())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name:   new("udp"),
				Number: new(int64(2)),
			})).To(BeFalse())
			Expect(Equality.DeepEqual(iaas.Protocol{
				Name:   new("tcp"),
				Number: new(int64(1)),
			}, iaas.Protocol{
				Name: new("udp"),
			})).To(BeFalse())
		})

		It("should work in SecurityGroupRule", func() {
			a := iaas.SecurityGroupRule{
				Direction: new(DirectionEgress),
				Protocol:  &iaas.Protocol{Name: new("tcp")},
			}
			b := iaas.SecurityGroupRule{
				Direction: new(DirectionEgress),
				Protocol:  &iaas.Protocol{Name: new("tcp")},
			}
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())

			a.Protocol.SetNumber(1)
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())

			b.Protocol.SetNumber(2)
			Expect(Equality.DeepEqual(a, b)).To(BeTrue())
		})
	})
})
