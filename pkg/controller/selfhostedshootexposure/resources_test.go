package selfhostedshootexposure

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock"
)

var _ = Describe("Resources", func() {
	var (
		ctx          context.Context
		logger       logr.Logger
		mockCtrl     *gomock.Controller
		mockLBClient *mock.MockLoadBalancingClient
		r            *Resources
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = logr.Discard()
		mockCtrl = gomock.NewController(GinkgoT())
		mockLBClient = mock.NewMockLoadBalancingClient(mockCtrl)
		r = &Resources{
			Options: Options{
				ResourceName: "test-lb",
			},
			LBClient: mockLBClient,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Describe("#getExistingResources", func() {
		It("should populate LoadBalancer when found", func() {
			expectedLB := &loadbalancer.LoadBalancer{
				Name: new("test-lb"),
			}
			mockLBClient.EXPECT().
				GetLoadBalancer(ctx, "test-lb").
				Return(expectedLB, nil)

			err := r.getExistingResources(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
			Expect(r.LoadBalancer).To(Equal(expectedLB))
		})

		It("should leave LoadBalancer nil when not found", func() {
			mockLBClient.EXPECT().
				GetLoadBalancer(ctx, "test-lb").
				Return(nil, nil)

			err := r.getExistingResources(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
			Expect(r.LoadBalancer).To(BeNil())
		})

		It("should return error when GetLoadBalancer fails", func() {
			mockLBClient.EXPECT().
				GetLoadBalancer(ctx, "test-lb").
				Return(nil, fmt.Errorf("API error"))

			err := r.getExistingResources(ctx, logger)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error getting load balancer"))
		})
	})
})
