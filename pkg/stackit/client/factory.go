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

	// IaaS returns a STACKIT IaaS service client.
	IaaS(context.Context, client.Client, corev1.SecretReference) (IaaSClient, error)
}

type factory struct {
	StackitRegion       string
	StackitAPIEndpoints stackitv1alpha1.APIEndpoints
	CABundle            string
}

func New(region string, cluster *extensionscontroller.Cluster) Factory {
	var apiEndpoints stackitv1alpha1.APIEndpoints
	var caBundle string

	if cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster); err == nil {
		apiEndpoints = ptr.Deref(cloudProfileConfig.APIEndpoints, stackitv1alpha1.APIEndpoints{})
		if cloudProfileConfig.CABundle != nil {
			caBundle = ptr.Deref(cloudProfileConfig.CABundle, "")
		}
	}

	return &factory{
		StackitRegion:       region,
		StackitAPIEndpoints: apiEndpoints,
		CABundle:            caBundle,
	}
}

func (f factory) LoadBalancing(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (LoadBalancingClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewLoadBalancingClient(ctx, f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundle)
}

func (f factory) IaaS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (IaaSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewIaaSClient(f.StackitRegion, f.StackitAPIEndpoints, credentials, f.CABundle)
}

func (f factory) DNS(ctx context.Context, c client.Client, secretRef corev1.SecretReference) (DNSClient, error) {
	credentials, err := stackit.GetCredentialsFromSecretRef(ctx, c, secretRef)
	if err != nil {
		return nil, err
	}

	return NewDNSClient(ctx, f.StackitAPIEndpoints, credentials, f.CABundle)
}

// InjectCAIntoHTTPClient injects a CABundle into an existing http.Client
func InjectCAIntoHTTPClient(client *http.Client, caBundle string) error {
	caCertPool, err := x509.SystemCertPool()
	if err != nil {
		// we could also fall back here and use an empty pool via  x509.NewCertPool()
		return err
	}
	if ok := caCertPool.AppendCertsFromPEM([]byte(caBundle)); !ok {
		return fmt.Errorf("failed to append CA bundle to cert pool")
	}
	var transport *http.Transport
	if client.Transport != nil {
		var ok bool
		transport, ok = client.Transport.(*http.Transport)
		if !ok {
			return fmt.Errorf("client.Transport is not an *http.Transport")
		}
		// Clone it to avoid race conditions if the transport is shared
		transport = transport.Clone()
	} else {
		// Explicitly clone the default transport
		// The client should already have transport. Should never happen
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}

	// Inject the custom TLS configuration
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}

	transport.TLSClientConfig.RootCAs = caCertPool

	// Re-assign the modified transport back to the client
	client.Transport = transport
	return nil
}
func clientOptions(endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) []sdkconfig.ConfigurationOption {
	result := []sdkconfig.ConfigurationOption{
		sdkconfig.WithUserAgent(UserAgent),
		sdkconfig.WithServiceAccountKey(credentials.SaKeyJSON),
	}

	if endpoints.TokenEndpoint != nil {
		result = append(result, sdkconfig.WithTokenEndpoint(*endpoints.TokenEndpoint))
	}

	return result
}
