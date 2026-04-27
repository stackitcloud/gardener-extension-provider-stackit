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

	CreateLoadBalancer(ctx context.Context, payload loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	GetLoadBalancer(ctx context.Context, name string) (*loadbalancer.LoadBalancer, error)
	ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error)
	UpdateLoadBalancer(ctx context.Context, name string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error)
	UpdateLoadBalancerTargetPool(ctx context.Context, name, targetPool string, payload loadbalancer.UpdateTargetPoolPayload) (*loadbalancer.TargetPool, error)
	DeleteLoadBalancer(ctx context.Context, name string) error
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

func (l loadBalancingClient) CreateLoadBalancer(ctx context.Context, payload loadbalancer.CreateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return l.Client.CreateLoadBalancer(ctx, l.projectID, l.region).CreateLoadBalancerPayload(payload).Execute()
}

func (l loadBalancingClient) GetLoadBalancer(ctx context.Context, name string) (*loadbalancer.LoadBalancer, error) {
	return l.Client.GetLoadBalancer(ctx, l.projectID, l.region, name).Execute()
}

func (l loadBalancingClient) ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error) {
	lbResponse, err := l.Client.ListLoadBalancers(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}
	return lbResponse.GetLoadBalancers(), nil
}

func (l loadBalancingClient) UpdateLoadBalancer(ctx context.Context, name string, payload loadbalancer.UpdateLoadBalancerPayload) (*loadbalancer.LoadBalancer, error) {
	return l.Client.UpdateLoadBalancer(ctx, l.projectID, l.region, name).UpdateLoadBalancerPayload(payload).Execute()
}

func (l loadBalancingClient) UpdateLoadBalancerTargetPool(ctx context.Context, name, targetPool string, payload loadbalancer.UpdateTargetPoolPayload) (*loadbalancer.TargetPool, error) {
	return l.Client.UpdateTargetPool(ctx, l.projectID, l.region, name, targetPool).UpdateTargetPoolPayload(payload).Execute()
}

func (l loadBalancingClient) DeleteLoadBalancer(ctx context.Context, name string) error {
	_, err := l.Client.DeleteLoadBalancer(ctx, l.projectID, l.region, name).Execute()
	return err
}
