package selfhostedshootexposure

import (
	"context"
	"errors"
	"fmt"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	reconcilerutils "github.com/gardener/gardener/pkg/controllerutils/reconciler"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock"
)

var _ = Describe("reconcileLoadBalancer", func() {
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
				Labels:       map[string]string{"cluster": "shoot--foo--bar"},
				NetworkID:    "network-123",
				PlanId:       "p10",
				SelfHostedShootExposure: &extensionsv1alpha1.SelfHostedShootExposure{
					Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
						Port: 443,
					},
				},
			},
			LBClient:     mockLBClient,
			LoadBalancer: nil,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("when no load balancer exists", func() {
		It("should requeue creation without endpoints", func() {
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{}

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).To(HaveOccurred())
			// Endpoints are populated asynchronously by gardenlet; empty endpoints should trigger a clean requeue,
			// not a fatal error.
			var rae *reconcilerutils.RequeueAfterError
			Expect(errors.As(err, &rae)).To(BeTrue())
			Expect(rae.Cause.Error()).To(ContainSubstring("waiting for endpoints to be populated"))
		})

		It("should create a new load balancer", func() {
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-1",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
					},
				},
				{
					NodeName: "node-2",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.20"},
					},
				},
			}

			createdLB := &loadbalancer.LoadBalancer{
				Name:            new("test-lb"),
				ExternalAddress: new("203.0.113.1"),
			}
			mockLBClient.EXPECT().
				CreateLoadBalancer(ctx, gomock.Any()).
				DoAndReturn(func(_ context.Context, payload loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
					// Verify the payload structure
					Expect(payload.Name).To(Equal(new("test-lb")))
					Expect(payload.PlanId).To(Equal(new("p10")))
					Expect(payload.Networks).To(HaveLen(1))
					Expect(payload.Networks[0].NetworkId).To(Equal(new("network-123")))
					Expect(payload.Listeners).To(HaveLen(1))
					Expect(*payload.Listeners[0].Port).To(BeEquivalentTo(443))
					Expect(payload.TargetPools).To(HaveLen(1))
					Expect(*payload.TargetPools[0].Name).To(Equal("target-pool-control-plane"))
					// Targets should be sorted by IP
					Expect(payload.TargetPools[0].Targets).To(HaveLen(2))
					Expect(*payload.TargetPools[0].Targets[0].Ip).To(Equal("10.0.1.10"))
					Expect(*payload.TargetPools[0].Targets[1].Ip).To(Equal("10.0.1.20"))
					return createdLB, nil
				})

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
			Expect(r.LoadBalancer).To(Equal(createdLB))
		})

		It("should return error when CreateLoadBalancer fails", func() {
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-1",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
					},
				},
			}

			mockLBClient.EXPECT().
				CreateLoadBalancer(ctx, gomock.Any()).
				Return(nil, fmt.Errorf("API error"))

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error creating load balancer"))
		})
	})

	Context("when load balancer already exists", func() {
		BeforeEach(func() {
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-1",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
					},
				},
			}
			r.LoadBalancer = &loadbalancer.LoadBalancer{
				Name:    new("test-lb"),
				PlanId:  new("p10"),
				Version: new("v1"),
				TargetPools: []loadbalancer.TargetPool{
					{
						Name: new("target-pool-control-plane"),
						Targets: []loadbalancer.Target{
							{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
						},
					},
				},
			}
		})

		It("should do nothing if no updates are needed", func() {
			// No mock expectations — nothing should be called
			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should update target pool only when targets changed", func() {
			// Spec has a new node
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-1",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
					},
				},
				{
					NodeName: "node-2",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.20"},
					},
				},
			}

			mockLBClient.EXPECT().
				UpdateLoadBalancerTargetPool(ctx, "test-lb", "target-pool-control-plane", gomock.Any()).
				DoAndReturn(func(_ context.Context, _, _ string, payload loadbalancer.UpdateTargetPoolPayload) (*loadbalancer.TargetPool, error) {
					Expect(payload.Targets).To(HaveLen(2))
					Expect(*payload.Targets[0].Ip).To(Equal("10.0.1.10"))
					Expect(*payload.Targets[1].Ip).To(Equal("10.0.1.20"))
					Expect(*payload.TargetPort).To(BeEquivalentTo(443))
					return &loadbalancer.TargetPool{}, nil
				})

			// After the target pool write, reconcileLoadBalancer re-GETs the LB so downstream
			// readiness checks see the post-write status (STACKIT flips the LB to PENDING).
			mockLBClient.EXPECT().
				GetLoadBalancer(ctx, "test-lb").
				Return(&loadbalancer.LoadBalancer{
					Name:   new("test-lb"),
					Status: new("STATUS_PENDING"),
				}, nil)

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should update plan via UpdateLoadBalancer when only plan changed", func() {
			r.PlanId = "p100" // Changed plan

			mockLBClient.EXPECT().
				UpdateLoadBalancer(ctx, "test-lb", gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
					Expect(payload.PlanId).To(Equal(new("p100")))
					Expect(payload.Version).To(Equal(new("v1")))
					// STACKIT UpdateLoadBalancer has PUT semantics — full desired state is required,
					// so TargetPools (and the other invariant fields) must be present even when only
					// the plan changed.
					Expect(payload.TargetPools).To(HaveLen(1))
					Expect(payload.Networks).To(HaveLen(1))
					Expect(payload.Listeners).To(HaveLen(1))
					return &loadbalancer.LoadBalancer{}, nil
				})

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should update plan and targets in a single call when both changed", func() {
			r.PlanId = "p100"
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-3",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.30"},
					},
				},
			}

			mockLBClient.EXPECT().
				UpdateLoadBalancer(ctx, "test-lb", gomock.Any()).
				DoAndReturn(func(_ context.Context, _ string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
					Expect(payload.PlanId).To(Equal(new("p100")))
					Expect(payload.Version).To(Equal(new("v1")))
					Expect(payload.TargetPools).To(HaveLen(1))
					Expect(*payload.TargetPools[0].Targets[0].Ip).To(Equal("10.0.1.30"))
					return &loadbalancer.LoadBalancer{}, nil
				})

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error when UpdateLoadBalancerTargetPool fails", func() {
			r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
				{
					NodeName: "node-new",
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeInternalIP, Address: "10.0.1.99"},
					},
				},
			}

			mockLBClient.EXPECT().
				UpdateLoadBalancerTargetPool(ctx, "test-lb", "target-pool-control-plane", gomock.Any()).
				Return(nil, fmt.Errorf("API error"))

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error updating load balancer target pool"))
		})

		It("should return error when UpdateLoadBalancer fails", func() {
			r.PlanId = "p100"

			mockLBClient.EXPECT().
				UpdateLoadBalancer(ctx, "test-lb", gomock.Any()).
				Return(nil, fmt.Errorf("API error"))

			err := r.reconcileLoadBalancer(ctx, logger)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("error updating load balancer"))
		})
	})
})

