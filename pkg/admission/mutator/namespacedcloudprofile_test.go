// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package mutator_test

import (
	"context"

	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/admission/mutator"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitinstall "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/install"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
)

var _ = Describe("NamespacedCloudProfile Mutator", func() {
	var (
		fakeClient  client.Client
		fakeManager manager.Manager
		namespace   string
		ctx         = context.Background()
		scheme      *runtime.Scheme

		namespacedCloudProfileMutator extensionswebhook.Mutator
		namespacedCloudProfile        *v1beta1.NamespacedCloudProfile
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		utilruntime.Must(stackitinstall.AddToScheme(scheme))
		utilruntime.Must(v1beta1.AddToScheme(scheme))
		fakeClient = fakeclient.NewClientBuilder().WithScheme(scheme).Build()
		fakeManager = &test.FakeManager{
			Client: fakeClient,
			Scheme: scheme,
		}
		namespace = "garden-dev"

		namespacedCloudProfileMutator = mutator.NewNamespacedCloudProfileMutator(fakeManager)
		namespacedCloudProfile = &v1beta1.NamespacedCloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "profile-1",
				Namespace: namespace,
			},
		}
	})

	Describe("#Mutate", func() {
		It("should succeed for NamespacedCloudProfile without provider config", func() {
			Expect(namespacedCloudProfileMutator.Mutate(ctx, namespacedCloudProfile, nil)).To(Succeed())
		})

		It("should skip if NamespacedCloudProfile is in deletion phase", func() {
			namespacedCloudProfile.DeletionTimestamp = ptr.To(metav1.Now())
			expectedProfile := namespacedCloudProfile.DeepCopy()

			Expect(namespacedCloudProfileMutator.Mutate(ctx, namespacedCloudProfile, nil)).To(Succeed())

			Expect(namespacedCloudProfile).To(DeepEqual(expectedProfile))
		})

		Describe("merge the provider configurations from a NamespacedCloudProfile and the parent CloudProfile", func() {
			It("should correctly merge apiEndpoints from spec", func() {
				namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"machineImages":[
  {"name":"image-1","versions":[{"version":"1.0","image":"image-name-1"}]}
],
"apiEndpoints":{"iaas":"https://iaas.api.stackit.cloud"}}`)}
				namespacedCloudProfile.Spec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"apiEndpoints":{"iaas":"https://custom-iaas.example.com","loadBalancer":"https://custom-lb.example.com"}}`)}

				Expect(namespacedCloudProfileMutator.Mutate(ctx, namespacedCloudProfile, nil)).To(Succeed())

				mergedConfig, err := helper.CloudProfileConfigFromRawExtension(namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig)
				Expect(err).ToNot(HaveOccurred())
				Expect(mergedConfig.APIEndpoints).ToNot(BeNil())
				Expect(mergedConfig.APIEndpoints.IaaS).To(PointTo(Equal("https://custom-iaas.example.com")))
				Expect(mergedConfig.APIEndpoints.LoadBalancer).To(PointTo(Equal("https://custom-lb.example.com")))
			})

			It("should correctly merge caBundle from spec", func() {
				namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"machineImages":[
  {"name":"image-1","versions":[{"version":"1.0","image":"image-name-1"}]}
]}`)}
				namespacedCloudProfile.Spec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"caBundle":"-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"}`)}

				Expect(namespacedCloudProfileMutator.Mutate(ctx, namespacedCloudProfile, nil)).To(Succeed())

				mergedConfig, err := helper.CloudProfileConfigFromRawExtension(namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig)
				Expect(err).ToNot(HaveOccurred())
				Expect(mergedConfig.CABundle).To(PointTo(Equal("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----")))
			})

			It("should correctly merge extended machineImages", func() {
				namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"machineImages":[
  {"name":"image-1","versions":[{"version":"1.0","image":"image-name-1","regions":[{"name":"image-region-1","id":"id-img-reg-1"}]}]}
]}`)}
				namespacedCloudProfile.Spec.ProviderConfig = &runtime.RawExtension{Raw: []byte(`{
"apiVersion":"stackit.provider.extensions.gardener.cloud/v1alpha1",
"kind":"CloudProfileConfig",
"machineImages":[
  {"name":"image-1","versions":[{"version":"1.1","image":"image-name-2","regions":[{"name":"image-region-2","id":"id-img-reg-2","architecture":"armhf"}]}]},
  {"name":"image-2","versions":[{"version":"2.0","image":"image-name-3","regions":[{"name":"image-region-3","id":"id-img-reg-3"}]}]}
]}`)}

				Expect(namespacedCloudProfileMutator.Mutate(ctx, namespacedCloudProfile, nil)).To(Succeed())

				mergedConfig, err := helper.CloudProfileConfigFromRawExtension(namespacedCloudProfile.Status.CloudProfileSpec.ProviderConfig)
				Expect(err).ToNot(HaveOccurred())
				Expect(mergedConfig.MachineImages).To(ConsistOf(
					MatchFields(IgnoreExtras, Fields{
						"Name": Equal("image-1"),
						"Versions": ContainElements(
							stackitv1alpha1.MachineImageVersion{Version: "1.0", Image: "image-name-1", Regions: []stackitv1alpha1.RegionIDMapping{{Name: "image-region-1", ID: "id-img-reg-1"}}},
							stackitv1alpha1.MachineImageVersion{Version: "1.1", Image: "image-name-2", Regions: []stackitv1alpha1.RegionIDMapping{{Name: "image-region-2", ID: "id-img-reg-2", Architecture: ptr.To("armhf")}}},
						),
					}),
					MatchFields(IgnoreExtras, Fields{
						"Name":     Equal("image-2"),
						"Versions": ContainElements(stackitv1alpha1.MachineImageVersion{Version: "2.0", Image: "image-name-3", Regions: []stackitv1alpha1.RegionIDMapping{{Name: "image-region-3", ID: "id-img-reg-3"}}}),
					}),
				))
			})
		})
	})
})
