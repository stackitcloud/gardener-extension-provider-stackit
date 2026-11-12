package infraflow

import (
	"context"
	"errors"
	"time"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	iaas "github.com/stackitcloud/stackit-sdk-go/services/iaas/v2api"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
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

	Describe("#Reconcile", func() {
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

			scheme := runtime.NewScheme()
			Expect(extensionsv1alpha1.AddToScheme(scheme)).To(Succeed())
			Expect(stackitv1alpha1.AddToScheme(scheme)).To(Succeed())

			infra := &extensionsv1alpha1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "infra",
					Namespace: "default",
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(infra).WithStatusSubresource(infra).Build()

			fctx = &FlowContext{
				state:       shared.NewWhiteboard(),
				iaasClient:  mockIaaS,
				technicalID: "shoot--foo--bar",
				log:         logr.Discard(),
				client:      fakeClient,
				config: &stackitv1alpha1.InfrastructureConfig{
					Networks: stackitv1alpha1.Networks{
						Workers: "10.250.0.0/16",
					},
				},
				cloudProfileConfig: &stackitv1alpha1.CloudProfileConfig{
					DNSServers: []string{"8.8.8.8"},
				},
				infra: infra,
			}
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		// If requests time out, the backend resource may still be created. The flow must not retry
		// internally and must return immediately so the controller-level rate limiter enforces a >=1m
		// backoff. This allows the backend time to finish provisioning (found by name on next reconcile)
		It("does not retry inside the flow when network creation fails and returns the error immediately", func() {
			networkCreationAttempts := 0
			expectedErr := errors.New("timeout creating network in backend")

			mockIaaS.EXPECT().GetNetworkByName(gomock.Any(), "shoot--foo--bar").Return([]iaas.Network{}, nil).Times(1)
			mockIaaS.EXPECT().CreateIsolatedNetwork(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, payload iaas.CreateIsolatedNetworkPayload) (*iaas.Network, error) {
					networkCreationAttempts++
					return nil, expectedErr
				},
			).Times(1)

			// Downstream tasks should never be executed when network creation fails
			mockIaaS.EXPECT().GetSecurityGroupByName(gomock.Any(), gomock.Any()).Times(0)
			mockIaaS.EXPECT().CreateSecurityGroup(gomock.Any(), gomock.Any()).Times(0)

			start := time.Now()
			err := fctx.Reconcile(ctx)
			elapsed := time.Since(start)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("timeout creating network in backend"))
			// Proves Gardener flow library does NOT retry tasks on its own inside the reconcile function
			Expect(networkCreationAttempts).To(Equal(1))
			// Proves the error returns immediately without sleeping or looping
			Expect(elapsed).To(BeNumerically("<", 1*time.Second))
		})
	})
})
