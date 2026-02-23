package controlplane

import (
	"fmt"
	"path"
	"strings"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/pelletier/go-toml/v2"
	"k8s.io/utils/ptr"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/config"
)

// ensureAdditionalFilesForRegCaches for the hosts config and optionally a custom CA.
func (e *ensurer) ensureAdditionalFilesForRegCaches(files *[]extensionsv1alpha1.File) error {
	for _, reg := range e.regCaches {
		if err := ensureHostsConfig(reg, files); err != nil {
			return err
		}

		if len(reg.CABundle) > 0 {
			ensureCAFile(reg, files)
		}
	}

	return nil
}

func ensureHostsConfig(reg config.RegistryCacheConfiguration, files *[]extensionsv1alpha1.File) error {
	data, err := hostsTOML(reg)
	if err != nil {
		return fmt.Errorf("failed to generate containerd hosts.toml: %w", err)
	}
	hostsFile := extensionsv1alpha1.File{
		Path:        path.Join(configBaseDir(reg.Server), "hosts.toml"),
		Permissions: ptr.To[uint32](0o644),
		Content: extensionsv1alpha1.FileContent{
			Inline: &extensionsv1alpha1.FileContentInline{
				Encoding: "",
				Data:     data,
			},
		},
	}
	*files = extensionswebhook.EnsureFileWithPath(*files, hostsFile)
	return nil
}

func ensureCAFile(reg config.RegistryCacheConfiguration, files *[]extensionsv1alpha1.File) {
	caFile := extensionsv1alpha1.File{
		Path:        caPath(reg),
		Permissions: ptr.To[uint32](0o644),
		Content: extensionsv1alpha1.FileContent{
			Inline: &extensionsv1alpha1.FileContentInline{
				Encoding: "b64",
				Data:     utils.EncodeBase64(reg.CABundle),
			},
		},
	}
	*files = extensionswebhook.EnsureFileWithPath(*files, caFile)
}

type containerdConfig struct {
	Server string                    `toml:"server" comment:"Created by gardener-extension-provider-stackit"`
	Host   map[string]containerdHost `toml:"host"`
}

type containerdHost struct {
	Capabilities []string `toml:"capabilities"`
	CA           string   `toml:"ca,omitempty"`
}

func hostsTOML(reg config.RegistryCacheConfiguration) (string, error) {
	host := containerdHost{
		Capabilities: reg.Capabilities,
	}
	if len(reg.CABundle) > 0 {
		host.CA = caPath(reg)
	}

	config := containerdConfig{
		Server: reg.Server,
		Host:   map[string]containerdHost{reg.Cache: host},
	}
	out, err := toml.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func hostname(h string) string {
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimSuffix(h, "/")
	return h
}

func configBaseDir(server string) string {
	const baseDir = "/etc/containerd/certs.d"
	return path.Join(baseDir, hostname(server))
}

func caPath(reg config.RegistryCacheConfiguration) string {
	return path.Join(configBaseDir(reg.Server), hostname(reg.Cache)+".crt")
}
