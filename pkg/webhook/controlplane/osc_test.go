// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("#ensureNTPInstallDisabled", func() {
	var (
		cluster *extensionscontroller.Cluster
		osc     *extensionsv1alpha1.OperatingSystemConfig

		disabledUnit = extensionsv1alpha1.Unit{
			Name: ntpInstallUnitName,
			DropIns: []extensionsv1alpha1.DropIn{{
				Name:    ntpInstallDropInName,
				Content: ntpInstallDropInContent,
			}},
		}
	)

	pool := func(name, image, version string) gardencorev1beta1.Worker {
		return gardencorev1beta1.Worker{
			Name: name,
			Machine: gardencorev1beta1.Machine{
				Image: &gardencorev1beta1.ShootMachineImage{Name: image, Version: new(version)},
			},
		}
	}

	oscForPool := func(poolName string) *extensionsv1alpha1.OperatingSystemConfig {
		return &extensionsv1alpha1.OperatingSystemConfig{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{v1beta1constants.LabelWorkerPool: poolName},
			},
			Spec: extensionsv1alpha1.OperatingSystemConfigSpec{
				Purpose: extensionsv1alpha1.OperatingSystemConfigPurposeReconcile,
			},
		}
	}

	BeforeEach(func() {
		cluster = &extensionscontroller.Cluster{
			Shoot: &gardencorev1beta1.Shoot{
				Spec: gardencorev1beta1.ShootSpec{
					Provider: gardencorev1beta1.Provider{
						Workers: []gardencorev1beta1.Worker{
							pool("jammy", "ubuntu", "2204.20260917.0"),
							pool("resolute", "ubuntu", "2604.20260917.0"),
						},
					},
				},
			},
		}
	})

	It("should add the drop-in for a pool running Ubuntu 26.04", func() {
		osc = oscForPool("resolute")
		ensureNTPInstallDisabled(osc, cluster)
		Expect(osc.Spec.Units).To(ConsistOf(disabledUnit))
	})
	It("should not add the drop-in for a pool running Ubuntu 22.04", func() {
		osc = oscForPool("jammy")
		ensureNTPInstallDisabled(osc, cluster)
		Expect(osc.Spec.Units).To(BeEmpty())
	})
	It("should not add the drop-in for an OSC without a pool label", func() {
		osc = oscForPool("")
		ensureNTPInstallDisabled(osc, cluster)
		Expect(osc.Spec.Units).To(BeEmpty())
	})
	It("should not add the drop-in for a pool that is not in the shoot", func() {
		osc = oscForPool("unknown")
		ensureNTPInstallDisabled(osc, cluster)
		Expect(osc.Spec.Units).To(BeEmpty())
	})
	It("should keep existing units", func() {
		osc = oscForPool("resolute")
		existing := extensionsv1alpha1.Unit{Name: "dummy.service", Content: new("dummy")}
		osc.Spec.Units = []extensionsv1alpha1.Unit{existing}

		ensureNTPInstallDisabled(osc, cluster)
		Expect(osc.Spec.Units).To(ConsistOf(existing, disabledUnit))
	})

})
