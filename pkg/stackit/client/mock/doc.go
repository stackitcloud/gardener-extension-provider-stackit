//go:generate mockgen -package client -destination=mocks.go github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client Factory,IaaSClient,LoadBalancingClient,DNSClient

package client
