package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	alb "github.com/stackitcloud/stackit-sdk-go/services/alb/v2api"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

type ApplicationLoadBalancingClient interface {
	ProjectID() string

	ListLoadBalancers(ctx context.Context) ([]alb.LoadBalancer, error)
	DeleteLoadBalancer(ctx context.Context, name string) error
}

type applicationLoadBalancingClient struct {
	Client    alb.DefaultAPI
	projectID string
	region    string
}

func NewApplicationLoadBalancingClient(_ context.Context, region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials, caBundle string) (ApplicationLoadBalancingClient, error) {
	options, err := clientOptions(endpoints, credentials, caBundle)
	if err != nil {
		return nil, err
	}
	if endpoints.ApplicationLoadBalancer != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.ApplicationLoadBalancer))
	}

	apiClient, err := alb.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &applicationLoadBalancingClient{
		Client:    apiClient.DefaultAPI,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (l applicationLoadBalancingClient) ProjectID() string {
	return l.projectID
}

func (l applicationLoadBalancingClient) ListLoadBalancers(ctx context.Context) ([]alb.LoadBalancer, error) {
	lbResponse, err := l.Client.ListLoadBalancers(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}
	return lbResponse.GetLoadBalancers(), nil
}

func (l applicationLoadBalancingClient) DeleteLoadBalancer(ctx context.Context, name string) error {
	_, err := l.Client.DeleteLoadBalancer(ctx, l.projectID, l.region, name).Execute()
	return err
}
