// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
)

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.Path {
	case "/version":
		body = `{"major":"1","minor":"29","gitVersion":"v1.29.0"}`
	case "/api":
		body = `{"kind":"APIVersions","versions":["v1"]}`
	case "/apis":
		body = `{"kind":"APIGroupList","groups":[]}`
	default:
		body = `{"kind":"Status","status":"Failure","message":"Not Found","reason":"NotFound","code":404}`
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}, nil
}

var _ = Describe("CompatCSICompatibilityHandler", func() {
	var (
		ctx                context.Context
		fakeClient         client.Client
		handler            *CompatCSICompatibilityHandler
		namespace          string
		config             *rest.Config
		controlPlaneValues map[string]any
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "test-namespace"

		fakeClient = fakeclient.NewClientBuilder().
			WithScheme(kubernetes.SeedScheme).
			Build()

		config = &rest.Config{
			Host:      "https://localhost",
			Transport: &mockRoundTripper{},
		}

		handler, _ = NewCompatCSICompatibilityHandler(fakeClient, config)

		controlPlaneValues = map[string]any{
			"global": map[string]any{
				"genericTokenKubeconfigSecretName": "generic-token-kubeconfig-92e9ae14",
			},
			openstack.CSISTACKITControllerName: map[string]any{
				"foo": "bar",
			},
		}
	})

	Describe("#HandleSeedCSICompatibility", func() {
		Context("when CSICompatibilityMode is DEFAULT", func() {
			It("should delete the managed resource", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: string(stackitv1alpha1.DEFAULT),
						},
					},
				}

				// Create the managed resource and secret beforehand to ensure deletion works
				mr := &resourcesv1alpha1.ManagedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      csiCompatSeedChartName,
						Namespace: "kube-system",
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "managedresource-" + csiCompatSeedChartName,
						Namespace: "kube-system",
					},
				}
				Expect(fakeClient.Create(ctx, mr)).To(Succeed())
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				err := handler.HandleSeedCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				// Check deletion
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatSeedChartName, Namespace: namespace}, mr)
				Expect(err).To(HaveOccurred())
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when CSICompatibilityMode is not set", func() {
			It("should delete the managed resource", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: "",
						},
					},
				}

				// Create the managed resource and secret beforehand to ensure deletion works
				mr := &resourcesv1alpha1.ManagedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      csiCompatSeedChartName,
						Namespace: "kube-system",
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "managedresource-" + csiCompatSeedChartName,
						Namespace: "kube-system",
					},
				}
				Expect(fakeClient.Create(ctx, mr)).To(Succeed())
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				err := handler.HandleSeedCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				// Check deletion
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatSeedChartName, Namespace: namespace}, mr)
				Expect(err).To(HaveOccurred())
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when CSICompatibilityMode is COMPAT", func() {
			It("should deploy the seed csi compatibility mode", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: string(stackitv1alpha1.COMPAT),
						},
					},
				}

				err := handler.HandleSeedCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				mr := &resourcesv1alpha1.ManagedResource{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: "stackit-csi-compat-chart", Namespace: namespace}, mr)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("#HandleShootCSICompatibility", func() {
		Context("when CSICompatibilityMode is DEFAULT", func() {
			It("should delete the managed resource", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: string(stackitv1alpha1.DEFAULT),
						},
					},
				}

				// Create the managed resource and secret beforehand to ensure deletion works
				mr := &resourcesv1alpha1.ManagedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      csiCompatShootChartName,
						Namespace: namespace,
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "managedresource-" + csiCompatShootChartName,
						Namespace: namespace,
					},
				}
				Expect(fakeClient.Create(ctx, mr)).To(Succeed())
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				err := handler.HandleShootCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				// Check deletion
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatShootChartName, Namespace: namespace}, mr)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when CSICompatibilityMode is not set", func() {
			It("should delete the managed resource", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: "",
						},
					},
				}

				// Create the managed resource and secret beforehand to ensure deletion works
				mr := &resourcesv1alpha1.ManagedResource{
					ObjectMeta: metav1.ObjectMeta{
						Name:      csiCompatShootChartName,
						Namespace: namespace,
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "managedresource-" + csiCompatShootChartName,
						Namespace: namespace,
					},
				}
				Expect(fakeClient.Create(ctx, mr)).To(Succeed())
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				err := handler.HandleShootCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				// Check deletion
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatShootChartName, Namespace: namespace}, mr)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when CSICompatibilityMode is COMPAT", func() {
			It("should deploy the shoot csi compatibility mode with blockLegacyCreation = false", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: string(stackitv1alpha1.COMPAT),
						},
					},
				}

				err := handler.HandleShootCSICompatibility(ctx, namespace, cpConfig, controlPlaneValues)
				Expect(err).NotTo(HaveOccurred())

				mr := &resourcesv1alpha1.ManagedResource{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatShootChartName, Namespace: namespace}, mr)
				Expect(err).NotTo(HaveOccurred())

				ds := getDaemonSetFromSecret(ctx, fakeClient, namespace, "managedresource-"+csiCompatShootChartName)
				var csiContainer *corev1.Container
				for i := range ds.Spec.Template.Spec.Containers {
					if ds.Spec.Template.Spec.Containers[i].Name == "csi-driver-stackit" {
						csiContainer = &ds.Spec.Template.Spec.Containers[i]
						break
					}
				}
				Expect(csiContainer).NotTo(BeNil(), "csi-driver-stackit container not found")
				Expect(csiContainer.Args).To(ContainElement("--legacy-storage-mode=true"))
				Expect(csiContainer.Args).NotTo(ContainElement("--legacy-volume-creation=false"))
			})
		})

		Context("when CSICompatibilityMode is COMPATBLOCK", func() {
			It("should deploy the shoot csi compatibility mode with blockLegacyCreation = true", func() {
				cpConfig := &stackitv1alpha1.ControlPlaneConfig{
					Storage: &stackitv1alpha1.Storage{
						CSI: &stackitv1alpha1.CSI{
							CompatibilityMode: string(stackitv1alpha1.COMPATBLOCK),
						},
					},
				}

				values := map[string]any{
					"global": map[string]any{
						"genericTokenKubeconfigSecretName": "generic-token-kubeconfig-92e9ae14",
					},
					openstack.CSISTACKITControllerName: map[string]any{
						"foo": "bar",
					},
				}

				err := handler.HandleShootCSICompatibility(ctx, namespace, cpConfig, values)
				Expect(err).NotTo(HaveOccurred())

				mr := &resourcesv1alpha1.ManagedResource{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: csiCompatShootChartName, Namespace: namespace}, mr)
				Expect(err).NotTo(HaveOccurred())

				ds := getDaemonSetFromSecret(ctx, fakeClient, namespace, "managedresource-"+csiCompatShootChartName)
				var csiContainer *corev1.Container
				for i := range ds.Spec.Template.Spec.Containers {
					if ds.Spec.Template.Spec.Containers[i].Name == "csi-driver-stackit" {
						csiContainer = &ds.Spec.Template.Spec.Containers[i]
						break
					}
				}
				Expect(csiContainer).NotTo(BeNil(), "csi-driver-stackit container not found")
				Expect(csiContainer.Args).To(ContainElement("--legacy-storage-mode=true"))
				Expect(csiContainer.Args).To(ContainElement("--legacy-volume-creation=false"))
			})
		})
	})
})

func getDaemonSetFromSecret(ctx context.Context, fakeClient client.Client, namespace string, prefix string) *appsv1.DaemonSet {
	GinkgoHelper()
	secretList := &corev1.SecretList{}
	Expect(fakeClient.List(ctx, secretList, client.InNamespace(namespace))).To(Succeed())
	var matchedSecret *corev1.Secret
	var names []string
	for _, s := range secretList.Items {
		names = append(names, s.Name)
		if strings.HasPrefix(s.Name, prefix) {
			matchedSecret = &s
			break
		}
	}

	if matchedSecret == nil {
		Fail(fmt.Sprintf("Secret starting with prefix %s not found. Found secrets: %v", prefix, names))
	}

	for _, data := range matchedSecret.Data {
		docs := bytes.Split(data, []byte("\n---"))
		for _, doc := range docs {
			if bytes.Contains(doc, []byte("kind: DaemonSet")) {
				ds := &appsv1.DaemonSet{}
				Expect(yaml.Unmarshal(doc, ds)).To(Succeed())
				return ds
			}
		}
	}
	Fail("DaemonSet not found in secret " + matchedSecret.Name)
	return nil
}
