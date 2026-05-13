package infraflow

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	mockclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock"
)

var _ = Describe("STACKIT infraflow reconcile", func() {
	Describe("#ensureSecGroup", func() {
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
			}
		})

		AfterEach(func() {
			ctrl.Finish()
		})

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
