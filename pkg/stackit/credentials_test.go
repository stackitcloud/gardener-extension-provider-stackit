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

	mockclient "github.com/gardener/gardener/third_party/mock/controller-runtime/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var (
	projectID = "foo"
	saKeyJSON = "{}"
)

var _ = Describe("Secret", func() {
	Describe("#GetCredentialsFromSecretRef", func() {
		var (
			ctrl *gomock.Controller
			c    *mockclient.MockClient

			ctx       = context.TODO()
			namespace = "namespace"
			name      = "name"

			secretRef = corev1.SecretReference{
				Name:      name,
				Namespace: namespace,
			}
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())

			c = mockclient.NewMockClient(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		It("should fail if the secret could not be read", func() {
			fakeErr := errors.New("error")
			c.EXPECT().Get(ctx, client.ObjectKey{namespace, name}, gomock.AssignableToTypeOf(&corev1.Secret{})).Return(fakeErr)

			credentials, err := GetCredentialsFromSecretRef(ctx, c, secretRef)

			Expect(err).To(Equal(fakeErr))
			Expect(credentials).To(BeNil())
		})

		It("should return the correct credentials object", func() {
			c.EXPECT().Get(
				ctx, client.ObjectKey{namespace, name},
				gomock.AssignableToTypeOf(&corev1.Secret{}),
				gomock.Any(),
			).DoAndReturn(
				func(_ context.Context, _ client.ObjectKey, secret *corev1.Secret, _ ...client.GetOption) error {
					secret.Data = map[string][]byte{
						ProjectID: []byte(projectID),
						SaKeyJSON: []byte(saKeyJSON),
					}
					return nil
				},
			)

			credentials, err := GetCredentialsFromSecretRef(ctx, c, secretRef)

			Expect(credentials).To(Equal(&Credentials{
				ProjectID: projectID,
				SaKeyJSON: saKeyJSON,
			}))
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
