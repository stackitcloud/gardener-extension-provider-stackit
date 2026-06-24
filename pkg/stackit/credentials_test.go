// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package stackit_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	mockclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/mock/controller-runtime/client"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var (
	projectID = "foo"
	saKeyJSON = "{}"
)

var _ = Describe("Secret", func() {
	Describe("#GetCredentialsFromSecretRef", func() {
		var (
			c client.Client

			ctx       = context.TODO()
			namespace = "namespace"
			name      = "name"

			secretRef = corev1.SecretReference{
				Name:      name,
				Namespace: namespace,
			}
		)

		BeforeEach(func() {
			c = fake.NewClientBuilder().Build()
		})

		It("should fail if the secret could not be read", func() {
			fakeErr := errors.New("error")
			c = fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
					Expect(key).To(Equal(client.ObjectKey{Namespace: namespace, Name: name}))
					Expect(obj).To(BeAssignableToTypeOf(&corev1.Secret{}))
					return fakeErr
				},
			}).Build()

			credentials, err := GetCredentialsFromSecretRef(ctx, c, secretRef)

			Expect(err).To(Equal(fakeErr))
			Expect(credentials).To(BeNil())
		})

		It("should return the correct credentials object", func() {
			c = fake.NewClientBuilder().WithObjects(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretRef.Name,
					Namespace: secretRef.Namespace,
				},
				Data: map[string][]byte{
					ProjectID: []byte(projectID),
					SaKeyJSON: []byte(saKeyJSON),
				},
			}).Build()

			credentials, err := GetCredentialsFromSecretRef(ctx, c, secretRef)

			Expect(credentials).To(Equal(&Credentials{
				ProjectID: projectID,
				SaKeyJSON: saKeyJSON,
			}))
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
