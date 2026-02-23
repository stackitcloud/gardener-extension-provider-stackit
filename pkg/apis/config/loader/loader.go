// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package loader

import (
	"fmt"
	"os"
	"regexp"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/config"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/config/install"
)

var (
	codec  runtime.Codec
	scheme *runtime.Scheme
	// labelKeyRegex validates that domain parts conform to DNS-like naming rules.
	// Allowed: alphanumeric, hyphens, underscores and dots.
	// Start and end must be alphanumeric.
	labelKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9]([-a-zA-Z0-9_.]*[a-zA-Z0-9])?$`)
)

func init() {
	scheme = runtime.NewScheme()
	install.Install(scheme)
	yamlSerializer := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme, scheme)
	codec = versioning.NewDefaultingCodecForScheme(
		scheme,
		yamlSerializer,
		yamlSerializer,
		schema.GroupVersion{Version: "v1alpha1"},
		runtime.InternalGroupVersioner,
	)
}

// LoadFromFile takes a filename and de-serializes the contents into ControllerConfiguration object.
func LoadFromFile(filename string) (*config.ControllerConfiguration, error) {
	bytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return Load(bytes)
}

// Load takes a byte slice and de-serializes the contents into ControllerConfiguration object.
// Encapsulates de-serialization without assuming the source is a file.
func Load(data []byte) (*config.ControllerConfiguration, error) {
	cfg := &config.ControllerConfiguration{}

	if len(data) == 0 {
		applyDefaults(cfg)
		return cfg, nil
	}

	decoded, _, err := codec.Decode(data, &schema.GroupVersionKind{Version: "v1alpha1", Kind: "Config"}, cfg)
	if err != nil {
		return nil, err
	}

	decodedCfg := decoded.(*config.ControllerConfiguration)
	applyDefaults(decodedCfg)

	if err := validate(decodedCfg); err != nil {
		return nil, err
	}

	return decodedCfg, nil
}

// applyDefaults applies default values to the ControllerConfiguration.
func applyDefaults(cfg *config.ControllerConfiguration) {
	if cfg.CustomLabelDomain == "" {
		// NOTE: Changing this default value MUST BE a breaking change.
		// It will lead to orphaned cloud resources without a migration plan.
		cfg.CustomLabelDomain = "kubernetes.io"
	}
}

// validate validates the configuration and all its fields.
func validate(cfg *config.ControllerConfiguration) error {
	// Validate customLabelDomain
	if !labelKeyRegex.MatchString(cfg.CustomLabelDomain) {
		return fmt.Errorf("invalid customLabelDomain %q: must start and end with alphanumeric characters and may contain hyphens, underscores and dots", cfg.CustomLabelDomain)
	}

	return nil
}
