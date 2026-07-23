package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
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

	ApplicationLoadBalancer(context.Context, client.Client, corev1.SecretReference) (ApplicationLoadBalancingClient, error)

	ApplicationLoadBalancerCertificate(context.Context, client.Client, corev1.SecretReference) (ApplicationLoadBalancerCertificateClient, error)

	// IaaS returns a STACKIT IaaS service client.
	IaaS(context.Context, client.Client, corev1.SecretReference) (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitAPIEndpoints stackitv1alpha1.APIEndpoints
	CABundleB64         string
}

func New(region string, cluster *extensionscontroller.Cluster) Factory {
	var apiEndpoints stackitv1alpha1.APIEndpoints
	var caBundle string

	if cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster); err == nil {
		apiEndpoints = ptr.Deref(cloudProfileConfig.APIEndpoints, stackitv1alpha1.APIEndpoints{})
	}

	if cluster.CloudProfile != nil && cluster.CloudProfile.Spec.CABundle != nil {
		caBundle = ptr.Deref(cluster.CloudProfile.Spec.CABundle, "")
	}

	return &factory{
		StackitRegion:       region,
		StackitAPIEndpoints: apiEndpoints,
		CABundleB64:         caBundle,
	}
}

func (f factory) LoadBalancing(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (LoadBalancingClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundleB64)
}

func (f factory) ApplicationLoadBalancer(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (ApplicationLoadBalancingClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewApplicationLoadBalancingClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundleB64)
}

func (f factory) ApplicationLoadBalancerCertificate(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (ApplicationLoadBalancerCertificateClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewApplicationLoadBalancerCertificateClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundleB64)
}

func (f factory) IaaS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (IaaSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundleB64)
}

func (f factory) DNS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (DNSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewDNSClient(ctx, f.StackitAPIEndpoints, credentials, f.CABundleB64)
}

// newHTTPClientWithCustomCA creates an http.Client with a custom CA
func newHTTPClientWithCustomCA(caBundle []byte) (*http.Client, error) {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		// we could also fall back here and use an empty pool via x509.NewCertPool()
		return nil, fmt.Errorf("failed to load system cert pool: %w", err)
	}
	if ok := caCertPool.AppendCertsFromPEM(caBundle); !ok {
		return nil, fmt.Errorf("failed to append CA bundle to cert pool")
	}

	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: caCertPool,
		},
	}}, nil
}

func clientOptions(endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials, caBundle string) ([]sdkconfig.ConfigurationOption, error) {
	result := []sdkconfig.ConfigurationOption{
		sdkconfig.WithUserAgent(UserAgent),
		sdkconfig.WithServiceAccountKey(credentials.SaKeyJSON),
	}

	if endpoints.TokenEndpoint != nil {
		result = append(result, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	if caBundle != "" {
		customHttpClient, err := newHTTPClientWithCustomCA([]byte(caBundle))
		if err != nil {
			return nil, err
		}
		result = append(result, sdkconfig.WithHTTPClient(customHttpClient))
	}

	return result, nil
}
