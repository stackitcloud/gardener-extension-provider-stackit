// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package healthcheck

import (
	"context"
	"time"

	healthcheckconfig "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/healthcheck"
	"github.com/gardener/gardener/extensions/pkg/controller/healthcheck/general"
	"github.com/gardener/gardener/extensions/pkg/controller/healthcheck/worker"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/controlplane"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

var (
	defaultSyncPeriod = time.Second * 30
	// DefaultAddOptions are the default DefaultAddArgs for AddToManager.
	DefaultAddOptions = healthcheck.DefaultAddArgs{
		HealthCheckConfig: healthcheckconfig.HealthCheckConfig{
			SyncPeriod: metav1.Duration{Duration: defaultSyncPeriod},
			ShootRESTOptions: &healthcheckconfig.RESTOptions{
				QPS:   ptr.To[float32](100),
				Burst: ptr.To(130),
			},
		},
	}
)

// RegisterHealthChecks registers health checks for each extension resource
// HealthChecks are grouped by extension (e.g worker), extension.type (e.g aws) and  Health Check Type (e.g ShootControlPlaneHealthy)
func RegisterHealthChecks(ctx context.Context, mgr manager.Manager, opts healthcheck.DefaultAddArgs) error {
	healthchecks := []healthcheck.ConditionTypeToHealthCheck{
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(openstack.STACKITCloudControllerManagerName),
		},
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(openstack.CloudControllerManagerName),
			PreCheckFunc:  checkCCMOpenstack,
		},
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(openstack.CSIControllerName),
			PreCheckFunc:  checkCSIOpenstack,
		},
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(openstack.CSISnapshotControllerName),
			PreCheckFunc:  checkCSIOpenstack,
		},
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(controlplane.CSIStackitPrefix + "-" + openstack.CSIControllerName),
			PreCheckFunc:  checkCSISTACKIT,
		},
		{
			ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
			HealthCheck:   general.NewSeedDeploymentHealthChecker(controlplane.CSIStackitPrefix + "-" + openstack.CSISnapshotControllerName),
			PreCheckFunc:  checkCSISTACKIT,
		},
	}

	if controlplane.DeployALBIngressController {
		healthchecks = append(healthchecks,
			healthcheck.ConditionTypeToHealthCheck{
				ConditionType: string(gardencorev1beta1.ShootControlPlaneHealthy),
				HealthCheck:   general.NewSeedDeploymentHealthChecker(openstack.STACKITALBControllerManagerName),
				PreCheckFunc:  checkALB,
			},
		)
	}

	if err := healthcheck.DefaultRegistration(
		stackit.Type,
		extensionsv1alpha1.SchemeGroupVersion.WithKind(extensionsv1alpha1.ControlPlaneResource),
		func() client.ObjectList { return &extensionsv1alpha1.ControlPlaneList{} },
		func() extensionsv1alpha1.Object { return &extensionsv1alpha1.ControlPlane{} },
		mgr,
		opts,
		nil,
		healthchecks,
		sets.New[gardencorev1beta1.ConditionType](),
	); err != nil {
		return err
	}

	return healthcheck.DefaultRegistration(
		stackit.Type,
		extensionsv1alpha1.SchemeGroupVersion.WithKind(extensionsv1alpha1.WorkerResource),
		func() client.ObjectList { return &extensionsv1alpha1.WorkerList{} },
		func() extensionsv1alpha1.Object { return &extensionsv1alpha1.Worker{} },
		mgr,
		opts,
		nil,
		[]healthcheck.ConditionTypeToHealthCheck{{
			ConditionType: string(gardencorev1beta1.ShootEveryNodeReady),
			HealthCheck:   worker.NewNodesChecker(),
			ErrorCodeCheckFunc: func(err error) []gardencorev1beta1.ErrorCode {
				return util.DetermineErrorCodes(err, helper.KnownCodes)
			},
		}},
		sets.New(gardencorev1beta1.ShootControlPlaneHealthy),
	)
}

func checkCSIOpenstack(_ context.Context, client client.Client, _ client.Object, clusterObj any) bool {
	cluster, ok := clusterObj.(*extensionscontroller.Cluster)
	if !ok {
		return false
	}
	return getCSINameFromCluster(cluster) == stackitv1alpha1.OPENSTACK
}

func checkCSISTACKIT(_ context.Context, client client.Client, _ client.Object, clusterObj any) bool {
	cluster, ok := clusterObj.(*extensionscontroller.Cluster)
	if !ok {
		return false
	}
	return getCSINameFromCluster(cluster) == stackitv1alpha1.STACKIT
}

func checkCCMOpenstack(_ context.Context, _ client.Client, _ client.Object, clusterObj any) bool {
	cluster, ok := clusterObj.(*extensionscontroller.Cluster)
	if !ok {
		return false
	}
	return getCCMNameFromCluster(cluster) == stackitv1alpha1.OPENSTACK
}

// getCSINameFromCluster returns the ControllerName of the Cluster
func getCSINameFromCluster(ext *extensionscontroller.Cluster) stackitv1alpha1.ControllerName {
	cpConfig, err := helper.ControlPlaneConfigFromCluster(ext)
	if err != nil {
		return ""
	}

	return stackitv1alpha1.ControllerName(cpConfig.Storage.CSI.Name)
}

func getCCMNameFromCluster(ext *extensionscontroller.Cluster) stackitv1alpha1.ControllerName {
	cpConfig, err := helper.ControlPlaneConfigFromCluster(ext)
	if err != nil {
		return ""
	}

	return stackitv1alpha1.ControllerName(cpConfig.CloudControllerManager.Name)
}

func checkALB(_ context.Context, client client.Client, _ client.Object, clusterObj any) bool {
	cluster, ok := clusterObj.(*extensionscontroller.Cluster)
	if !ok {
		return false
	}
	cpConfig, err := helper.ControlPlaneConfigFromCluster(cluster)
	if err != nil {
		return false
	}

	return controlplane.DeploySTACKITALB(cpConfig)
}

// AddToManager adds a controller with the default Options.
func AddToManager(ctx context.Context, mgr manager.Manager) error {
	return RegisterHealthChecks(ctx, mgr, DefaultAddOptions)
}
