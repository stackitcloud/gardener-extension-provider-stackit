package selfhostedshootexposure

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	reconcilerutils "github.com/gardener/gardener/pkg/controllerutils/reconciler"
	"github.com/go-logr/logr"
	gocmp "github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	loadbalancersdk "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer" //nolint:staticcheck // SA1019: see TODO below — v2api lacks the typed enum constants we need.
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	loadbalancerwait "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api/wait"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
)

// TODO(jamand): drop the loadbalancersdk import once v2api re-exports the typed enum constants
// (NetworkRole, ListenerProtocol, LoadBalancerErrorTypes). v2api currently weakened these to
// *string (known openapi-generator limitation confirmed with the LB team); the authoritative
// values still live in the deprecated top-level stackit-sdk-go/services/loadbalancer package,
// scheduled for removal after 2026-09-30. We reference them here as the source of truth and
// convert to string at the call sites.
const (
	// listenerName is the (single) hardcoded listener name for exposing the control plane API server.
	listenerName = "control-plane"
	// targetPoolName is the (single) hardcoded target pool name for control plane nodes.
	targetPoolName = "control-plane"
)

func (r *Resources) reconcileLoadBalancer(ctx context.Context, log logr.Logger) error {
	targets, err := r.buildTargets()
	if err != nil {
		return fmt.Errorf("error building targets: %w", err)
	}

	if r.LoadBalancer == nil {
		return r.createLoadBalancer(ctx, log, targets)
	}

	if !r.loadBalancerNeedsUpdate(targets) {
		return nil
	}
	// STACKIT also exposes a partial target-pool update endpoint, but it returns only the
	// TargetPool — we'd still need a follow-up GET to refresh r.LoadBalancer for the readiness
	// check, so it costs an extra round-trip without a server-side latency win (STACKIT
	// transitions the LB to PENDING on either write). Use the full PUT instead.
	return r.updateLoadBalancer(ctx, log, targets)
}

func (r *Resources) createLoadBalancer(ctx context.Context, log logr.Logger, targets []loadbalancer.Target) error {
	createdLB, err := r.LBClient.CreateLoadBalancer(ctx, loadbalancer.CreateLoadBalancerPayload{
		Name:        &r.ResourceName,
		Labels:      &r.Labels,
		Networks:    r.desiredNetworks(),
		Listeners:   r.desiredListeners(),
		TargetPools: r.desiredTargetPools(targets),
		PlanId:      &r.PlanID,
		Options:     r.desiredOptions(),
	})
	if err != nil {
		return fmt.Errorf("error creating load balancer: %w", err)
	}

	r.LoadBalancer = createdLB
	log.Info("Created load balancer", "loadBalancer", r.ResourceName)
	return nil
}

func (r *Resources) updateLoadBalancer(ctx context.Context, log logr.Logger, targets []loadbalancer.Target) error {
	// STACKIT requires ExternalAddress to be set on PUT (and rejects EphemeralAddress=true once
	// the LB has a floating IP). If the LB hasn't been assigned an external address yet, return
	// an error so controller-runtime retries with backoff rather than 400ing the API.
	if r.LoadBalancer.ExternalAddress == nil {
		return fmt.Errorf("waiting for load balancer external address before updating")
	}

	// LB endpoint is PUT-only and requires sending the whole resource.
	updated, err := r.LBClient.UpdateLoadBalancer(ctx, r.ResourceName, loadbalancer.UpdateLoadBalancerPayload{
		Name:            &r.ResourceName,
		Version:         r.LoadBalancer.Version,
		ExternalAddress: r.LoadBalancer.ExternalAddress,
		Labels:          &r.Labels,
		Networks:        r.desiredNetworks(),
		Listeners:       r.desiredListeners(),
		TargetPools:     r.desiredTargetPools(targets),
		PlanId:          &r.PlanID,
		Options:         r.desiredOptions(),
	})
	if err != nil {
		return fmt.Errorf("error updating load balancer: %w", err)
	}
	r.LoadBalancer = updated
	log.Info("Updated load balancer", "loadBalancer", r.ResourceName)
	return nil
}

// desiredNetworks returns the LB's network block. The control-plane exposure LB uses a single
// network acting as both listener and target network.
func (r *Resources) desiredNetworks() []loadbalancer.Network {
	return []loadbalancer.Network{{
		NetworkId: &r.NetworkID,
		Role:      new(string(loadbalancersdk.NETWORKROLE_LISTENERS_AND_TARGETS)), //nolint:staticcheck // SA1019: see TODO at the top of the file.
	}}
}

// desiredListeners returns the single TCP listener that fronts the kube-apiserver.
func (r *Resources) desiredListeners() []loadbalancer.Listener {
	return []loadbalancer.Listener{{
		DisplayName: new(listenerName),
		Port:        new(r.SelfHostedShootExposure.Spec.Port),
		Protocol:    new(string(loadbalancersdk.LISTENERPROTOCOL_TCP)), //nolint:staticcheck // SA1019: see TODO at the top of the file.
		TargetPool:  new(targetPoolName),
	}}
}

