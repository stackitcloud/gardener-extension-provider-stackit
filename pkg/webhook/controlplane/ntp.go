// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gcontext "github.com/gardener/gardener/extensions/pkg/webhook/context"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ubuntuOSType            = "ubuntu"
	ubuntu2604VersionPrefix = "2604"
	ntpInstallUnitName      = "install-ntp-client.service"
	ntpInstallDropInName    = "99-skip-ntp-install.conf"
	ntpInstallDropInContent = "[Service]\nExecStart=\nExecStart=/bin/true\n"
)

type ntpMutator struct {
	extensionswebhook.Mutator
	client client.Client
}

func newNTPMutator(delegate extensionswebhook.Mutator, c client.Client) extensionswebhook.Mutator {
	return &ntpMutator{Mutator: delegate, client: c}
}

func (m *ntpMutator) Mutate(ctx context.Context, newObj, oldObj client.Object) error {
	if err := m.Mutator.Mutate(ctx, newObj, oldObj); err != nil {
		return err
	}

	osc, ok := newObj.(*extensionsv1alpha1.OperatingSystemConfig)
	if !ok || osc.DeletionTimestamp != nil || osc.Spec.Type != ubuntuOSType || osc.Spec.Purpose != extensionsv1alpha1.OperatingSystemConfigPurposeReconcile {
		return nil
	}

	cluster, err := gcontext.NewGardenContext(m.client, osc).GetCluster(ctx)
	if err != nil {
		return err
	}

	ensureNTPInstallDisabled(osc, cluster)
	return nil
}

func ensureNTPInstallDisabled(osc *extensionsv1alpha1.OperatingSystemConfig, cluster *extensionscontroller.Cluster) {
	if !isUbuntu2604Pool(cluster, osc.Labels[v1beta1constants.LabelWorkerPool]) {
		return
	}

	osc.Spec.Units = extensionswebhook.EnsureUnitWithName(osc.Spec.Units, extensionsv1alpha1.Unit{
		Name: ntpInstallUnitName,
		DropIns: []extensionsv1alpha1.DropIn{{
			Name:    ntpInstallDropInName,
			Content: ntpInstallDropInContent,
		}},
	})
}

func isUbuntu2604Pool(cluster *extensionscontroller.Cluster, poolName string) bool {
	if cluster == nil || cluster.Shoot == nil || poolName == "" {
		return false
	}

	for _, pool := range cluster.Shoot.Spec.Provider.Workers {
		if pool.Name != poolName {
			continue
		}
		image := pool.Machine.Image
		return image != nil && image.Name == ubuntuOSType && image.Version != nil && strings.HasPrefix(*image.Version, ubuntu2604VersionPrefix)
	}

	return false
}
