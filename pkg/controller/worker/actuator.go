// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package worker

import (
	"context"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/worker"
	"github.com/gardener/gardener/extensions/pkg/controller/worker/genericactuator"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardener "github.com/gardener/gardener/pkg/client/kubernetes"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
	openstackclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/openstack/client"
)

type delegateFactory struct {
	seedClient        client.Client
	restConfig        *rest.Config
	scheme            *runtime.Scheme
	customLabelDomain string
}

// NewActuator creates a new Actuator that updates the status of the handled WorkerPoolConfigs.
func NewActuator(mgr manager.Manager, gardenCluster cluster.Cluster, customLabelDomain string) worker.Actuator {
	var (
		workerDelegate = &delegateFactory{
			seedClient:        mgr.GetClient(),
			restConfig:        mgr.GetConfig(),
			scheme:            mgr.GetScheme(),
			customLabelDomain: customLabelDomain,
		}
	)

	return genericactuator.NewActuator(
		mgr,
		gardenCluster,
		workerDelegate,
		func(err error) []gardencorev1beta1.ErrorCode {
			return util.DetermineErrorCodes(err, helper.KnownCodes)
		},
	)
}

func (d *delegateFactory) WorkerDelegate(ctx context.Context, worker *extensionsv1alpha1.Worker, cluster *extensionscontroller.Cluster) (genericactuator.WorkerDelegate, error) {
	clientset, err := kubernetes.NewForConfig(d.restConfig)
	if err != nil {
		return nil, err
	}

	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	seedChartApplier, err := gardener.NewChartApplierForConfig(d.restConfig)
	if err != nil {
		return nil, err
	}

	return NewWorkerDelegate(
		d.seedClient,
		d.scheme,

		seedChartApplier,
		serverVersion.GitVersion,

		worker,
		cluster,
		d.customLabelDomain,
	)
}

type workerDelegate struct {
	seedClient client.Client
	scheme     *runtime.Scheme
	decoder    runtime.Decoder

	seedChartApplier gardener.ChartApplier
	serverVersion    string

	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig
	cluster            *extensionscontroller.Cluster
	worker             *extensionsv1alpha1.Worker
	customLabelDomain  string

	machineClasses     []map[string]any
	machineDeployments worker.MachineDeployments
	machineImages      []stackitv1alpha1.MachineImage

	openstackClient openstackclient.Factory
}

// NewWorkerDelegate creates a new context for a worker reconciliation.
func NewWorkerDelegate(
	seedClient client.Client,
	scheme *runtime.Scheme,

	seedChartApplier gardener.ChartApplier,
	serverVersion string,

	worker *extensionsv1alpha1.Worker,
	cluster *extensionscontroller.Cluster,
	customLabelDomain string,
) (genericactuator.WorkerDelegate, error) {
	config, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}

	return &workerDelegate{
		seedClient: seedClient,
		scheme:     scheme,
		decoder:    serializer.NewCodecFactory(scheme, serializer.EnableStrict).UniversalDecoder(),

		seedChartApplier: seedChartApplier,
		serverVersion:    serverVersion,

		cloudProfileConfig: config,
		cluster:            cluster,
		worker:             worker,
		customLabelDomain:  customLabelDomain,
	}, nil
}
