// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/extensions/pkg/webhook/controlplane"
	"github.com/gardener/gardener/extensions/pkg/webhook/controlplane/genericmutator"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/original/components/kubelet"
	oscutils "github.com/gardener/gardener/pkg/component/extensions/operatingsystemconfig/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/config"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

var DefaultAddOptions = AddOptions{}

type AddOptions struct {
	RegistryCaches []config.RegistryCacheConfiguration
}

var logger = log.Log.WithName("stackit-controlplane-webhook")

// AddToManager creates a webhook and adds it to the manager.
func AddToManagerWithOptions(mgr manager.Manager, opts AddOptions) (*extensionswebhook.Webhook, error) {
	logger.Info("Adding webhook to manager")
	fciCodec := oscutils.NewFileContentInlineCodec()
	return controlplane.New(mgr, controlplane.Args{
		Kind:     controlplane.KindShoot,
		Provider: stackit.Type,
		Types: []extensionswebhook.Type{
			{Obj: &appsv1.Deployment{}},
			{Obj: &vpaautoscalingv1.VerticalPodAutoscaler{}},
			{Obj: &extensionsv1alpha1.OperatingSystemConfig{}},
		},
		ObjectSelector: &metav1.LabelSelector{MatchLabels: map[string]string{v1beta1constants.LabelExtensionProviderMutatedByControlplaneWebhook: "true"}},
		Mutator: genericmutator.NewMutator(mgr, NewEnsurer(opts.RegistryCaches, logger), oscutils.NewUnitSerializer(),
			kubelet.NewConfigCodec(fciCodec), fciCodec, logger),
	})
}

// AddToManager creates a webhook with the default options and adds it to the manager.
func AddToManager(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	return AddToManagerWithOptions(mgr, DefaultAddOptions)
}
