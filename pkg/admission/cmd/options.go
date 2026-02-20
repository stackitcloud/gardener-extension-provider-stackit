package cmd

import (
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/admission/mutator"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/admission/validator"
)

// GardenWebhookSwitchOptions are the webhookcmd.SwitchOptions for the admission webhooks.
func GardenWebhookSwitchOptions() *webhookcmd.SwitchOptions {
	return webhookcmd.NewSwitchOptions(
		webhookcmd.Switch(validator.Name, validator.New),
		webhookcmd.Switch(mutator.Name, mutator.New),
	)
}
