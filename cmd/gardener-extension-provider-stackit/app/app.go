// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"fmt"
	"os"

	druidcorev1alpha1 "github.com/gardener/etcd-druid/api/core/v1alpha1"
	"github.com/gardener/gardener/extensions/pkg/controller"
	controllercmd "github.com/gardener/gardener/extensions/pkg/controller/cmd"
	"github.com/gardener/gardener/extensions/pkg/controller/controlplane/genericactuator"
	"github.com/gardener/gardener/extensions/pkg/controller/heartbeat"
	heartbeatcmd "github.com/gardener/gardener/extensions/pkg/controller/heartbeat/cmd"
	"github.com/gardener/gardener/extensions/pkg/util"
	webhookcmd "github.com/gardener/gardener/extensions/pkg/webhook/cmd"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	gardenerhealthz "github.com/gardener/gardener/pkg/healthz"
	machinev1alpha1 "github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/component-base/version/verflag"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	stackitinstall "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/install"
	stackitcmd "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/cmd"
	stackitbastion "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/bastion"
	stackitcontrolplane "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/controlplane"
	stackitdnsrecord "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/dnsrecord"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/healthcheck"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure"
	stackitinfrastructure "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/infrastructure/stackit"
	stackitworker "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/controller/worker"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitwebhookcontrolplane "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/webhook/controlplane"
	stackitseedprovider "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/webhook/seedprovider"
)

