// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

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

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/openstack/infraflow/access"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/openstack/infraflow/shared"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/feature"
	infrainternal "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/internal/infrastructure"
	osclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack/client"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
)

const (

	// IdentifierRouter is the key for the router id
	IdentifierRouter = "Router"
	// IdentifierNetwork is the key for the network id
	IdentifierNetwork = "Network"
	// IdentifierSubnet is the key for the subnet id
	IdentifierSubnet = "Subnet"
	// IdentifierFloatingNetwork is the key for the floating network id
	IdentifierFloatingNetwork = "FloatingNetwork"
	// IdentifierSecGroup is the key for the security group id
	IdentifierSecGroup = "SecurityGroup"
	// IdentifierEgressCIDRs is the key for the slice containing egress CIDRs strings.
	IdentifierEgressCIDRs = "EgressCIDRs"

	// NameFloatingNetwork is the key for the floating network name
	NameFloatingNetwork = "FloatingNetworkName"
	// NameFloatingPoolSubnet is the name/regex for the floating pool subnets
	NameFloatingPoolSubnet = "FloatingPoolSubnetName"
	// NameNetwork is the name of the network
	NameNetwork = "NetworkName"
	// NameKeyPair is the key for the name of the EC2 key pair resource
	NameKeyPair = "KeyPair"
	// NameSecGroup is the name of the security group
	NameSecGroup = "SecurityGroupName"

	// RouterIP is the key for the router IP address
	RouterIP = "RouterIP"

	// ObjectSecGroup is the key for the cached security group
	ObjectSecGroup = "SecurityGroup"

	// CreatedResourcesExistKey marks that there are infrastructure resources created by Gardener.
	CreatedResourcesExistKey = "resource_exist"
)

// Opts contain options to initiliaze a FlowContext
type Opts struct {
	Log            logr.Logger
	ClientFactory  osclient.Factory
	Infrastructure *extensionsv1alpha1.Infrastructure
	Cluster        *extensionscontroller.Cluster
	State          *stackitv1alpha1.InfrastructureState
	Client         client.Client
	StackitLB      stackitclient.LoadBalancingClient
	IaaSClient     stackitclient.IaaSClient
}

// FlowContext contains the logic to reconcile or delete the infrastructure.
type FlowContext struct {
	state              shared.Whiteboard
	client             client.Client
	log                logr.Logger
	infra              *extensionsv1alpha1.Infrastructure
	config             *stackitv1alpha1.InfrastructureConfig
	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig
	networkSpec        *corev1beta1.Networking
	isSNAShoot         bool
	nodesCIDR          *string
	dnsNameservers     *[]string
	networking         osclient.Networking
	loadbalancing      osclient.Loadbalancing
	access             access.NetworkingAccess
	compute            osclient.Compute
	stackitLB          stackitclient.LoadBalancingClient
	iaasClient         stackitclient.IaaSClient
	hasStackitMCM      bool
	technicalID        string

	*shared.BasicFlowContext
}

// NewFlowContext creates a new FlowContext object
func NewFlowContext(ctx context.Context, opts Opts) (*FlowContext, error) {
	whiteboard := shared.NewWhiteboard()
	if opts.State != nil {
		whiteboard.ImportFromFlatMap(opts.State.Data)
	}

	networking, err := opts.ClientFactory.Networking(osclient.WithRegion(opts.Infrastructure.Spec.Region))
	if err != nil {
		return nil, fmt.Errorf("creating networking client failed: %w", err)
	}
	access, err := access.NewNetworkingAccess(ctx, networking, opts.Log)
	if err != nil {
		return nil, fmt.Errorf("creating networking access failed: %w", err)
	}
	compute, err := opts.ClientFactory.Compute(osclient.WithRegion(opts.Infrastructure.Spec.Region))
	if err != nil {
		return nil, fmt.Errorf("creating compute client failed: %w", err)
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
		state:              whiteboard,
		infra:              opts.Infrastructure,
		config:             infraConfig,
		cloudProfileConfig: cloudProfileConfig,
		networkSpec:        networkSpec,
		isSNAShoot:         isSNAShoot,
		networking:         networking,
		access:             access,
		compute:            compute,
		log:                opts.Log,
		client:             opts.Client,
		stackitLB:          opts.StackitLB,
		iaasClient:         opts.IaaSClient,
		hasStackitMCM:      feature.UseStackitMachineControllerManager(opts.Cluster),
		technicalID:        opts.Cluster.Shoot.Status.TechnicalID,
	}
	return flowContext, nil
}

func (fctx *FlowContext) persistState(ctx context.Context) error {
	// status is nil such that there's no need to pass the nodesCIDR
	return infrainternal.PatchProviderStatusAndState(ctx, fctx.client, fctx.infra, nil, nil, fctx.computeInfrastructureState())
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

func (fctx *FlowContext) computeInfrastructureStatus() *stackitv1alpha1.InfrastructureStatus {
	status := &stackitv1alpha1.InfrastructureStatus{
		TypeMeta: infrainternal.StatusTypeMeta,
	}

	status.Networks.FloatingPool.ID = ptr.Deref(fctx.state.Get(IdentifierFloatingNetwork), "")
	status.Networks.FloatingPool.Name = ptr.Deref(fctx.state.Get(NameFloatingNetwork), "")

	status.Networks.ID = ptr.Deref(fctx.state.Get(IdentifierNetwork), "")
	status.Networks.Name = ptr.Deref(fctx.state.Get(NameNetwork), "")

	status.Networks.Router.ID = ptr.Deref(fctx.state.Get(IdentifierRouter), "")
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
