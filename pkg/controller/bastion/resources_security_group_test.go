package bastion

import (
	"fmt"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

var _ = Describe("Security Group", func() {
	Describe("determineWantedSecurityGroupRules", func() {
		var o *Options

		BeforeEach(func() {
			o = &Options{
				Bastion: &extensionsv1alpha1.Bastion{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foo",
						Namespace: "shoot--garden--hops",
					},
					Spec: extensionsv1alpha1.BastionSpec{
						Ingress: []extensionsv1alpha1.BastionIngressPolicy{
							{
								IPBlock: networkingv1.IPBlock{
									CIDR: "1.2.3.4/32",
								},
							},
							{
								IPBlock: networkingv1.IPBlock{
									CIDR: "2001:db8:1::/48",
								},
							},
						},
					},
				},
			}
		})

		It("should return the security group rules for Bastion.spec.ingress", func() {
			Expect(o.determineWantedSecurityGroupRules()).To(ContainElements(
				iaas.SecurityGroupRule{
					Description: ptr.To(fmt.Sprintf("Allow ingress to Bastion %s from %s", o.Bastion.Name, "1.2.3.4/32")),

					Direction: ptr.To(stackit.DirectionIngress),
					Ethertype: ptr.To(stackit.EtherTypeIPv4),
					Protocol:  ptr.To(stackit.ProtocolTCP),
					PortRange: iaas.NewPortRange(22, 22),

					IpRange: ptr.To("1.2.3.4/32"),
				},
				iaas.SecurityGroupRule{
					Description: ptr.To(fmt.Sprintf("Allow ingress to Bastion %s from %s", o.Bastion.Name, "2001:db8:1::/48")),

					Direction: ptr.To(stackit.DirectionIngress),
					Ethertype: ptr.To(stackit.EtherTypeIPv6),
					Protocol:  ptr.To(stackit.ProtocolTCP),
					PortRange: iaas.NewPortRange(22, 22),

					IpRange: ptr.To("2001:db8:1::/48"),
				},
			))
		})

		It("should add a security group rule if Bastion.spec.ingress is empty", func() {
			o.Bastion.Spec.Ingress = nil

			Expect(o.determineWantedSecurityGroupRules()).To(ContainElement(
				iaas.SecurityGroupRule{
					Description: ptr.To(fmt.Sprintf("Allow ingress to Bastion %s from world", o.Bastion.Name)),

					Direction: ptr.To(stackit.DirectionIngress),
					Ethertype: ptr.To(stackit.EtherTypeIPv4),
					Protocol:  ptr.To(stackit.ProtocolTCP),
					PortRange: iaas.NewPortRange(22, 22),

					IpRange: ptr.To("0.0.0.0/0"),
				},
			))
		})
	})
})
