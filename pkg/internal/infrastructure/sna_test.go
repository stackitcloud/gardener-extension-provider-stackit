package infrastructure

import (
	"context"
	"errors"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client/mocks"
)

var _ = Describe("SNA", func() {

	var (
		ctrl       *gomock.Controller
		nw         *mocks.MockNetworking
		ctx        = context.Background()
		networkID  = "bf4ed175-1c4e-4aed-9af3-6a5d55b64b5f"
		subnetID   = "f4da24f7-428b-4474-adb0-4cd503e0bb1d"
		routerID   = "282b8583-05f4-4ffa-88f4-da6e56f09290"
		subnetCIDR = "10.0.42.0/27"
	)

	stubGatewayInfo := routers.GatewayInfo{ExternalFixedIPs: []routers.ExternalFixedIP{{}}}
	//nolint:unparam // mock is unused but leaving as it for future use
	addMockRouter := func(mock *mocks.MockNetworking, id string, tags []string) {
		nw.EXPECT().GetRouterByID(ctx, id).Return(&routers.Router{
			GatewayInfo: stubGatewayInfo,
			ID:          id,
			Tags:        tags,
		}, nil)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		nw = mocks.NewMockNetworking(ctrl)
	})

	Context("resolve subnet", func() {
		It("should fail on client error", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(nil, errors.New("client error"))
			_, err := getSubnet(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("client error")))
		})
		It("should fail on zero subnets", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return([]subnets.Subnet{}, nil)
			_, err := getSubnet(ctx, nw, networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should fail on multiple subnets", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(
				[]subnets.Subnet{{}, {}}, nil)
			_, err := getSubnet(ctx, nw, networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should return the single subnet", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(
				[]subnets.Subnet{{ID: subnetID, CIDR: subnetCIDR}}, nil)
			subnet, err := getSubnet(ctx, nw, networkID)
			Expect(err).To(Succeed())
			Expect(subnet.ID).To(Equal(subnetID))
			Expect(subnet.CIDR).To(Equal(subnetCIDR))
		})
	})

	Context("resolve router", func() {
		It("should fail on router interface client error", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return(nil, errors.New("client error"))
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("client error")))
		})
		It("should fail if no router exists", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{}, nil)
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should fail if router fails to resolve", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			nw.EXPECT().GetRouterByID(ctx, routerID).Return(nil, errors.New("router error"))
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("router error")))
		})
		It("should fail if non SNA router exists", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			addMockRouter(nw, routerID, nil)
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("found non-SNA router with external gateway")))
		})
		It("should succeed if single SNA router exists", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			addMockRouter(nw, routerID, []string{"SNA"})
			snaRouterID, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(Succeed())
			Expect(snaRouterID).To(Equal(routerID))
		})
		It("should fail if single SNA router is internal", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			addMockRouter(nw, routerID, []string{"SNA", "internal"})
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should fail if single SNA router has no gateways", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			nw.EXPECT().GetRouterByID(ctx, routerID).Return(&routers.Router{
				ID:   routerID,
				Tags: []string{"SNA"},
			}, nil)
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("no router found")))
		})
		It("should fail if two router exist without external tag", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{
				{DeviceID: routerID},
				{DeviceID: routerID + "2"},
			}, nil)
			addMockRouter(nw, routerID, []string{"SNA"})
			addMockRouter(nw, routerID+"2", []string{"SNA"})
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("no router found")))
		})
		It("should succeed if two router exist and one has external tag", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{
				{DeviceID: routerID},
				{DeviceID: routerID + "2"},
			}, nil)
			addMockRouter(nw, routerID, []string{"SNA", "internal"})
			addMockRouter(nw, routerID+"2", []string{"SNA", "external"})
			snaRouterID, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(Succeed())
			Expect(snaRouterID).To(Equal(routerID + "2"))
		})
		It("should fail if three router exist and two have external tag", func() {
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{
				{DeviceID: routerID},
				{DeviceID: routerID + "2"},
				{DeviceID: routerID + "3"},
			}, nil)
			addMockRouter(nw, routerID, []string{"SNA"})
			addMockRouter(nw, routerID+"2", []string{"SNA", "external"})
			addMockRouter(nw, routerID+"3", []string{"SNA", "external"})
			_, err := getSNARouterIDFromNetworkID(ctx, nw, networkID)
			Expect(err).To(MatchError(ContainSubstring("multiple external routers found")))
		})
	})

	Context("get sna config", func() {
		It("should err for nil networkID", func() {
			_, err := GetSNAConfigFromNetworkID(ctx, nw, nil)
			Expect(err).To(HaveOccurred())
		})
		It("should err on subnet lookup error", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(nil, errors.New("subnet error"))
			_, err := GetSNAConfigFromNetworkID(ctx, nw, &networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should err on router lookup error", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(
				[]subnets.Subnet{{ID: subnetID, CIDR: subnetCIDR}}, nil)
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return(nil, errors.New("router error"))
			_, err := GetSNAConfigFromNetworkID(ctx, nw, &networkID)
			Expect(err).To(HaveOccurred())
		})
		It("should succeed for proper network setup", func() {
			nw.EXPECT().ListSubnets(ctx, subnets.ListOpts{NetworkID: networkID}).Return(
				[]subnets.Subnet{{ID: subnetID, CIDR: subnetCIDR}}, nil)
			nw.EXPECT().GetRouterInterfacePortsByNetwork(ctx, networkID).Return([]ports.Port{{DeviceID: routerID}}, nil)
			addMockRouter(nw, routerID, []string{"SNA"})
			config, err := GetSNAConfigFromNetworkID(ctx, nw, &networkID)
			Expect(err).To(Succeed())
			Expect(config).To(Equal(&SNAConfig{
				NetworkID:   networkID,
				RouterID:    routerID,
				SubnetID:    subnetID,
				WorkersCIDR: subnetCIDR,
			}))
		})
	})

	Context("inject config", func() {
		It("should properly inject fields", func() {
			var config stackitv1alpha1.Networks
			snaConfig := &SNAConfig{
				NetworkID:   networkID,
				RouterID:    routerID,
				SubnetID:    subnetID,
				WorkersCIDR: subnetCIDR,
			}
			InjectConfig(&config, snaConfig)
			Expect(config.ID).To(Equal(&snaConfig.NetworkID))
			Expect(config.Router.ID).To(Equal(snaConfig.RouterID))
			Expect(config.SubnetID).To(Equal(&snaConfig.SubnetID))
			Expect(config.Workers).To(Equal(snaConfig.WorkersCIDR))
		})
	})
})

var _ = Describe("IsSNAShoot", func() {
	It("should return true if label is set", func() {
		Expect(IsSNAShoot(map[string]string{"stackit.cloud/area-id": "some-uuid"})).To(BeTrue())
	})
	It("should return false if label is empty", func() {
		Expect(IsSNAShoot(map[string]string{"stackit.cloud/area-id": ""})).To(BeFalse())
	})
	It("should return false if label is missing", func() {
		Expect(IsSNAShoot(nil)).To(BeFalse())
	})
})
