package client

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client/mock/loadbalancer"
)

var _ = Describe("LoadBalancingClient", func() {
	var (
		ctx      context.Context
		mockCtrl *gomock.Controller
		mockAPI  *mock.MockDefaultApi
		client   *loadBalancingClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultApi(mockCtrl)
		client = &loadBalancingClient{
			Client:    mockAPI,
			projectID: "test-project",
			region:    "eu01",
		}
	})

	It("gets a list of loadbalancers", func() {
		expectedLoadBalancers := []loadbalancer.LoadBalancer{
			{Name: ptr.To("testLB1")},
			{Name: ptr.To("testLB2")},
		}
		response := loadbalancer.ListLoadBalancersResponse{
			LoadBalancers: &expectedLoadBalancers,
		}
		mockAPI.EXPECT().ListLoadBalancersExecute(ctx, client.projectID, client.region).Return(&response, nil)
		actualLoadBalancers, err := client.ListLoadBalancers(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(actualLoadBalancers).To(Equal(expectedLoadBalancers))
	})

	It("deletes a certain loadbalancer", func() {
		mockAPI.EXPECT().DeleteLoadBalancerExecute(ctx, client.projectID, client.region, "testLB").Return(nil, nil)
		err := client.DeleteLoadBalancer(ctx, "testLB")
		Expect(err).NotTo(HaveOccurred())
	})

	It("gets a certain loadbalancer", func() {
		name := "testLB"
		expectedLoadBalancer := &loadbalancer.LoadBalancer{
			Name: ptr.To(name),
		}
		request := mock.NewMockApiGetLoadBalancerRequest(mockCtrl)
		request.EXPECT().Execute().Return(expectedLoadBalancer, nil)
		mockAPI.EXPECT().GetLoadBalancer(ctx, client.projectID, client.region, name).Return(request)

		actualLoadBalancer, err := client.GetLoadBalancer(ctx, "testLB")
		Expect(err).NotTo(HaveOccurred())

		Expect(actualLoadBalancer).To(Equal(expectedLoadBalancer))
	})
})