// desiredTargetPools returns the single target pool referenced by the listener.
func (r *Resources) desiredTargetPools(targets []loadbalancer.Target) []loadbalancer.TargetPool {
	return []loadbalancer.TargetPool{{
		Name:       new(targetPoolName),
		TargetPort: new(r.SelfHostedShootExposure.Spec.Port),
		Targets:    targets,
	}}
}

// desiredOptions returns the LB options.
//
// Initial create: ask STACKIT to assign an ephemeral external IP.
//
// Subsequent PUT updates: do NOT set EphemeralAddress — STACKIT rejects PUTs with
// EphemeralAddress=true once the LB has been assigned a floating IP ("Ephemeral address is
// not supported for existing floating IPs"). The constraint that one of ExternalAddress,
// EphemeralAddress=true, or PrivateNetworkOnly=true must be set is satisfied via
// UpdateLoadBalancerPayload.ExternalAddress, which the caller passes through from the
// existing LB. updateLoadBalancer guards against the LB not yet having an ExternalAddress.
func (r *Resources) desiredOptions() *loadbalancer.LoadBalancerOptions {
	opts := &loadbalancer.LoadBalancerOptions{}
	if r.LoadBalancer == nil {
		opts.EphemeralAddress = new(true)
	}
	if len(r.AllowedSourceRanges) > 0 {
		opts.AccessControl = &loadbalancer.LoadbalancerOptionAccessControl{
			AllowedSourceRanges: r.AllowedSourceRanges,
		}
	}
	return opts
}

// loadBalancerNeedsUpdate reports whether any controller-managed field of the LB (targets,
// plan, ACL) differs from the desired state. Caller must guarantee r.LoadBalancer is non-nil.
func (r *Resources) loadBalancerNeedsUpdate(specTargets []loadbalancer.Target) bool {
	return r.targetsNeedUpdate(specTargets) || r.planNeedsUpdate() || r.accessControlNeedsUpdate()
}

// planNeedsUpdate checks for an existing LB if its current plan needs to be updated.
func (r *Resources) planNeedsUpdate() bool {
	return ptr.Deref(r.LoadBalancer.PlanId, "") != r.PlanID
}

// accessControlNeedsUpdate reports whether the LB's currently configured source-IP allowlist
// differs from the desired set. Comparison is order-independent and ignores duplicates: the LB
// API may or may not de-duplicate, so set semantics avoid spurious update churn. An empty
// desired list means "no restriction" — detected by diff against whatever the LB reports.
func (r *Resources) accessControlNeedsUpdate() bool {
	var current []string
	if r.LoadBalancer.Options != nil && r.LoadBalancer.Options.AccessControl != nil {
		current = r.LoadBalancer.Options.AccessControl.AllowedSourceRanges
	}
	return !sets.New(current...).Equal(sets.New(r.AllowedSourceRanges...))
}

// checkLoadBalancerReady returns nil only if the LB is fully provisioned (STATUS_READY with an
// external VIP), otherwise RequeueAfterError.
//
// STATUS_ERROR is ambiguous: it may be transient (e.g. control-plane nodes not yet serving
// traffic during fresh shoot creation, reported as TYPE_TARGET_NOT_ACTIVE) or permanent.
func (r *Resources) checkLoadBalancerReady(log logr.Logger) error {
	if r.LoadBalancer == nil || r.LoadBalancer.Status == nil {
		return &reconcilerutils.RequeueAfterError{
			RequeueAfter: 15 * time.Second,
			Cause:        fmt.Errorf("waiting for load balancer status to be reported"),
		}
	}

	switch *r.LoadBalancer.Status {
	case loadbalancerwait.LOADBALANCERSTATUS_READY:
		if r.LoadBalancer.ExternalAddress == nil {
			return &reconcilerutils.RequeueAfterError{
				RequeueAfter: 15 * time.Second,
				Cause:        fmt.Errorf("waiting for load balancer external address to be assigned"),
			}
		}
		return nil
	case loadbalancerwait.LOADBALANCERSTATUS_PENDING, loadbalancerwait.LOADBALANCERSTATUS_UNSPECIFIED:
		return &reconcilerutils.RequeueAfterError{
			RequeueAfter: 15 * time.Second,
			Cause:        fmt.Errorf("waiting for load balancer to become ready (status=%s)", *r.LoadBalancer.Status),
		}
	case loadbalancerwait.LOADBALANCERSTATUS_ERROR:
		if lbErrorsAllTransient(r.LoadBalancer.Errors) {
			log.Info("Load balancer reports STATUS_ERROR with only transient errors, requeuing",
				"loadBalancer", r.ResourceName, "errors", formatLBErrors(r.LoadBalancer.Errors))
			return &reconcilerutils.RequeueAfterError{
				RequeueAfter: 15 * time.Second,
				Cause:        fmt.Errorf("load balancer is in transient STATUS_ERROR: %s", formatLBErrors(r.LoadBalancer.Errors)),
			}
		}
		return fmt.Errorf("load balancer is in unrecoverable state %s: %s", *r.LoadBalancer.Status, formatLBErrors(r.LoadBalancer.Errors))
	case loadbalancerwait.LOADBALANCERSTATUS_TERMINATING:
		return fmt.Errorf("load balancer is in unrecoverable state %s: %s", *r.LoadBalancer.Status, formatLBErrors(r.LoadBalancer.Errors))
	default:
		return fmt.Errorf("load balancer has unexpected status %s: %s", *r.LoadBalancer.Status, formatLBErrors(r.LoadBalancer.Errors))
	}
}

