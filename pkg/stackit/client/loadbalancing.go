package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/loadbalancer"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

type LoadBalancingClient interface {
	ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, lbName string) error
	GetLoadBalancer(ctx context.Context, id string) (*loadbalancer.LoadBalancer, error)
}

type loadBalancingClient struct {
	Client    loadbalancer.DefaultApi
	projectID string
	region    string
}

func NewLoadBalancingClient(ctx context.Context, region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) (LoadBalancingClient, error) {
	options := clientOptions(&region, endpoints, credentials)

	if endpoints.LoadBalancer != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.LoadBalancer))
	}

	apiClient, err := loadbalancer.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &loadBalancingClient{
		Client:    apiClient,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (l loadBalancingClient) ListLoadBalancers(ctx context.Context) ([]loadbalancer.LoadBalancer, error) {
	lbResponse, err := l.Client.ListLoadBalancersExecute(ctx, l.projectID, l.region)
	if err != nil {
		return nil, err
	}
	return lbResponse.GetLoadBalancers(), nil
}

func (l loadBalancingClient) DeleteLoadBalancer(ctx context.Context, lbName string) error {
	_, err := l.Client.DeleteLoadBalancerExecute(ctx, l.projectID, l.region, lbName)
	return err
}

func (l loadBalancingClient) GetLoadBalancer(ctx context.Context, lbName string) (*loadbalancer.LoadBalancer, error) {
	return l.Client.GetLoadBalancer(ctx, l.projectID, l.region, lbName).Execute()
}
