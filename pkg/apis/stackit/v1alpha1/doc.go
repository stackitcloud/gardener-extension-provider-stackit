// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// +k8s:deepcopy-gen=package
// +k8s:openapi-gen=true
// +k8s:defaulter-gen=TypeMeta

//go:generate crd-ref-docs --source-path=. --config=../../../../hack/api-reference/api.yaml --renderer=markdown --templates-dir=$GARDENER_HACK_DIR/api-reference/template --log-level=ERROR --output-path=../../../../hack/api-reference/api.md

// Package v1alpha1 contains the STACKIT provider API resources.
// +groupName=stackit.provider.extensions.gardener.cloud
package v1alpha1 // import "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
