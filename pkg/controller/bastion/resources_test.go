package bastion

import (
	"context"
	"net/http"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
	mock "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client/mock"
)

var _ = Describe("Bastion Resources", func() {
	var (
		ctx       context.Context
		logSink   *inMemoryLogSink
		logger    logr.Logger
		mockCtrl  *gomock.Controller
		mockIaaS  *mock.MockIaaSClient
		resources *Resources
	)

	BeforeEach(func() {
		ctx = context.Background()
		logSink = newInMemoryLogSink()
		logger = logr.New(logSink)
		mockCtrl = gomock.NewController(GinkgoT())
		mockIaaS = mock.NewMockIaaSClient(mockCtrl)
		resources = &Resources{
			Options:       Options{},
			IaaS:          mockIaaS,
			SecurityGroup: nil,
			Server:        nil,
			PublicIP:      nil,
		}
	})

	Context("getExistingResources", func() {
		It("populates security group, server, and public IP", func() {
			resources.ResourceName = "test-resource"
			resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}

			expectedSecurityGroup := []iaas.SecurityGroup{{Name: ptr.To("test-security-group")}}
			mockIaaS.EXPECT().GetSecurityGroupByName(ctx, resources.ResourceName).Return(expectedSecurityGroup, nil)

			expectedServer := []iaas.Server{{Name: ptr.To("test-server")}}
			mockIaaS.EXPECT().GetServerByName(ctx, resources.ResourceName).Return(expectedServer, nil)

			expectedPublicIP := []iaas.PublicIp{{Id: ptr.To("test-ip")}}
			mockIaaS.EXPECT().GetPublicIpByLabels(ctx, resources.Labels).Return(expectedPublicIP, nil)

			err := resources.getExistingResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(resources.SecurityGroup).To(Equal(&expectedSecurityGroup[0]))
			Expect(resources.Server).To(Equal(&expectedServer[0]))
			Expect(resources.PublicIP).To(Equal(&expectedPublicIP[0]))
		})

		It("logs the populated security group, server, and public IP ids", func() {
			resources.ResourceName = "test-resource"
			resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}

			mockIaaS.EXPECT().GetSecurityGroupByName(ctx, resources.ResourceName).Return(
				[]iaas.SecurityGroup{{Id: ptr.To("test-security-group")}},
				nil,
			)
			mockIaaS.EXPECT().GetServerByName(ctx, resources.ResourceName).Return(
				[]iaas.Server{{Id: ptr.To("test-server")}},
				nil,
			)
			mockIaaS.EXPECT().GetPublicIpByLabels(ctx, resources.Labels).Return(
				[]iaas.PublicIp{{Id: ptr.To("test-ip")}},
				nil,
			)

			err := resources.getExistingResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-security-group"))
			Expect(logSink.Buf.String()).To(ContainSubstring("test-server"))
			Expect(logSink.Buf.String()).To(ContainSubstring("test-ip"))
		})

		It("ignores NotFound errors", func() {
			resources.ResourceName = "test-resource"
			resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}

			mockIaaS.EXPECT().GetSecurityGroupByName(ctx, resources.ResourceName).Return([]iaas.SecurityGroup{}, nil)
			mockIaaS.EXPECT().GetServerByName(ctx, resources.ResourceName).Return([]iaas.Server{}, nil)
			mockIaaS.EXPECT().GetPublicIpByLabels(ctx, resources.Labels).Return([]iaas.PublicIp{}, nil)

			err := resources.getExistingResources(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(resources.SecurityGroup).To(BeNil())
			Expect(resources.Server).To(BeNil())
			Expect(resources.PublicIP).To(BeNil())

			Expect(logSink.Buf.String()).ToNot(ContainSubstring("error getting security group"))
			Expect(logSink.Buf.String()).ToNot(ContainSubstring("error getting server"))
			Expect(logSink.Buf.String()).ToNot(ContainSubstring("error getting public IP"))
		})

	})

	Context("reconcilePublicIP", func() {
		When("public IP is nil", func() {
			It("creates a new public IP", func() {
				resources.ResourceName = "test-resource"
				resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}
				resources.Server = &iaas.Server{
					Id: ptr.To("test-server"),
				}

				expectedPublicIP := &iaas.PublicIp{Id: ptr.To("test-public-ip")}
				mockIaaS.EXPECT().CreatePublicIp(ctx, gomock.Any()).Return(expectedPublicIP, nil)
				mockIaaS.EXPECT().AddPublicIpToServer(ctx, "test-server", "test-public-ip")

				err := resources.reconcilePublicIP(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Expect(resources.PublicIP).To(Equal(expectedPublicIP))
			})

			It("logs the created public IP's id", func() {
				resources.ResourceName = "test-resource"
				resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}
				resources.Server = &iaas.Server{
					Id: ptr.To("test-server"),
				}

				expectedPublicIP := &iaas.PublicIp{Id: ptr.To("test-public-ip")}
				mockIaaS.EXPECT().CreatePublicIp(ctx, gomock.Any()).Return(expectedPublicIP, nil)
				mockIaaS.EXPECT().AddPublicIpToServer(ctx, "test-server", "test-public-ip")

				err := resources.reconcilePublicIP(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Expect(logSink.Buf.String()).To(ContainSubstring("Created public IP"))
				Expect(logSink.Buf.String()).To(ContainSubstring("test-public-ip"))
			})
		})

		It("does not add the public IP to the server if the public IP is already associated with a network interface", func() {
			resources.ResourceName = "test-resource"
			resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}
			resources.Server = &iaas.Server{
				Id: ptr.To("test-server"),
			}
			resources.PublicIP = &iaas.PublicIp{
				Id:               ptr.To("test-public-ip"),
				NetworkInterface: iaas.NewNullableString(ptr.To("test-interface")),
			}

			err := resources.reconcilePublicIP(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-interface"))
			Expect(logSink.Buf.String()).ToNot(ContainSubstring("test-server"))
		})
	})

	Context("deletePublicIP", func() {
		It("bails out if the public IP is nil", func() {
			err := resources.deletePublicIP(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).ToNot(ContainSubstring("publicIP"))
		})

		It("deletes the public IP", func() {
			resources.ResourceName = "test-resource"
			resources.PublicIP = &iaas.PublicIp{
				Id: ptr.To("test-public-ip"),
			}

			mockIaaS.EXPECT().DeletePublicIp(ctx, "test-public-ip").Return(nil)

			err := resources.deletePublicIP(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-public-ip"))
		})
	})

	Context("reconcileServer", func() {
		It("bails out if the server already set", func() {
			resources.Server = &iaas.Server{
				Id: ptr.To("test-server"),
			}

			err := resources.reconcileServer(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).ToNot(ContainSubstring("test-server"))
		})

		It("creates a server based on the resource's options", func() {
			resources.Options = Options{
				ResourceName: "test-resource",
				Labels: map[string]string{
					"test-label-key": "test-label-value",
				},
				AvailabilityZone: "test-az",
				MachineType:      "test-machine",
				ImageID:          "test-image",
				NetworkID:        "test-network",
				Bastion: &extensionsv1alpha1.Bastion{
					Spec: extensionsv1alpha1.BastionSpec{
						UserData: []byte{1, 2, 3, 4},
					},
				},
			}
			resources.SecurityGroup = &iaas.SecurityGroup{Id: ptr.To("test-security-group")}

			expectedPayload := iaas.CreateServerPayload{
				Name: ptr.To("test-resource"),
				Labels: ptr.To(stackit.ToLabels(map[string]string{
					"test-label-key": "test-label-value",
				})),
				AvailabilityZone: ptr.To("test-az"),
				MachineType:      ptr.To("test-machine"),
				BootVolume: &iaas.ServerBootVolume{
					DeleteOnTermination: ptr.To(true),
					Source:              iaas.NewBootVolumeSource("test-image", "image"),
					Size:                ptr.To[int64](10),
				},
				SecurityGroups: ptr.To([]string{"test-security-group"}),
				Networking: ptr.To(iaas.CreateServerNetworkingAsCreateServerPayloadAllOfNetworking(&iaas.CreateServerNetworking{
					NetworkId: ptr.To("test-network"),
				})),

				UserData: ptr.To([]byte{1, 2, 3, 4}),
			}
			expectedServer := &iaas.Server{
				Id: ptr.To("test-server"),
			}

			mockIaaS.EXPECT().CreateServer(ctx, expectedPayload).Return(expectedServer, nil)

			err := resources.reconcileServer(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(resources.Server).To(Equal(expectedServer))
			Expect(logSink.Buf.String()).To(ContainSubstring("test-server"))
		})
	})

	Context("deleteServer", func() {
		It("bails out if the server is nil", func() {
			err := resources.deleteServer(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).ToNot(ContainSubstring("Deleted server"))
		})

		It("deletes the server", func() {
			resources.ResourceName = "test-resource"
			resources.Server = &iaas.Server{
				Id: ptr.To("test-server"),
			}

			mockIaaS.EXPECT().DeleteServer(ctx, "test-server")

			err := resources.deleteServer(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-server"))
		})
	})

	Context("reconcileSecurityGroup", func() {
		When("security group is nil", func() {
			It("creates a new security group and reconciles the corresponding rules", func() {
				resources.ResourceName = "test-resource"
				resources.Labels = map[string]string{"test-labels-key": "test-labels-value"}
				resources.Bastion = &extensionsv1alpha1.Bastion{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-bastion",
					},
					Spec: extensionsv1alpha1.BastionSpec{
						Ingress: nil, // keep it simple, just to make determineWantedSecurityGroupRules succeed
					},
				}

				expectedPayload := iaas.CreateSecurityGroupPayload{
					Name:        ptr.To("test-resource"),
					Labels:      ptr.To(stackit.ToLabels(map[string]string{"test-labels-key": "test-labels-value"})),
					Description: ptr.To("Security group for Bastion test-bastion"),
				}
				expectedSecurityGroup := &iaas.SecurityGroup{
					Id: ptr.To("test-security-group"),
				}
				expectedWantedRules, _ := resources.determineWantedSecurityGroupRules()

				mockIaaS.EXPECT().CreateSecurityGroup(ctx, expectedPayload).Return(expectedSecurityGroup, nil)
				mockIaaS.EXPECT().ReconcileSecurityGroupRules(ctx, logger, expectedSecurityGroup, expectedWantedRules)

				err := resources.reconcileSecurityGroup(ctx, logger)
				Expect(err).ToNot(HaveOccurred())

				Expect(resources.SecurityGroup).To(Equal(expectedSecurityGroup))
				Expect(logSink.Buf.String()).To(ContainSubstring("test-security-group"))
				Expect(expectedWantedRules).To(HaveLen(4))
			})
		})
	})

	Context("deleteSecurityGroup", func() {
		It("bails out if the security group is nil", func() {
			err := resources.deleteSecurityGroup(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).ToNot(ContainSubstring("securityGroup"))
		})

		It("deletes the security group", func() {
			resources.ResourceName = "test-resource"
			resources.SecurityGroup = &iaas.SecurityGroup{
				Id: ptr.To("test-security-group"),
			}

			mockIaaS.EXPECT().DeleteSecurityGroup(ctx, "test-security-group").Return(nil)

			err := resources.deleteSecurityGroup(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-security-group"))

		})
	})

	Context("reconcileWorkerSecurityGroupRule", func() {
		It("ignores conflicting security group rules", func() {
			resources.SecurityGroup = &iaas.SecurityGroup{
				Id: ptr.To("test-security-group"),
			}
			resources.WorkerSecurityGroupID = "test-rule"
			resources.Bastion = &extensionsv1alpha1.Bastion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-bastion",
				},
			}
			conflictingError := &stackitclient.Error{
				StatusCode: http.StatusConflict,
			}
			mockIaaS.EXPECT().CreateSecurityGroupRule(ctx, "test-rule", gomock.Any()).Return(nil, conflictingError)

			err := resources.reconcileWorkerSecurityGroupRule(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("test-rule"))
			Expect(logSink.Buf.String()).To(ContainSubstring("already exists"))
		})

		It("logs the created security group rule's ID", func() {
			resources.SecurityGroup = &iaas.SecurityGroup{
				Id: ptr.To("test-security-group"),
			}
			resources.WorkerSecurityGroupID = "worker-security-group"
			resources.Bastion = &extensionsv1alpha1.Bastion{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-bastion",
				},
			}

			expectedRule := &iaas.SecurityGroupRule{
				Id:          ptr.To("expected-rule"),
				Description: ptr.To("expected-rule-description"),
			}
			mockIaaS.EXPECT().CreateSecurityGroupRule(ctx, "worker-security-group", gomock.Any()).Return(expectedRule, nil)

			err := resources.reconcileWorkerSecurityGroupRule(ctx, logger)
			Expect(err).ToNot(HaveOccurred())

			Expect(logSink.Buf.String()).To(ContainSubstring("worker-security-group"))
			Expect(logSink.Buf.String()).To(ContainSubstring("expected-rule"))
			Expect(logSink.Buf.String()).To(ContainSubstring("expected-rule-description"))
		})
	})
})
