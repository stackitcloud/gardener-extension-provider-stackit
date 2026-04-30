// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// DefaultLoadBalancerPlanID is the default LB service plan for SelfHostedShootExposure.
const DefaultLoadBalancerPlanID = "p10"

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

func SetDefaults_SelfHostedShootExposureConfig(obj *SelfHostedShootExposureConfig) {
	if obj.LoadBalancer == nil {
		obj.LoadBalancer = &LoadBalancer{}
	}
	if obj.LoadBalancer.PlanID == nil {
		obj.LoadBalancer.PlanID = new(DefaultLoadBalancerPlanID)
	}
}

func SetDefaults_ControlPlaneConfig(obj *ControlPlaneConfig) {
	if obj == nil {
		obj = &ControlPlaneConfig{}
	}
	if obj.CloudControllerManager == nil {
		obj.CloudControllerManager = &CloudControllerManagerConfig{}
	}
	if obj.CloudControllerManager.Name == "" {
		obj.CloudControllerManager.Name = DefaultCCMName
	}
	if obj.Storage == nil {
		obj.Storage = &Storage{}
	}
	if obj.Storage.CSI == nil {
		obj.Storage.CSI = &CSI{}
	}
	if obj.Storage.CSI.Name == "" {
		obj.Storage.CSI.Name = DefaultCSIName
	}
}
