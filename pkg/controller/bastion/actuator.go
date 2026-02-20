package bastion

import (
	"context"
	"fmt"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/controllerutils/reconciler"
	"github.com/go-logr/logr"
	iaaswait "github.com/stackitcloud/stackit-sdk-go/services/iaas/wait"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
)

type Actuator struct {
	Client            client.Client
	Decoder           runtime.Decoder
	CustomLabelDomain string
}

func (a *Actuator) WithManager(mgr manager.Manager) *Actuator {
	if a.Client == nil {
		a.Client = mgr.GetClient()
	}
	if a.Decoder == nil {
		a.Decoder = serializer.NewCodecFactory(a.Client.Scheme(), serializer.EnableStrict).UniversalDecoder()
	}

	return a
}

func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, bastion *extensionsv1alpha1.Bastion, cluster *extensionscontroller.Cluster) error {
	r, err := a.getResources(ctx, log, bastion, cluster)
	if err != nil {
		return err
	}

	if err := r.reconcileSecurityGroup(ctx, log); err != nil {
		return fmt.Errorf("error reconciling security group: %w", err)
	}

	if err := r.reconcileWorkerSecurityGroupRule(ctx, log); err != nil {
		return fmt.Errorf("error reconciling worker security group rule: %w", err)
	}

	if err := r.reconcileServer(ctx, log); err != nil {
		return fmt.Errorf("error reconciling server: %w", err)
	}

	if err := r.reconcilePublicIP(ctx, log); err != nil {
		return fmt.Errorf("error reconciling public IP: %w", err)
	}

	switch r.Server.GetStatus() {
	case iaaswait.ServerActiveStatus:
		log.Info("Server for Bastion is active", "server", r.Server.GetId())
	case iaaswait.ErrorStatus:
		message := ""
		if r.Server.HasErrorMessage() {
			message = " with message: " + r.Server.GetErrorMessage()
		}

		return &reconciler.RequeueAfterError{
			RequeueAfter: 5 * time.Minute,
			Cause:        fmt.Errorf("server %s is in status %s%s", r.Server.GetId(), r.Server.GetStatus(), message),
		}
	default:
		return &reconciler.RequeueAfterError{
			RequeueAfter: 15 * time.Second,
			Cause:        fmt.Errorf("waiting for server to become ready, current status: %s", r.Server.GetStatus()),
		}
	}

	// We're ready, publish the endpoint on the Bastion resource to notify the client.
	patch := client.MergeFrom(bastion.DeepCopy())
	bastion.Status.Ingress = &corev1.LoadBalancerIngress{
		IP: r.PublicIP.GetIp(),
	}
	return a.Client.Status().Patch(ctx, bastion, patch)
}

func (a *Actuator) Delete(ctx context.Context, log logr.Logger, bastion *extensionsv1alpha1.Bastion, cluster *extensionscontroller.Cluster) error {
	r, err := a.getResources(ctx, log, bastion, cluster)
	if err != nil {
		return err
	}

	if err := r.deletePublicIP(ctx, log); err != nil {
		return fmt.Errorf("error deleting public IP: %w", err)
	}

	if err := r.deleteServer(ctx, log); err != nil {
		return fmt.Errorf("error deleting server: %w", err)
	}

	if err := r.deleteSecurityGroup(ctx, log); err != nil {
		return fmt.Errorf("error deleting security group: %w", err)
	}

	return nil
}

func (a *Actuator) ForceDelete(context.Context, logr.Logger, *extensionsv1alpha1.Bastion, *extensionscontroller.Cluster) error {
	// Nothing to do for force deletion.
	// Gardener expects us to orphan all remaining resources in the shoot infrastructure.
	return nil
}

// getResources initializes Resources and Options for the given Bastion, needed for reconciliation/deletion.
func (a *Actuator) getResources(ctx context.Context, log logr.Logger, bastion *extensionsv1alpha1.Bastion, cluster *extensionscontroller.Cluster) (*Resources, error) {
	region := stackit.DetermineRegion(cluster)

	secretRef := corev1.SecretReference{
		Name:      v1beta1constants.SecretNameCloudProvider,
		Namespace: bastion.Namespace,
	}

	iaasClient, err := stackitclient.New(region, cluster).IaaS(ctx, a.Client, secretRef)
	if err != nil {
		return nil, fmt.Errorf("error creating IaaS client: %w", err)
	}

	opts, err := a.DetermineOptions(ctx, bastion, cluster, iaasClient.ProjectID())
	if err != nil {
		return nil, fmt.Errorf("error determining options: %w", err)
	}

	r := &Resources{
		Options: *opts,
		IaaS:    iaasClient,
	}
	if err := r.getExistingResources(ctx, log); err != nil {
		return nil, fmt.Errorf("error getting existing resources: %w", err)
	}

	return r, nil
}

func (a *Actuator) getStackitCredentials(ctx context.Context, bastion *extensionsv1alpha1.Bastion) (*stackit.Credentials, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: bastion.Namespace, Name: v1beta1constants.SecretNameCloudProvider}

	if err := a.Client.Get(ctx, key, secret); err != nil {
		return nil, fmt.Errorf("error reading %q secret: %w", v1beta1constants.SecretNameCloudProvider, err)
	}

	credentials, err := stackit.ReadCredentialsSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("error reading STACKIT credentials: %w", err)
	}

	return credentials, nil
}
