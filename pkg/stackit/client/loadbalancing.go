package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

type LoadBalancingClient interface {
	ProjectID() string

	ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, lbName string) error
	GetLoadBalancer(ctx context.Context, id string) (*loadbalancer.LoadBalancer, error)
	CreateLoadBalancer(ctx context.Context, payload loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, lbName string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancerTargetPool(ctx context.Context, lbName, tpName string, payload loadbalancer.UpdateTargetPoolPayload) (*loadbalancer.TargetPool, error)
}

type loadBalancingClient struct {
	Client    loadbalancer.DefaultAPI
	projectID string
	region    string
}

func NewLoadBalancingClient(_ context.Context, region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) (LoadBalancingClient, error) {
	options := clientOptions(endpoints, credentials)

	if endpoints.LoadBalancer != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.LoadBalancer))
	}

	apiClient, err := loadbalancer.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &loadBalancingClient{
		Client:    apiClient.DefaultAPI,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (l loadBalancingClient) ProjectID() string {
	return l.projectID
}

func (l loadBalancingClient) ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error) {
	lbResponse, err := l.Client.ListLoadBalancers(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}
	return lbResponse.GetLoadBalancers(), nil
}

func (l loadBalancingClient) DeleteLoadBalancer(ctx context.Context, lbName string) error {
	_, err := l.Client.DeleteLoadBalancer(ctx, l.projectID, l.region, lbName).Execute()
	return err
}

func (l loadBalancingClient) GetLoadBalancer(ctx context.Context, lbName string) (*loadbalancer.LoadBalancer, error) {
	lb, err := l.Client.GetLoadBalancer(ctx, l.projectID, l.region, lbName).Execute()
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return lb, nil
}

func (l loadBalancingClient) CreateLoadBalancer(ctx context.Context, payload loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return l.Client.CreateLoadBalancer(ctx, l.projectID, l.region).CreateLoadBalancerPayload(payload).Execute()
}

func (l loadBalancingClient) UpdateLoadBalancer(ctx context.Context, lbName string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return l.Client.UpdateLoadBalancer(ctx, l.projectID, l.region, lbName).UpdateLoadBalancerPayload(payload).Execute()
}

func (l loadBalancingClient) UpdateLoadBalancerTargetPool(ctx context.Context, lbName, tpName string, payload loadbalancer.UpdateTargetPoolPayload) (*loadbalancer.TargetPool, error) {
	return l.Client.UpdateTargetPool(ctx, l.projectID, l.region, lbName, tpName).UpdateTargetPoolPayload(payload).Execute()
}
