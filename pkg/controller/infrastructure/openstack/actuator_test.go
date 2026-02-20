package openstack

import (
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = DescribeTable("infrastructureStateFromRaw",
	func(raw string) {
		infra := &extensionsv1alpha1.Infrastructure{
			Status: extensionsv1alpha1.InfrastructureStatus{
				DefaultStatus: extensionsv1alpha1.DefaultStatus{
					State: &runtime.RawExtension{
						Raw: []byte(raw),
					},
				},
			},
		}
		result, err := infrastructureStateFromRaw(infra)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.Data).To(Equal(map[string]string{
			"FloatingNetworkName": "foobar",
		},
		))
	},
	Entry("convert openstackv1alpha1 to openstack", `{
"apiVersion": "openstack.provider.extensions.gardener.cloud/v1alpha1",
"kind": "InfrastructureState",
"data": {"FloatingNetworkName": "foobar"}
}`),
	Entry("convert stackitv1alpha1 to openstack", `{
"apiVersion": "stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind": "InfrastructureState",
"data": {"FloatingNetworkName": "foobar"}
}`),
)
