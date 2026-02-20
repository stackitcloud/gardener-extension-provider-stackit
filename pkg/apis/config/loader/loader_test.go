// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package loader_test

import (
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/config/loader"
)

func TestLoader(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Loader Suite")
}

var _ = Describe("Loader", func() {
	Describe("#Load", func() {
		buildConfigYAML := func(customLabelDomain string) []byte {
			return []byte(fmt.Sprintf(`apiVersion: stackit.provider.extensions.config.stackit.cloud/v1alpha1
kind: ControllerConfiguration
customLabelDomain: %s
`, customLabelDomain))
		}

		It("should apply defaults when data is empty", func() {
			cfg, err := loader.Load([]byte{})
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.CustomLabelDomain).To(Equal("kubernetes.io"))
		})

		DescribeTable("should accept valid customLabelDomain values",
			func(domain string) {
				cfg, err := loader.Load(buildConfigYAML(domain))
				Expect(err).NotTo(HaveOccurred())
				Expect(cfg.CustomLabelDomain).To(Equal(domain))
			},
			Entry("default kubernetes.io", "kubernetes.io"),
			Entry("custom ske.stackit.cloud", "ske.stackit.cloud"),
			Entry("custom example.com", "example.com"),
			Entry("single character", "a"),
			Entry("mixed case", "MyDomain.Com"),
			Entry("with hyphens", "example-domain.io"),
			Entry("with underscores", "example_domain.com"),
			Entry("alphanumeric", "a1b2c3"),
		)

		DescribeTable("should reject invalid customLabelDomain values",
			func(domain string) {
				_, err := loader.Load(buildConfigYAML(domain))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid customLabelDomain"))
			},
			Entry("starts with hyphen", "-invalid.com"),
			Entry("ends with hyphen", "invalid-"),
			Entry("starts with dot", ".invalid.com"),
			Entry("ends with dot", "invalid.com."),
			Entry("contains space", "invalid domain.com"),
			Entry("contains at sign", "invalid@domain.com"),
			Entry("contains slash", "example.com/part"),
			Entry("only special characters", "---"),
		)
	})

	Describe("#LoadFromFile", func() {
		It("should fail when file does not exist", func() {
			_, err := loader.LoadFromFile("/nonexistent/path/to/config.yaml")
			Expect(err).To(HaveOccurred())
		})
	})

})
