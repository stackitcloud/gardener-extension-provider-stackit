// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"

	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	"github.com/spf13/pflag"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/config"
	configloader "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/config/loader"
)

var ErrConfigFilePathNotSet = errors.New("config file path not set")

// ConfigOptions are command line options that can be set for config.ControllerConfiguration.
type ConfigOptions struct {
	ConfigFilePath string

	config *Config
}

// Config is a completed controller configuration.
type Config struct {
	// Config is the controller configuration.
	Config *config.ControllerConfiguration
}

func (c *ConfigOptions) buildConfig() (*config.ControllerConfiguration, error) {
	if c.ConfigFilePath == "" {
		return nil, ErrConfigFilePathNotSet
	}
	return configloader.LoadFromFile(c.ConfigFilePath)
}

// Complete implements RESTCompleter.Complete.
func (c *ConfigOptions) Complete() error {
	controllerConfig, err := c.buildConfig()
	if err != nil {
		return err
	}

	c.config = &Config{controllerConfig}
	return nil
}

// Completed returns the completed Config. Only call this if `Complete` was successful.
func (c *ConfigOptions) Completed() *Config {
	return c.config
}

// AddFlags implements Flagger.AddFlags.
func (c *ConfigOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.ConfigFilePath, "config-file", "", "path to the controller manager configuration file")
}

// Apply sets the values of this Config in the given config.ControllerConfiguration.
func (c *Config) Apply(cfg *config.ControllerConfiguration) {
	*cfg = *c.Config
}

// ApplyETCDStorage sets the given etcd storage configuration to that of this Config.
func (c *Config) ApplyETCDStorage(etcdStorage *config.ETCDStorage) {
	*etcdStorage = c.Config.ETCD.Storage
}

// ApplyRegistryCaches sets the given Registry Cache configurations.
func (c *Config) ApplyRegistryCaches(regCaches *[]config.RegistryCacheConfiguration) {
	if len(c.Config.RegistryCaches) == 0 {
		return
	}
	*regCaches = c.Config.RegistryCaches
}

func (c *Config) ApplyDeployALBIngressController(deployALBIngressController *bool) {
	*deployALBIngressController = c.Config.DeployALBIngressController
}

// ApplyCustomLabelDomain sets the custom label domain configuration for infrastructure resources.
func (c *Config) ApplyCustomLabelDomain(customLabelDomain *string) {
	*customLabelDomain = c.Config.CustomLabelDomain
}

// Options initializes empty config.ControllerConfiguration, applies the set values and returns it.
func (c *Config) Options() config.ControllerConfiguration {
	var cfg config.ControllerConfiguration
	c.Apply(&cfg)
	return cfg
}

// ApplyHealthCheckConfig applies the HealthCheckConfig to the config
func (c *Config) ApplyHealthCheckConfig(cfg *healthcheckconfig.HealthCheckConfig) {
	if c.Config.HealthCheckConfig != nil {
		*cfg = *c.Config.HealthCheckConfig
	}
}
