package selfhostedshootexposure

import (
	"context"
	"fmt"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/util"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

type Actuator struct {
	Client  client.Client
	Decoder runtime.Decoder
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

func (a *Actuator) Reconcile(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) ([]corev1.LoadBalancerIngress, error) {
	ingresses, err := a.reconcile(ctx, log, exposure, cluster)
	return ingresses, util.DetermineError(err, helper.KnownCodes)
}

func (a *Actuator) reconcile(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) ([]corev1.LoadBalancerIngress, error) {
	r, err := a.getResources(ctx, log, exposure, cluster)
	if err != nil {
		return nil, err
	}

	if err := r.reconcileLoadBalancer(ctx, log); err != nil {
		return nil, fmt.Errorf("error reconciling load balancer: %w", err)
	}

	if err := r.checkLoadBalancerReady(log); err != nil {
		return nil, err
	}

	return []corev1.LoadBalancerIngress{
		{IP: *r.LoadBalancer.ExternalAddress},
	}, nil
}

func (a *Actuator) Delete(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) error {
	return util.DetermineError(a.delete(ctx, log, exposure, cluster), helper.KnownCodes)
}

func (a *Actuator) delete(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) error {
	r, err := a.getResources(ctx, log, exposure, cluster)
	if err != nil {
		return err
	}

	if err := r.deleteLoadBalancer(ctx, log); err != nil {
		return fmt.Errorf("error deleting load balancer: %w", err)
	}

	return nil
}

func (a *Actuator) ForceDelete(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) error {
	return a.Delete(ctx, log, exposure, cluster)
}

// getResources initializes Resources and Options for the given SelfHostedShootExposure, needed for reconciliation/deletion.
func (a *Actuator) getResources(ctx context.Context, log logr.Logger, exposure *extensionsv1alpha1.SelfHostedShootExposure, cluster *extensionscontroller.Cluster) (*Resources, error) {
	region := stackit.DetermineRegion(cluster)

	// Determine which secret to use for credentials.
	// Support explicit CredentialsRef (required by GEP-0036), with fallback to default cloud-provider secret.
	// TODO(jamand): Support WorkloadIdentity once integrated into SKE.
	// For now, we only support secret-based credentials via ObjectReference pointing to a Secret.
	var secretRef corev1.SecretReference
	if exposure.Spec.CredentialsRef != nil {
		// Use the explicitly provided ObjectReference (must point to a Secret for now)
		secretRef = corev1.SecretReference{
			Name:      exposure.Spec.CredentialsRef.Name,
			Namespace: exposure.Spec.CredentialsRef.Namespace,
		}
	} else {
		// Fall back to the default cloud-provider secret
		secretRef = corev1.SecretReference{
			Name:      v1beta1constants.SecretNameCloudProvider,
			Namespace: exposure.Namespace,
		}
	}

	lbClient, err := stackitclient.New(region, cluster).LoadBalancing(ctx, a.Client, secretRef)
	if err != nil {
		return nil, fmt.Errorf("error creating LoadBalancer client: %w", err)
	}

	opts, err := a.DetermineOptions(ctx, exposure, cluster, lbClient.ProjectID())
	if err != nil {
		return nil, fmt.Errorf("error determining options: %w", err)
	}

	r := &Resources{
		Options:  *opts,
		LBClient: lbClient,
	}
	if err := r.getExistingResources(ctx, log); err != nil {
		return nil, fmt.Errorf("error getting existing resources: %w", err)
	}

	return r, nil
}