var _ = Describe("deleteLoadBalancer", func() {
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
			LBClient:     mockLBClient,
			LoadBalancer: nil,
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should return nil if load balancer is not set (idempotent)", func() {
		r.LoadBalancer = nil

		err := r.deleteLoadBalancer(ctx, logger)

		Expect(err).NotTo(HaveOccurred())
	})

	It("should delete the load balancer if it exists", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			Name: new("test-lb"),
		}

		mockLBClient.EXPECT().
			DeleteLoadBalancer(ctx, "test-lb").
			Return(nil)

		err := r.deleteLoadBalancer(ctx, logger)

		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error when DeleteLoadBalancer fails", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			Name: new("test-lb"),
		}

		mockLBClient.EXPECT().
			DeleteLoadBalancer(ctx, "test-lb").
			Return(fmt.Errorf("API error"))

		err := r.deleteLoadBalancer(ctx, logger)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("error deleting load balancer"))
	})
})

var _ = Describe("buildTargets", func() {
	var r *Resources

	BeforeEach(func() {
		r = &Resources{
			Options: Options{
				SelfHostedShootExposure: &extensionsv1alpha1.SelfHostedShootExposure{
					Spec: extensionsv1alpha1.SelfHostedShootExposureSpec{
						Port:      443,
						Endpoints: []extensionsv1alpha1.ControlPlaneEndpoint{},
					},
				},
			},
		}
	})

	It("should extract InternalIP addresses and sort them", func() {
		r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
			{
				NodeName: "node-2",
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "10.0.1.20"},
				},
			},
			{
				NodeName: "node-1",
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
				},
			},
		}

		targets, err := r.buildTargets()

		Expect(err).NotTo(HaveOccurred())
		Expect(targets).To(HaveLen(2))
		Expect(*targets[0].Ip).To(Equal("10.0.1.10"))
		Expect(*targets[0].DisplayName).To(Equal("node-1"))
		Expect(*targets[1].Ip).To(Equal("10.0.1.20"))
		Expect(*targets[1].DisplayName).To(Equal("node-2"))
	})

	It("should error when endpoint has no InternalIP", func() {
		r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
			{
				NodeName: "node-1",
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeHostName, Address: "node-1"},
				},
			},
		}

		targets, err := r.buildTargets()

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no InternalIP address"))
		Expect(targets).To(BeNil())
	})

	It("should handle empty endpoints", func() {
		r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{}

		targets, err := r.buildTargets()

		Expect(err).NotTo(HaveOccurred())
		Expect(targets).To(BeEmpty())
	})

	It("should select InternalIP when multiple address types present", func() {
		r.SelfHostedShootExposure.Spec.Endpoints = []extensionsv1alpha1.ControlPlaneEndpoint{
			{
				NodeName: "node-1",
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeHostName, Address: "node-1.example.com"},
					{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
					{Type: corev1.NodeInternalIP, Address: "10.0.1.10"},
				},
			},
		}

		targets, err := r.buildTargets()

		Expect(err).NotTo(HaveOccurred())
		Expect(targets).To(HaveLen(1))
		Expect(*targets[0].Ip).To(Equal("10.0.1.10"))
	})
})

