package infraflow

import (
	"context"

	"github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	mockclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock"
)

var _ = Describe("STACKIT infraflow reconcile", func() {
	var (
		ctx      context.Context
		ctrl     *gomock.Controller
		mockIaaS *mockclient.MockIaaSClient
		fctx     *FlowContext
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockIaaS = mockclient.NewMockIaaSClient(ctrl)

		fctx = &FlowContext{
			state:       shared.NewWhiteboard(),
			iaasClient:  mockIaaS,
			technicalID: "shoot--foo--bar",
			cluster: &controller.Cluster{
				Shoot: &gardencorev1beta1.Shoot{},
			},
			config: &stackitv1alpha1.InfrastructureConfig{},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("#ensureSecGroupRules", func() {
		group := &iaas.SecurityGroup{
			Id: new("secgroup"),
		}
		//nolint:prealloc // no need to preallocate for a test
		defaultWantedGroups := []iaas.SecurityGroupRule{
			{
				Direction:             stackit.DirectionIngress,
				Ethertype:             new(stackit.EtherTypeIPv4),
				RemoteSecurityGroupId: new(group.GetId()),
				Description:           new(securityGroupRuleDescription("IPv4: allow all incoming traffic within the same security group")),
			},
			{
				Direction:             stackit.DirectionEgress,
				Ethertype:             new(stackit.EtherTypeIPv4),
				RemoteSecurityGroupId: new(group.GetId()),
				Description:           new(securityGroupRuleDescription("IPv4: allow all egress traffic within the same security group")),
			},
			{
				Direction: stackit.DirectionIngress,
				Ethertype: new(stackit.EtherTypeIPv4),
				Protocol:  new(stackit.ProtocolTCP),
				PortRange: &iaas.PortRange{
					Max: int64(32767),
					Min: int64(30000),
				},
				IpRange:     new("0.0.0.0/0"),
				Description: new(securityGroupRuleDescription("IPv4: allow all incoming tcp traffic with port range 30000-32767")),
			},
			{
				Direction: stackit.DirectionIngress,
				Ethertype: new(stackit.EtherTypeIPv4),
				Protocol:  new(stackit.ProtocolUDP),
				PortRange: &iaas.PortRange{
					Max: int64(32767),
					Min: int64(30000),
				},
				IpRange:     new("0.0.0.0/0"),
				Description: new(securityGroupRuleDescription("IPv4: allow all incoming udp traffic with port range 30000-32767")),
			},
			{
				Direction:   stackit.DirectionEgress,
				Ethertype:   new(stackit.EtherTypeIPv4),
				Description: new(securityGroupRuleDescription("IPv4: allow metadata")),
				Protocol:    new(stackit.ProtocolTCP),
				IpRange:     new("169.254.169.254/32"),
				PortRange: &iaas.PortRange{
					Min: 80,
					Max: 80,
				},
			},
			{
				Direction:   stackit.DirectionEgress,
				Ethertype:   new(stackit.EtherTypeIPv4),
				Description: new(securityGroupRuleDescription("IPv4: allow dhcp")),
				Protocol:    new(stackit.ProtocolUDP),
				PortRange: &iaas.PortRange{
					Min: 67,
					Max: 67,
				},
			},
		}
		BeforeEach(func() {
			fctx.state.SetObject(ObjectSecGroup, group)
		})
		DescribeTable("should handle allowPublicEgress of infrastructure", func(allowEgress bool, expectedWantedGroups []iaas.SecurityGroupRule) {
			fctx.config.SecurityGroup = &stackitv1alpha1.SecurityGroup{AllowPublicEgress: &allowEgress}

			mockIaaS.EXPECT().
				UpdateSecurityGroupRules(ctx, group, gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, sg *iaas.SecurityGroup, rules []iaas.SecurityGroupRule, _ func(rule *iaas.SecurityGroupRule) bool) (bool, error) {
					// Use Gomega assertions here for rich diff reporting
					Expect(rules).To(ConsistOf(expectedWantedGroups))
					return true, nil
				})
			Expect(fctx.ensureSecGroupRules(ctx)).To(Succeed())
		},
			Entry("without public egress egress", true, append(defaultWantedGroups, iaas.SecurityGroupRule{
				Direction:   stackit.DirectionEgress,
				Ethertype:   new(stackit.EtherTypeIPv4),
				Description: new(securityGroupRuleDescription("IPv4: allow all outgoing traffic")),
			})),
			Entry("without public egress egress", false, defaultWantedGroups),
		)
	})

	Describe("#ensureSecGroup", func() {
		It("clears default egress rules before saving the security group in state", func() {
			expectedPayload := iaas.CreateSecurityGroupPayload{
				Name:        "shoot--foo--bar",
				Description: new("Cluster Nodes"),
			}
			defaultEgressRules := []iaas.SecurityGroupRule{
				{
					Id:        new("default-egress-ipv4"),
					Direction: stackit.DirectionEgress,
					Ethertype: new(stackit.EtherTypeIPv4),
				},
				{
					Id:        new("default-egress-ipv6"),
					Direction: stackit.DirectionEgress,
					Ethertype: new(stackit.EtherTypeIPv6),
				},
			}
			createdSecurityGroup := &iaas.SecurityGroup{
				Id:    new("security-group-id"),
				Name:  "shoot--foo--bar",
				Rules: defaultEgressRules,
			}
			expectedWantedRules := []iaas.SecurityGroupRule{}

			mockIaaS.EXPECT().GetSecurityGroupByName(ctx, "shoot--foo--bar").Return([]iaas.SecurityGroup{}, nil)
			mockIaaS.EXPECT().CreateSecurityGroup(ctx, expectedPayload).Return(createdSecurityGroup, nil)
			mockIaaS.EXPECT().ReconcileSecurityGroupRules(ctx, gomock.Any(), createdSecurityGroup, expectedWantedRules).Return(nil)

			err := fctx.ensureSecGroup(ctx)
			Expect(err).ToNot(HaveOccurred())

			Expect(fctx.state.Get(IdentifierSecGroup)).ToNot(BeNil())
			Expect(*fctx.state.Get(IdentifierSecGroup)).To(Equal("security-group-id"))
			Expect(fctx.state.Get(NameSecGroup)).ToNot(BeNil())
			Expect(*fctx.state.Get(NameSecGroup)).To(Equal("shoot--foo--bar"))

			obj := fctx.state.GetObject(ObjectSecGroup)
			Expect(obj).To(BeAssignableToTypeOf(&iaas.SecurityGroup{}))

			savedSecurityGroup, ok := obj.(*iaas.SecurityGroup)
			Expect(ok).To(BeTrue())
			Expect(savedSecurityGroup.GetId()).To(Equal("security-group-id"))
			Expect(savedSecurityGroup.GetName()).To(Equal("shoot--foo--bar"))
			Expect(savedSecurityGroup.GetRules()).To(BeEmpty())
		})
	})
})
