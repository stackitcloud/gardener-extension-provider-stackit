package registrycache

import (
	"encoding/base64"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/config"
)

var _ = Describe("registry cache files", func() {
	var (
		regCaches []config.RegistryCacheConfiguration
		files     []extensionsv1alpha1.File
	)

	BeforeEach(func() {
		regCaches = []config.RegistryCacheConfiguration{{
			Server:       "https://foo.com",
			Cache:        "https://foo-cache.com",
			Capabilities: []string{"pull"},
		}}
		files = []extensionsv1alpha1.File{}
	})

	It("should not add anything with an empty list (AdditionalProvisionFiles)", func() {
		e := &Ensurer{}

		Expect(e.EnsureCaches(&files)).To(Succeed())
		Expect(files).NotTo(ContainElement(
			HaveField("Path", ContainSubstring("hosts.toml")),
		))
	})

	It("should inject the config without CA (AdditionalProvisionFiles)", func() {
		e := &Ensurer{
			Caches: regCaches,
		}

		Expect(e.EnsureCaches(&files)).To(Succeed())

		expetedData := `# Created by gardener-extension-provider-stackit
server = 'https://foo.com'

[host]
[host.'https://foo-cache.com']
capabilities = ['pull']
`
		Expect(files).To(ContainElement(And(
			HaveField("Path", "/etc/containerd/certs.d/foo.com/hosts.toml"),
			HaveField("Content.Inline", And(
				HaveField("Encoding", ""),
				HaveField("Data", Equal(expetedData)),
			)),
		)))
	})

	It("should work with CA (AdditionalFiles)", func() {
		dummyCA := []byte("--- Certificate_---\nfoo\nbar")
		regCaches[0].CABundle = dummyCA
		encoded := base64.StdEncoding.EncodeToString(dummyCA)
		e := &Ensurer{
			Caches: regCaches,
		}

		Expect(e.EnsureCaches(&files)).To(Succeed())

		Expect(files).To(ContainElements(
			And(
				HaveField("Path", "/etc/containerd/certs.d/foo.com/foo-cache.com.crt"),
				HaveField("Content.Inline", And(
					HaveField("Encoding", "b64"),
					HaveField("Data", Equal(encoded)),
				)),
			),
			And(
				HaveField("Path", "/etc/containerd/certs.d/foo.com/hosts.toml"),
				HaveField("Content.Inline.Data", ContainSubstring("ca = '/etc/containerd/certs.d/foo.com/foo-cache.com.crt'")),
			),
		))
	})
})
