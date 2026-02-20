// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = DescribeTable("decode sets typemeta",
	func(raw *runtime.RawExtension, expectErr bool) {
		cpc, err := CloudProfileConfigFromRawExtension(raw)
		if expectErr {
			Expect(err).To(HaveOccurred())
			return
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(cpc.TypeMeta.APIVersion).To(Equal("stackit.provider.extensions.gardener.cloud/v1alpha1"))
		Expect(cpc.TypeMeta.Kind).To(Equal("CloudProfileConfig"))
	},
	Entry("with data", &runtime.RawExtension{Raw: []byte(`{"dhcpDomain":"bar"}`)}, false),
	Entry("empty data", &runtime.RawExtension{Raw: []byte{}}, false),
	Entry("empty JSON", &runtime.RawExtension{Raw: []byte(`{}`)}, false),
	Entry("contains version", &runtime.RawExtension{Raw: []byte(`{"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1","kind":"CloudProfileConfig"}`)}, false),
	Entry("error with unknown version", &runtime.RawExtension{Raw: []byte(`{"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha2","kind":"CloudProfileConfig"}`)}, true),
)

var _ = Describe("decode sets typemeta", func() {
	It("should work", func() {
		cpc, err := ControlPlaneConfigFromCluster(nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(cpc.TypeMeta.APIVersion).To(Equal("stackit.provider.extensions.gardener.cloud/v1alpha1"))
		Expect(cpc.TypeMeta.Kind).To(Equal("ControlPlaneConfig"))
	})
})
