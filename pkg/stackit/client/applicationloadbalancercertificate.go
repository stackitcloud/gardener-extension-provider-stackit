package client

import (
	"context"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	albcert "github.com/stackitcloud/stackit-sdk-go/services/certificates/v2api"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

type ApplicationLoadBalancerCertificateClient interface {
	ProjectID() string

	ListApplicationLoadBalancerCertificates(ctx context.Context) ([]albcert.GetCertificateResponse, error)
	DeleteApplicationLoadBalancerCertificates(ctx context.Context, id string) error
}

type applicationLoadBalancingCertificateClient struct {
	Client    albcert.DefaultAPI
	projectID string
	region    string
}

func NewApplicationLoadBalancerCertificateClient(_ context.Context, region string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials, caBundle string) (ApplicationLoadBalancerCertificateClient, error) {
	options, err := clientOptions(endpoints, credentials, caBundle)
	if err != nil {
		return nil, err
	}
	if endpoints.ApplicationLoadBalancerCertificate != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.ApplicationLoadBalancerCertificate))
	}

	apiClient, err := albcert.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &applicationLoadBalancingCertificateClient{
		Client:    apiClient.DefaultAPI,
		projectID: credentials.ProjectID,
		region:    region,
	}, nil
}

func (l applicationLoadBalancingCertificateClient) ProjectID() string {
	return l.projectID
}

func (l applicationLoadBalancingCertificateClient) ListApplicationLoadBalancerCertificates(ctx context.Context) ([]albcert.GetCertificateResponse, error) {
	certResponse, err := l.Client.ListCertificates(ctx, l.projectID, l.region).Execute()
	if err != nil {
		return nil, err
	}
	return certResponse.GetItems(), nil
}

func (l applicationLoadBalancingCertificateClient) DeleteApplicationLoadBalancerCertificates(ctx context.Context, id string) error {
	_, err := l.Client.DeleteCertificate(ctx, l.projectID, l.region, id).Execute()
	return err
}