var _ = Describe("targetsEqual", func() {
	It("should return true for equal target lists", func() {
		a := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}

		Expect(targetsEqual(a, b)).To(BeTrue())
	})

	It("should return false for different IPs", func() {
		a := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.99"), DisplayName: new("node-1")},
		}

		Expect(targetsEqual(a, b)).To(BeFalse())
	})

	It("should return false for different display names", func() {
		a := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-2")},
		}

		Expect(targetsEqual(a, b)).To(BeFalse())
	})

	It("should return false for different lengths", func() {
		a := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
			{Ip: new("10.0.1.20"), DisplayName: new("node-2")},
		}

		Expect(targetsEqual(a, b)).To(BeFalse())
	})

	It("should handle empty target lists", func() {
		Expect(targetsEqual([]loadbalancer.Target{}, []loadbalancer.Target{})).To(BeTrue())
	})

	It("should return false when IP is nil", func() {
		a := []loadbalancer.Target{
			{Ip: nil, DisplayName: new("node-1")},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}

		Expect(targetsEqual(a, b)).To(BeFalse())
	})

	It("should return false when DisplayName is nil", func() {
		a := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: nil},
		}
		b := []loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		}

		Expect(targetsEqual(a, b)).To(BeFalse())
	})
})

var _ = Describe("targetPoolNeedsUpdate", func() {
	var r *Resources

	BeforeEach(func() {
		r = &Resources{}
	})

	It("should return false when no load balancer exists", func() {
		r.LoadBalancer = nil

		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeFalse())
	})

	It("should return true when LB has no target pools but spec has targets", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{},
		}

		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeTrue())
	})

	It("should return false when LB has no target pools and spec has no targets", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{},
		}

		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeFalse())
	})

	It("should return error when target pool has unexpected name", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{
				{Name: new("wrong-name")},
			},
		}

		_, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unexpected target pool name"))
	})

	It("should return false when targets match", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{
				{
					Name: new("target-pool-control-plane"),
					Targets: []loadbalancer.Target{
						{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
					},
				},
			},
		}

		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeFalse())
	})

	It("should return true when targets differ", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{
				{
					Name: new("target-pool-control-plane"),
					Targets: []loadbalancer.Target{
						{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
					},
				},
			},
		}

		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
			{Ip: new("10.0.1.20"), DisplayName: new("node-2")},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeTrue())
	})

	It("should compare correctly regardless of LB target order", func() {
		r.LoadBalancer = &loadbalancer.LoadBalancer{
			TargetPools: []loadbalancer.TargetPool{
				{
					Name: new("target-pool-control-plane"),
					Targets: []loadbalancer.Target{
						// LB returns targets in reverse order
						{Ip: new("10.0.1.20"), DisplayName: new("node-2")},
						{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
					},
				},
			},
		}

		// Spec targets are sorted
		needsUpdate, err := r.targetPoolNeedsUpdate([]loadbalancer.Target{
			{Ip: new("10.0.1.10"), DisplayName: new("node-1")},
			{Ip: new("10.0.1.20"), DisplayName: new("node-2")},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(needsUpdate).To(BeFalse())
	})
})
