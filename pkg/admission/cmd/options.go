package cmd

import (
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"github.com/spf13/pflag"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/admission/mutator"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/admission/validator"
)

// GardenWebhookSwitchOptions are the webhookcmd.SwitchOptions for the admission webhooks.
func GardenWebhookSwitchOptions() *webhookcmd.SwitchOptions {
	return webhookcmd.NewSwitchOptions(
		webhookcmd.Switch(validator.Name, validator.New),
		webhookcmd.Switch(mutator.Name, mutator.New),
	)
}

// ConfigOptions are command line options that can be set for admission webhooks.
type ConfigOptions struct {
	// AllowApplicationLoadBalancerController configures if the application load balancer controller can be used in the ControlPlaneConfig.
	AllowApplicationLoadBalancerController bool

	config *Config
}

// Config is a completed admission configuration.
type Config struct {
	// AllowApplicationLoadBalancerController configures if the application load balancer controller can be used in the ControlPlaneConfig.
	AllowApplicationLoadBalancerController bool
}

// AddFlags implements Flagger.AddFlags.
func (c *ConfigOptions) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(
		&c.AllowApplicationLoadBalancerController,
		"allow-application-load-balancer-controller",
		false,
		"Configures if the application load balancer controller can be used in the ControlPlaneConfig.",
	)
}

// Complete implements RESTCompleter.Complete.
func (c *ConfigOptions) Complete() error {
	c.config = &Config{
		AllowApplicationLoadBalancerController: c.AllowApplicationLoadBalancerController,
	}
	return nil
}

// Completed returns the completed Config. Only call this if `Complete` was successful.
func (c *ConfigOptions) Completed() *Config {
	return c.config
}

// ApplyAllowApplicationLoadBalancerController sets the values of this Config in the given config.AllowApplicationLoadBalancerController.
func (c *Config) ApplyAllowApplicationLoadBalancerController(cfg *bool) {
	*cfg = c.AllowApplicationLoadBalancerController
}
