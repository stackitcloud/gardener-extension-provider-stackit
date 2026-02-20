package client

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

const (
	UserAgent = "gardener-extension-provider-stackit"
)

// Factory produces clients for various STACKIT services.
type Factory interface {
	// DNS returns a STACKIT DNS service client.
	DNS(context.Context, client.Client, corev1.SecretReference) (DNSClient, error)

	// LoadBalancing returns a STACKIT load balancing service client.
	LoadBalancing(context.Context, client.Client, corev1.SecretReference) (LoadBalancingClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(context.Context, client.Client, corev1.SecretReference) (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitAPIEndpoints stackitv1alpha1.APIEndpoints
}

func New(region string, cluster *extensionscontroller.Cluster) Factory {
	var apiEndpoints stackitv1alpha1.APIEndpoints

	if cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster); err == nil {
		apiEndpoints = ptr.Deref(cloudProfileConfig.APIEndpoints, stackitv1alpha1.APIEndpoints{})
	}

	return &factory{
		StackitRegion:       region,
		StackitAPIEndpoints: apiEndpoints,
	}
}

func (f factory) LoadBalancing(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (LoadBalancingClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials)
}

func (f factory) IaaS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (IaaSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints, credentials)
}

func (f factory) DNS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (DNSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewDNSClient(ctx, f.StackitAPIEndpoints, credentials)
}

func clientOptions(region *string, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) []sdkconfig.ConfigurationOption {
	result := []sdkconfig.ConfigurationOption{
		sdkconfig.WithUserAgent(UserAgent),
		sdkconfig.WithServiceAccountKey(credentials.SaKeyJSON),
	}
	if region != nil {
		result = append(result, sdkconfig.WithRegion(*region))
	}

	if endpoints.TokenEndpoint != nil {
		result = append(result, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	return result
}