// lbErrorsAllTransient reports whether every entry in errs is in the transient allowlist.
// An empty slice returns false: STATUS_ERROR without any diagnostics should not be silently
// swallowed as transient.
func lbErrorsAllTransient(errs []loadbalancer.LoadBalancerError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.Type == nil {
			return false
		}
		if *e.Type != string(loadbalancersdk.LOADBALANCERERRORTYPE_TARGET_NOT_ACTIVE) { //nolint:staticcheck // SA1019: see TODO at the top of the file.
			return false
		}
	}
	return true
}

// formatLBErrors renders the LB's reported errors for inclusion in an error message.
func formatLBErrors(errs []loadbalancer.LoadBalancerError) string {
	if len(errs) == 0 {
		return "no error details reported"
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		t, d := "", ""
		if e.Type != nil {
			t = *e.Type
		}
		if e.Description != nil {
			d = *e.Description
		}
		parts = append(parts, fmt.Sprintf("%s: %s", t, d))
	}
	return strings.Join(parts, "; ")
}

func (r *Resources) deleteLoadBalancer(ctx context.Context, log logr.Logger) error {
	if r.LoadBalancer == nil {
		return nil
	}

	if err := r.LBClient.DeleteLoadBalancer(ctx, r.ResourceName); err != nil {
		return fmt.Errorf("error deleting load balancer: %w", err)
	}

	log.Info("Deleted load balancer", "loadBalancer", r.ResourceName)
	return nil
}

// buildTargets creates a sorted list of load balancer targets from the endpoints in the spec.
// Targets are sorted by IP address for deterministic ordering.
func (r *Resources) buildTargets() ([]loadbalancer.Target, error) {
	targets := make([]loadbalancer.Target, len(r.SelfHostedShootExposure.Spec.Endpoints))
	for i, endpoint := range r.SelfHostedShootExposure.Spec.Endpoints {
		ip, err := extractInternalIP(&endpoint)
		if err != nil {
			return nil, err
		}
		targets[i] = loadbalancer.Target{
			DisplayName: &endpoint.NodeName,
			Ip:          &ip,
		}
	}
	sortTargets(targets)
	return targets, nil
}

// sortTargets sorts the given target slice in-place by IP (primary) and DisplayName (secondary).
func sortTargets(targets []loadbalancer.Target) {
	slices.SortFunc(targets, func(a, b loadbalancer.Target) int {
		return cmp.Or(
			cmp.Compare(*a.Ip, *b.Ip),
			cmp.Compare(*a.DisplayName, *b.DisplayName),
		)
	})
}

// extractInternalIP finds and returns the internal IP address from an endpoint's addresses.
// This function requires InternalIP because the STACKIT LB only supports IPs as target.
func extractInternalIP(endpoint *extensionsv1alpha1.ControlPlaneEndpoint) (string, error) {
	for _, addr := range endpoint.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("endpoint %s has no InternalIP address", endpoint.NodeName)
}

// targetsNeedUpdate compares the LB's current first target pool against the desired targets.
// If no target pool exists or its name doesn't match, signals an update (the full PUT will
// replace whatever is there with the desired single pool); empty current vs. empty desired is
// a no-op so we don't churn updates.
//
// Target.AdditionalProperties is excluded from the comparison: the SDK populates it from any
// JSON fields STACKIT echoes back that the SDK doesn't have a typed field for. We never set
// these on the desired side, so leaving them in would make every reconcile see a diff and PUT.
func (r *Resources) targetsNeedUpdate(specTargets []loadbalancer.Target) bool {
	var current []loadbalancer.Target
	if len(r.LoadBalancer.TargetPools) > 0 {
		if ptr.Deref(r.LoadBalancer.TargetPools[0].Name, "") != targetPoolName {
			return true
		}
		current = slices.Clone(r.LoadBalancer.TargetPools[0].Targets)
		sortTargets(current)
	}
	return !gocmp.Equal(specTargets, current, cmpopts.IgnoreFields(loadbalancer.Target{}, "AdditionalProperties"))
}
