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

package stackit

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrSecretNoData = errors.New("secret does not contain any data")
	ErrFieldMissing = errors.New("missing field in secret")
)

const (
	// ProjectID from the project controller
	ProjectID = "project-id"
	// SaKeyJSON serviceaccount.json from the STACKIT SA
	SaKeyJSON = "serviceaccount.json"
)

// Credentials stores STACKIT credentials.
type Credentials struct {
	ProjectID                     string
	SaKeyJSON                     string
	LoadBalancerAPIEmergencyToken string
}

// GetCredentialsFromSecretRef reads the secret given by the secret reference and returns the read Credentials
// object.
func GetCredentialsFromSecretRef(ctx context.Context, k8sClient client.Client, secretRef corev1.SecretReference) (*Credentials, error) {
	secret, err := extensionscontroller.GetSecretByReference(ctx, k8sClient, &secretRef)
	if err != nil {
		return nil, err
	}
	return ReadCredentialsSecret(secret)
}

// ReadCredentialsSecret reads a secret containing credentials.
func ReadCredentialsSecret(secret *corev1.Secret) (*Credentials, error) {
	if secret.Data == nil {
		return nil, ErrSecretNoData
	}

	projectID, err := getSecretDataValue(secret, ProjectID, true)
	if err != nil {
		return nil, err
	}

	saKeyJSON, err := getSecretDataValue(secret, SaKeyJSON, true)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		ProjectID: projectID,
		SaKeyJSON: saKeyJSON,
	}, nil
}

func getSecretDataValue(secret *corev1.Secret, key string, required bool) (string, error) {
	if value, ok := secret.Data[key]; ok {
		return string(value), nil
	}
	if required {
		return "", errors.Wrap(ErrFieldMissing, key)
	}
	return "", nil
}
