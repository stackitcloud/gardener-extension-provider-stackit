package controlplane

import (
	"context"
	"encoding/base64"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gcontext "github.com/gardener/gardener/extensions/pkg/webhook/context"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/config"
)

var _ = Describe("registry cache files", func() {
	var (
		ctx  context.Context
		gctx gcontext.GardenContext
		log  logr.Logger

		regCaches []config.RegistryCacheConfiguration
		files     []extensionsv1alpha1.File
	)

	BeforeEach(func() {
		ctx = context.Background()
		gctx = &fakeGardenContext{}
		log = logr.Discard()

		regCaches = []config.RegistryCacheConfiguration{{
			Server:       "https://foo.com",
			Cache:        "https://foo-cache.com",
			Capabilities: []string{"pull"},
		}}
		files = []extensionsv1alpha1.File{}
	})

	It("should not add anything with an empty list (AdditionalProvisionFiles)", func() {
		e := NewEnsurer(nil, log)

		Expect(e.EnsureAdditionalProvisionFiles(ctx, gctx, &files, nil)).To(Succeed())
		Expect(files).NotTo(ContainElement(
			HaveField("Path", ContainSubstring("hosts.toml")),
		))
	})

	It("should inject the config without CA (AdditionalProvisionFiles)", func() {
		e := NewEnsurer(regCaches, log)

		Expect(e.EnsureAdditionalProvisionFiles(ctx, gctx, &files, nil)).To(Succeed())

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
		e := NewEnsurer(regCaches, log)

		Expect(e.EnsureAdditionalFiles(ctx, gctx, &files, nil)).To(Succeed())

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

type fakeGardenContext struct {
}

func (f *fakeGardenContext) GetCluster(ctx context.Context) (*extensionscontroller.Cluster, error) {
	return &extensionscontroller.Cluster{}, nil
}
