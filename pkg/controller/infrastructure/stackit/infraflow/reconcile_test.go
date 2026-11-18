package infraflow

import (
	"context"
	"errors"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
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

	Describe("#ensureSecGroupRules", func() {
		var (
			ctx      context.Context
			ctrl     *gomock.Controller
			mockIaaS *mockclient.MockIaaSClient
			fctx     *FlowContext
			group    *iaas.SecurityGroup
		)

		BeforeEach(func() {
			ctx = context.Background()
			ctrl = gomock.NewController(GinkgoT())
			mockIaaS = mockclient.NewMockIaaSClient(ctrl)

			group = &iaas.SecurityGroup{
				Id:   new("security-group-id"),
				Name: "shoot--foo--bar",
			}

			whiteboard := shared.NewWhiteboard()
			whiteboard.SetObject(ObjectSecGroup, group)

			fctx = &FlowContext{
				state:       whiteboard,
				iaasClient:  mockIaaS,
				technicalID: "shoot--foo--bar",
				cluster: &extensionscontroller.Cluster{
					Shoot: &gardencorev1beta1.Shoot{},
				},
			}
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("reconciles desired security group rules including nodeports and kube-proxy health check", func() {
			expectedRules := []iaas.SecurityGroupRule{
				{
					Direction:             stackit.DirectionIngress,
					Ethertype:             new(stackit.EtherTypeIPv4),
					RemoteSecurityGroupId: new("security-group-id"),
					Description:           new("IPv4: allow all incoming traffic within the same security group"),
				},
				{
					Direction:   stackit.DirectionEgress,
					Ethertype:   new(stackit.EtherTypeIPv4),
					Description: new("IPv4: allow all outgoing traffic"),
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
					Description: new("IPv4: allow all incoming tcp traffic with port range 30000-32767"),
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
					Description: new("IPv4: allow all incoming udp traffic with port range 30000-32767"),
				},
				{
					Direction: stackit.DirectionIngress,
					Ethertype: new(stackit.EtherTypeIPv4),
					Protocol:  new(stackit.ProtocolTCP),
					PortRange: &iaas.PortRange{
						Max: int64(10256),
						Min: int64(10256),
					},
					IpRange:     new("0.0.0.0/0"),
					Description: new("IPv4: allow incoming tcp traffic to kube-proxy healthcheck port"),
				},
			}

			mockIaaS.EXPECT().UpdateSecurityGroupRules(ctx, group, expectedRules, gomock.Any()).Return(false, nil)

			err := fctx.ensureSecGroupRules(ctx)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("#ensureIsolatedNetwork", func() {
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
				log:         logr.Discard(),
				config: &stackitv1alpha1.InfrastructureConfig{
					Networks: stackitv1alpha1.Networks{
						Workers: "10.250.0.0/16",
					},
				},
				cloudProfileConfig: &stackitv1alpha1.CloudProfileConfig{
					DNSServers: []string{"8.8.8.8"},
				},
			}
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("creates the network when it does not exist", func() {
			mockIaaS.EXPECT().GetNetworkByName(ctx, "shoot--foo--bar").Return([]iaas.Network{}, nil)
			mockIaaS.EXPECT().CreateIsolatedNetwork(ctx, gomock.Any()).Return(&iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Ipv4: &iaas.NetworkIPv4{
					Nameservers: []string{"8.8.8.8"},
				},
			}, nil)

			err := fctx.ensureIsolatedNetwork(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(fctx.state.Get(IdentifierNetwork)).To(Equal(new("net-id")))
		})

		It("does not update the network when settings match", func() {
			currentNet := &iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Dhcp: new(true),
				Ipv4: &iaas.NetworkIPv4{
					Nameservers: []string{"8.8.8.8"},
				},
			}
			mockIaaS.EXPECT().GetNetworkByName(ctx, "shoot--foo--bar").Return([]iaas.Network{*currentNet}, nil)
			mockIaaS.EXPECT().UpdateNetwork(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

			err := fctx.ensureIsolatedNetwork(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(fctx.state.Get(IdentifierNetwork)).To(Equal(new("net-id")))
		})

		It("updates the network when settings do not match", func() {
			currentNet := &iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Dhcp: new(true),
				Ipv4: &iaas.NetworkIPv4{
					Nameservers: []string{"1.1.1.1"},
				},
			}
			mockIaaS.EXPECT().GetNetworkByName(ctx, "shoot--foo--bar").Return([]iaas.Network{*currentNet}, nil)
			mockIaaS.EXPECT().UpdateNetwork(ctx, "net-id", gomock.Any()).Return(nil, nil)

			err := fctx.ensureIsolatedNetwork(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(fctx.state.Get(IdentifierNetwork)).To(Equal(new("net-id")))
		})
	})

	Describe("#ensureNetworkReady", func() {
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
				log:         logr.Discard(),
				config:      &stackitv1alpha1.InfrastructureConfig{},
			}
			fctx.state.Set(IdentifierNetwork, "net-id")
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("succeeds and populates state when prefixes and egress IP exist", func() {
			net := &iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Ipv4: &iaas.NetworkIPv4{
					Prefixes:    []string{"10.250.0.0/16"},
					Nameservers: []string{"8.8.8.8"},
					PublicIp:    new("1.2.3.4"),
				},
			}
			mockIaaS.EXPECT().GetNetworkById(ctx, "net-id").Return(net, nil)

			err := fctx.ensureNetworkReady(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(fctx.state.Get(NameNetwork)).To(Equal(new("shoot--foo--bar")))
			Expect(fctx.dnsNameservers).To(Equal(new([]string{"8.8.8.8"})))
			Expect(fctx.egressCIDRs).To(Equal([]string{"1.2.3.4/32"}))
		})

		It("returns error when no prefixes are found", func() {
			net := &iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Ipv4: &iaas.NetworkIPv4{
					Prefixes: []string{},
				},
			}
			mockIaaS.EXPECT().GetNetworkById(ctx, "net-id").Return(net, nil)

			err := fctx.ensureNetworkReady(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no prefixes found for network 'net-id'"))
		})

		It("returns error when egress IP is not found", func() {
			net := &iaas.Network{
				Id:   "net-id",
				Name: "shoot--foo--bar",
				Ipv4: &iaas.NetworkIPv4{
					Prefixes: []string{"10.250.0.0/16"},
				},
			}
			mockIaaS.EXPECT().GetNetworkById(ctx, "net-id").Return(net, nil)

			err := fctx.ensureNetworkReady(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("egress IP not found for network: net-id"))
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
