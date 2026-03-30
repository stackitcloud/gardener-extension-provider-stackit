package stackit

import (
	"github.com/stackitcloud/stackit-sdk-go/services/iaas"
)

// The SDK is lacking constants for well-known values of the security group rule fields.
const (
	// Type is the type of resources managed by the STACKIT actuators.
	Type = "stackit"

	// Name is the name of the STACKIT provider.
	Name = "provider-stackit"

	EtherTypeIPv4    = "IPv4"
	EtherTypeIPv6    = "IPv6"
	DirectionEgress  = "egress"
	DirectionIngress = "ingress"

	// STACKITPodIdentityWebhookName is a constant for the name of the Pod Identity Webhook. (stackit)
	STACKITPodIdentityWebhookName = "pod-identity-webhook"
)

var (
	// ProtocolTCP is a shortcut for specifying a security group rule's protocol.
	ProtocolTCP = iaas.Protocol{Name: new("tcp")}
	// ProtocolUDP is a shortcut for specifying a security group rule's protocol.
	ProtocolUDP = iaas.Protocol{Name: new("udp")}
)