// NewControllerManagerCommand creates a new command for running a STACKIT provider controller.
//
//nolint:funlen // copy-paste method from source repository
func NewControllerManagerCommand(ctx context.Context) *cobra.Command {
	var (
		generalOpts   = &controllercmd.GeneralOptions{}
		reconcileOpts = &controllercmd.ReconcilerOptions{}
		restOpts      = &controllercmd.RESTOptions{}
		mgrOpts       = &controllercmd.ManagerOptions{
			LeaderElection:          true,
			LeaderElectionID:        controllercmd.LeaderElectionNameID(stackit.Name),
			LeaderElectionNamespace: os.Getenv("LEADER_ELECTION_NAMESPACE"),
			WebhookServerPort:       443,
			WebhookCertDir:          "/tmp/gardener-extensions-cert",
			MetricsBindAddress:      ":8080",
			HealthBindAddress:       ":8081",
		}
		configFileOpts = &stackitcmd.ConfigOptions{}

		// options for the health care controller
		healthCheckCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the heartbeat controller
		heartbeatCtrlOpts = &heartbeatcmd.Options{
			ExtensionName:        stackit.Name,
			RenewIntervalSeconds: 30,
			Namespace:            os.Getenv("LEADER_ELECTION_NAMESPACE"),
		}

		// options for the bastion controller
		bastionCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the control plane controller
		controlPlaneCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		dnsRecordCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the infrastructure controller
		infraCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the worker controller
		workerCtrlOpts = &controllercmd.ControllerOptions{
			MaxConcurrentReconciles: 5,
		}

		// options for the webhook server
		webhookServerOptions = &webhookcmd.ServerOptions{
			Namespace: os.Getenv("WEBHOOK_CONFIG_NAMESPACE"),
		}

		controllerSwitches = stackitcmd.ControllerSwitchOptions()
		webhookSwitches    = stackitcmd.WebhookSwitchOptions()
		webhookOptions     = webhookcmd.NewAddToManagerOptions(
			stackit.Name,
			genericactuator.ShootWebhooksResourceName,
			genericactuator.ShootWebhookNamespaceSelector(stackit.Type),
			generalOpts,
			webhookServerOptions,
			webhookSwitches,
		)

		aggOption = controllercmd.NewOptionAggregator(
			generalOpts,
			restOpts,
			mgrOpts,
			controllercmd.PrefixOption("bastion-", bastionCtrlOpts),
			controllercmd.PrefixOption("controlplane-", controlPlaneCtrlOpts),
			controllercmd.PrefixOption("dnsrecord-", dnsRecordCtrlOpts),
			controllercmd.PrefixOption("infrastructure-", infraCtrlOpts),
			controllercmd.PrefixOption("worker-", workerCtrlOpts),
			controllercmd.PrefixOption("healthcheck-", healthCheckCtrlOpts),
			controllercmd.PrefixOption("heartbeat-", heartbeatCtrlOpts),
			controllerSwitches,
			configFileOpts,
			reconcileOpts,
			webhookOptions,
		)
	)

	cmd := &cobra.Command{
		Use: fmt.Sprintf("%s-controller-manager", stackit.Name),

		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			if err := aggOption.Complete(); err != nil {
				return fmt.Errorf("error completing options: %w", err)
			}

			if err := heartbeatCtrlOpts.Validate(); err != nil {
				return err
			}

			util.ApplyClientConnectionConfigurationToRESTConfig(configFileOpts.Completed().Config.ClientConnection, restOpts.Completed().Config)

			mgr, err := manager.New(restOpts.Completed().Config, mgrOpts.Completed().Options())
			if err != nil {
				return fmt.Errorf("could not instantiate manager: %w", err)
			}

			scheme := mgr.GetScheme()
			if err := controller.AddToScheme(scheme); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}
			if err := druidcorev1alpha1.AddToScheme(scheme); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}
			if err := stackitinstall.AddToScheme(scheme); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}
			if err := vpaautoscalingv1.AddToScheme(scheme); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}
			if err := machinev1alpha1.AddToScheme(scheme); err != nil {
				return fmt.Errorf("could not update manager scheme: %w", err)
			}

			log := mgr.GetLogger()
			gardenCluster, err := getGardenCluster(log)
			if err != nil {
				return err
			}
			log.Info("Adding garden cluster to manager")
			if err := mgr.Add(gardenCluster); err != nil {
				return fmt.Errorf("failed adding garden cluster to manager: %w", err)
			}

			log.Info("Adding controllers to manager")
			configFileOpts.Completed().ApplyETCDStorage(&stackitseedprovider.DefaultAddOptions.ETCDStorage)
			configFileOpts.Completed().ApplyHealthCheckConfig(&healthcheck.DefaultAddOptions.HealthCheckConfig)
			configFileOpts.Completed().ApplyRegistryCaches(&stackitwebhookcontrolplane.DefaultAddOptions.RegistryCaches)
			configFileOpts.Completed().ApplyDeployALBIngressController(&stackitcontrolplane.DeployALBIngressController)
			configFileOpts.Completed().ApplyCustomLabelDomain(&stackitworker.DefaultAddOptions.CustomLabelDomain)
			configFileOpts.Completed().ApplyCustomLabelDomain(&stackitcontrolplane.DefaultAddOptions.CustomLabelDomain)
			configFileOpts.Completed().ApplyCustomLabelDomain(&stackitinfrastructure.DefaultAddOptions.CustomLabelDomain)
			log.Info("DeployALBIngressController?", "deploy", configFileOpts.Completed().Config.DeployALBIngressController)

			bastionCtrlOpts.Completed().Apply(&stackitbastion.DefaultAddOptions.Controller)
			configFileOpts.Completed().ApplyCustomLabelDomain(&stackitbastion.DefaultAddOptions.CustomLabelDomain)
			controlPlaneCtrlOpts.Completed().Apply(&stackitcontrolplane.DefaultAddOptions.Controller)
			dnsRecordCtrlOpts.Completed().Apply(&stackitdnsrecord.DefaultAddOptions.Controller)
			healthCheckCtrlOpts.Completed().Apply(&healthcheck.DefaultAddOptions.Controller)
			heartbeatCtrlOpts.Completed().Apply(&heartbeat.DefaultAddOptions)
			configFileOpts.Completed().ApplyCustomLabelDomain(&infrastructure.DefaultAddOptions.CustomLabelDomain)
			infraCtrlOpts.Completed().Apply(&stackitinfrastructure.DefaultAddOptions.Controller)
			workerCtrlOpts.Completed().Apply(&stackitworker.DefaultAddOptions.Controller)

			reconcileOpts.Completed().Apply(&stackitbastion.DefaultAddOptions.IgnoreOperationAnnotation, &stackitbastion.DefaultAddOptions.ExtensionClasses)
			reconcileOpts.Completed().Apply(&stackitcontrolplane.DefaultAddOptions.IgnoreOperationAnnotation, &stackitcontrolplane.DefaultAddOptions.ExtensionClasses)
			reconcileOpts.Completed().Apply(&stackitdnsrecord.DefaultAddOptions.IgnoreOperationAnnotation, &stackitdnsrecord.DefaultAddOptions.ExtensionClasses)
			reconcileOpts.Completed().Apply(&stackitinfrastructure.DefaultAddOptions.IgnoreOperationAnnotation, &stackitinfrastructure.DefaultAddOptions.ExtensionClasses)
			reconcileOpts.Completed().Apply(&stackitworker.DefaultAddOptions.IgnoreOperationAnnotation, &stackitworker.DefaultAddOptions.ExtensionClasses)

			stackitworker.DefaultAddOptions.GardenCluster = gardenCluster
			stackitworker.DefaultAddOptions.SelfHostedShootCluster = generalOpts.Completed().SelfHostedShootCluster

			if _, err := webhookOptions.Completed().AddToManager(ctx, mgr, nil); err != nil {
				return fmt.Errorf("could not add webhooks to manager: %w", err)
			}

			stackitcontrolplane.DefaultAddOptions.WebhookServerNamespace = webhookOptions.Server.Namespace

			if err := controllerSwitches.Completed().AddToManager(ctx, mgr); err != nil {
				return fmt.Errorf("could not add controllers to manager: %w", err)
			}

			if err := mgr.AddReadyzCheck("informer-sync", gardenerhealthz.NewCacheSyncHealthz(mgr.GetCache())); err != nil {
				return fmt.Errorf("could not add readycheck for informers: %w", err)
			}

			if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
				return fmt.Errorf("could not add health check to manager: %w", err)
			}

			if err := mgr.AddReadyzCheck("webhook-server", mgr.GetWebhookServer().StartedChecker()); err != nil {
				return fmt.Errorf("could not add ready check for webhook server to manager: %w", err)
			}

			if err := mgr.Start(ctx); err != nil {
				return fmt.Errorf("error running manager: %w", err)
			}

			return nil
		},
	}

	verflag.AddFlags(cmd.Flags())
	aggOption.AddFlags(cmd.Flags())
	feature.MutableGate.AddFlag(cmd.Flags())

	return cmd
}

func getGardenCluster(log logr.Logger) (cluster.Cluster, error) {
	log.Info("Getting rest config for garden")
	gardenRESTConfig, err := kubernetes.RESTConfigFromKubeconfigFile(os.Getenv("GARDEN_KUBECONFIG"), kubernetes.AuthTokenFile)
	if err != nil {
		return nil, err
	}

	log.Info("Setting up cluster object for garden")
	gardenCluster, err := cluster.New(gardenRESTConfig, func(opts *cluster.Options) {
		opts.Scheme = kubernetes.GardenScheme
		opts.Logger = log
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating garden cluster object: %w", err)
	}

	return gardenCluster, nil
}
