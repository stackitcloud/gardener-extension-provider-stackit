package infraflow

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	corev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/access"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	infrainternal "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/internal/infrastructure"
	osclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack/client"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

const (

	// NameFloatingNetwork is the key for the floating network name
	NameFloatingNetwork = "FloatingNetworkName"
	// IdentifierFloatingNetwork is the key for the floating network id
	IdentifierFloatingNetwork = "FloatingNetwork"
	// IdentifierNetwork is the key for the network id
	IdentifierNetwork = "Network"
	// NameNetwork is the name of the network
	NameNetwork = "NetworkName"
	// IdentifierSecGroup is the key for the security group id
	IdentifierSecGroup = "SecurityGroup"
	// ObjectSecGroup is the key for the cached security group
	ObjectSecGroup = "SecurityGroup"
	// NameSecGroup is the name of the security group
	NameSecGroup = "SecurityGroupName"
	// IdentifierSubnet is the key for the subnet id
	IdentifierSubnet = "Subnet"
	// IdentifierEgressCIDRs is the key for the slice containing egress CIDRs strings.
	IdentifierEgressCIDRs = "EgressCIDRs"
	// NameKeyPair is the key for the name of the EC2 key pair resource
	NameKeyPair = "KeyPair"
)

// Opts contain options to initiliaze a FlowContext
type Opts struct {
	Log                logr.Logger
	Infrastructure     *extensionsv1alpha1.Infrastructure
	Cluster            *extensionscontroller.Cluster
	ClientFactory      osclient.Factory
	State              *stackitv1alpha1.InfrastructureState
	Client             client.Client
	StackitLB          stackitclient.LoadBalancingClient
	IaaSClient         stackitclient.IaaSClient
	UseOpenStackClient bool
	CustomLabelDomain  string
}

type FlowContext struct {
	state                   shared.Whiteboard
	client                  client.Client
	log                     logr.Logger
	infra                   *extensionsv1alpha1.Infrastructure
	config                  *stackitv1alpha1.InfrastructureConfig
	cloudProfileConfig      *stackitv1alpha1.CloudProfileConfig
	cluster                 *extensionscontroller.Cluster
	networkSpec             *corev1beta1.Networking
	access                  access.NetworkingAccess
	compute                 osclient.Compute
	networking              osclient.Networking
	isSNAShoot              bool
	nodesCIDR               *string
	dnsNameservers          *[]string
	stackitLB               stackitclient.LoadBalancingClient
	iaasClient              stackitclient.IaaSClient
	hasStackitMCM           bool
	hasOpenStackCredentials bool
	technicalID             string

	*shared.BasicFlowContext
}

// NewFlowContext creates a new FlowContext object
func NewFlowContext(ctx context.Context, opts Opts) (*FlowContext, error) {
	whiteboard := shared.NewWhiteboard()
	if opts.State != nil {
		whiteboard.ImportFromFlatMap(opts.State.Data)
	}

	infraConfig, err := helper.InfrastructureConfigFromInfrastructure(opts.Infrastructure)
	if err != nil {
		return nil, err
	}
	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(opts.Cluster)
	if err != nil {
		return nil, err
	}
	var networkSpec *corev1beta1.Networking
	if opts.Cluster != nil && opts.Cluster.Shoot.Spec.Networking != nil {
		networkSpec = opts.Cluster.Shoot.Spec.Networking
	}
	var isSNAShoot bool
	if opts.Cluster != nil {
		isSNAShoot = infrainternal.IsSNAShoot(opts.Cluster.Shoot.Labels)
	}

	flowContext := &FlowContext{
		state:                   whiteboard,
		infra:                   opts.Infrastructure,
		config:                  infraConfig,
		cloudProfileConfig:      cloudProfileConfig,
		networkSpec:             networkSpec,
		isSNAShoot:              isSNAShoot,
		log:                     opts.Log,
		client:                  opts.Client,
		cluster:                 opts.Cluster,
		stackitLB:               opts.StackitLB,
		iaasClient:              opts.IaaSClient,
		hasStackitMCM:           feature.UseStackitMachineControllerManager(opts.Cluster),
		hasOpenStackCredentials: opts.UseOpenStackClient,
		technicalID:             opts.Cluster.Shoot.Status.TechnicalID,
	}

	// Check if we have a valid ClientFactory
	if opts.UseOpenStackClient {
		networking, err := opts.ClientFactory.Networking(osclient.WithRegion(opts.Infrastructure.Spec.Region))
		if err != nil {
			return nil, fmt.Errorf("creating networking client failed: %w", err)
		}
		networkingAccess, err := access.NewNetworkingAccess(ctx, networking, opts.Log)
		if err != nil {
			return nil, fmt.Errorf("creating networking networkingAccess failed: %w", err)
		}
		compute, err := opts.ClientFactory.Compute(osclient.WithRegion(opts.Infrastructure.Spec.Region))
		if err != nil {
			return nil, fmt.Errorf("creating compute client failed: %w", err)
		}

		flowContext.networking = networking
		flowContext.compute = compute
		flowContext.access = networkingAccess
	}

	return flowContext, nil
}

func (fctx *FlowContext) persistState(ctx context.Context) error {
	// status is nil such that there's no need to pass the nodesCIDR
	return infrainternal.PatchProviderStatusAndState(ctx, fctx.client, fctx.infra, nil, nil, fctx.computeInfrastructureState())
}

func (fctx *FlowContext) computeInfrastructureStatus() *stackitv1alpha1.InfrastructureStatus {
	status := &stackitv1alpha1.InfrastructureStatus{
		TypeMeta: infrainternal.StatusTypeMeta,
	}

	status.Networks.FloatingPool.ID = ptr.Deref(fctx.state.Get(IdentifierFloatingNetwork), "")
	status.Networks.FloatingPool.Name = ptr.Deref(fctx.state.Get(NameFloatingNetwork), "")

	status.Networks.ID = ptr.Deref(fctx.state.Get(IdentifierNetwork), "")
	status.Networks.Name = ptr.Deref(fctx.state.Get(NameNetwork), "")

	status.Networks.Router.ExternalFixedIPs = fctx.state.GetObject(IdentifierEgressCIDRs).([]string)
	// backwards compatibility change for the deprecated field
	if len(status.Networks.Router.ExternalFixedIPs) > 0 {
		status.Networks.Router.IP = status.Networks.Router.ExternalFixedIPs[0]
	}

	status.Node.KeyName = ptr.Deref(fctx.state.Get(NameKeyPair), "")

	if v := fctx.state.Get(IdentifierSubnet); v != nil {
		status.Networks.Subnets = []stackitv1alpha1.Subnet{
			{
				Purpose:        stackitv1alpha1.PurposeNodes,
				ID:             *v,
				DNSNameservers: fctx.dnsNameservers,
			},
		}
	}

	if v := fctx.state.Get(IdentifierSecGroup); v != nil {
		status.SecurityGroups = []stackitv1alpha1.SecurityGroup{
			{
				Purpose: stackitv1alpha1.PurposeNodes,
				ID:      *v,
				Name:    ptr.Deref(fctx.state.Get(NameSecGroup), ""),
			},
		}
	}

	return status
}

func (fctx *FlowContext) computeInfrastructureState() *runtime.RawExtension {
	return &runtime.RawExtension{
		Object: &stackitv1alpha1.InfrastructureState{
			TypeMeta: metav1.TypeMeta{
				APIVersion: stackitv1alpha1.SchemeGroupVersion.String(),
				Kind:       "InfrastructureState",
			},
			Data: fctx.state.ExportAsFlatMap(),
		},
	}
}
