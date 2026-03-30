package client

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock/loadbalancer"
)

var _ = Describe("LoadBalancingClient", func() {
	var (
		ctx      context.Context
		mockCtrl *gomock.Controller
		mockAPI  *mock.MockDefaultAPI
		client   *loadBalancingClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultAPI(mockCtrl)
		client = &loadBalancingClient{
			Client:    mockAPI,
			projectID: "test-project",
			region:    "eu01",
		}
	})

	It("gets a list of loadbalancers", func() {
		expectedLoadBalancers := []loadbalancer.LoadBalancer{
			{Name: new("testLB1")},
			{Name: new("testLB2")},
		}
		response := loadbalancer.ListLoadBalancersResponse{
			LoadBalancers: expectedLoadBalancers,
		}
		mockAPI.EXPECT().ListLoadBalancers(ctx, client.projectID, client.region).Return(loadbalancer.ApiListLoadBalancersRequest{ApiService: mockAPI})
		mockAPI.EXPECT().ListLoadBalancersExecute(gomock.Any()).Return(&response, nil)
		actualLoadBalancers, err := client.ListLoadBalancers(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(actualLoadBalancers).To(Equal(expectedLoadBalancers))
	})

	It("deletes a certain loadbalancer", func() {
		mockAPI.EXPECT().DeleteLoadBalancer(ctx, client.projectID, client.region, "testLB").Return(loadbalancer.ApiDeleteLoadBalancerRequest{ApiService: mockAPI})
		mockAPI.EXPECT().DeleteLoadBalancerExecute(gomock.Any()).Return(nil, nil)
		err := client.DeleteLoadBalancer(ctx, "testLB")
		Expect(err).NotTo(HaveOccurred())
	})

	It("gets a certain loadbalancer", func() {
		name := "testLB"
		expectedLoadBalancer := &loadbalancer.LoadBalancer{
			Name: new(name),
		}
		mockAPI.EXPECT().GetLoadBalancer(ctx, client.projectID, client.region, name).Return(loadbalancer.ApiGetLoadBalancerRequest{ApiService: mockAPI})
		mockAPI.EXPECT().GetLoadBalancerExecute(gomock.Any()).Return(expectedLoadBalancer, nil)

		actualLoadBalancer, err := client.GetLoadBalancer(ctx, "testLB")
		Expect(err).NotTo(HaveOccurred())

		Expect(actualLoadBalancer).To(Equal(expectedLoadBalancer))
	})
})
