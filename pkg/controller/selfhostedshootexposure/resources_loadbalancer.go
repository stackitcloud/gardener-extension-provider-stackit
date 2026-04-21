package selfhostedshootexposure

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	reconcilerutils "github.com/gardener/gardener/pkg/controllerutils/reconciler"
	"github.com/go-logr/logr"
	loadbalancer "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api"
	loadbalancerwait "github.com/stackitcloud/stackit-sdk-go/services/loadbalancer/v2api/wait"
	corev1 "k8s.io/api/core/v1"

	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

const (
	// lbNetworkRoleListenersAndTargets is the network role for listeners and targets in the load balancer network.
	lbNetworkRoleListenersAndTargets = "ROLE_LISTENERS_AND_TARGETS"
	// protocolTCP is the TCP protocol identifier for the listener.
	protocolTCP = "PROTOCOL_TCP"
	// listenerName is the (single) hardcoded listener name for exposing the control plane API server.
	listenerName = "listener-control-plane"
	// targetPoolName is the (single) hardcoded target pool name for control plane nodes.
	targetPoolName = "target-pool-control-plane"
)

// STACKIT LB error types reported in LoadBalancer.Errors[].Type. v2api weakened Type from a
// generated enum to *string (known openapi-generator limitation confirmed with the LB team);
// the authoritative set of values still lives as LOADBALANCERERRORTYPE_* constants in the
// deprecated top-level stackit-sdk-go/services/loadbalancer package.
const (
	// lbErrTypeTargetNotActive encodes that target may not be ready (yet).
	lbErrTypeTargetNotActive = "TYPE_TARGET_NOT_ACTIVE"
)

func (r *Resources) reconcileLoadBalancer(ctx context.Context, log logr.Logger) error {
	targets, err := r.buildTargets()
	if err != nil {
		return err
	}

	if r.LoadBalancer == nil {
		return r.createLoadBalancer(ctx, log, targets)
	}

	targetPoolNeedsUpdate, err := r.targetPoolNeedsUpdate(targets)
	if err != nil {
		return err
	}
	fullStateNeedsUpdate := r.planNeedsUpdate() || r.accessControlNeedsUpdate()

	if !targetPoolNeedsUpdate && !fullStateNeedsUpdate {
		return nil
	}

	// Fast path: only targets changed (e.g. control-plane node added/removed). The sub-resource
	// endpoint is scoped to the target pool, so we avoid re-sending the full LB state.
	if targetPoolNeedsUpdate && !fullStateNeedsUpdate {
		return r.updateTargetPool(ctx, log, targets)
	}

	// Full-state update: STACKIT's UpdateLoadBalancer (PUT endpoint)
	return r.updateLoadBalancer(ctx, log, targets)
}

func (r *Resources) createLoadBalancer(ctx context.Context, log logr.Logger, targets []loadbalancer.Target) error {
	if len(targets) == 0 {
		// Endpoints are populated asynchronously by gardenlet from healthy control-plane nodes.
		// Empty endpoints on first create is a normal transient state.
		return &reconcilerutils.RequeueAfterError{
			RequeueAfter: 30 * time.Second,
			Cause:        fmt.Errorf("waiting for endpoints to be populated in spec"),
		}
	}

	log.V(1).Info("Creating load balancer", "loadBalancer", r.ResourceName, "networkID", r.NetworkID, "planID", r.PlanId)
	createdLB, err := r.LBClient.CreateLoadBalancer(ctx, loadbalancer.CreateLoadBalancerPayload{
		Name:        &r.ResourceName,
		Labels:      &r.Labels,
		Networks:    r.desiredNetworks(),
		Listeners:   r.desiredListeners(),
		TargetPools: r.desiredTargetPools(targets),
		PlanId:      &r.PlanId,
		Options:     r.desiredOptions(),
	})
	if err != nil {
		return wrapLBAPIError("creating load balancer", err)
	}

	r.LoadBalancer = createdLB
	log.Info("Created load balancer", "loadBalancer", r.ResourceName)
	return nil
}

func (r *Resources) updateTargetPool(ctx context.Context, log logr.Logger, targets []loadbalancer.Target) error {
	log.Info("Target pool needs updating", "loadBalancer", r.ResourceName)
	_, err := r.LBClient.UpdateLoadBalancerTargetPool(ctx,
		r.ResourceName,
		targetPoolName,
		loadbalancer.UpdateTargetPoolPayload{
			Name:       new(targetPoolName),
			TargetPort: &r.SelfHostedShootExposure.Spec.Port,
			Targets:    targets,
		})
	if err != nil {
		return wrapLBAPIError("updating load balancer target pool", err)
	}
	log.Info("Updated load balancer target pool", "loadBalancer", r.ResourceName)

	// Re-read the LB so downstream readiness checks don't see the pre-write status (STACKIT
	// transitions the LB to STATUS_PENDING on any write). UpdateLoadBalancerTargetPool only
	// returns the TargetPool, so a full GET is needed.
	refreshed, err := r.LBClient.GetLoadBalancer(ctx, r.ResourceName)
	if err != nil {
		return fmt.Errorf("error refreshing load balancer after target pool update: %w", err)
	}
	r.LoadBalancer = refreshed
	return nil
}

func (r *Resources) updateLoadBalancer(ctx context.Context, log logr.Logger, targets []loadbalancer.Target) error {
	log.Info("Load balancer needs updating", "loadBalancer", r.ResourceName, "newPlan", r.PlanId)

	// LB Endpoint only available as PUT, requires sending whole resource.
	payload := loadbalancer.UpdateLoadBalancerPayload{
		Name:            &r.ResourceName,
		Version:         r.LoadBalancer.Version,
		ExternalAddress: r.LoadBalancer.ExternalAddress,
		Labels:          &r.Labels,
		Networks:        r.desiredNetworks(),
		Listeners:       r.desiredListeners(),
		TargetPools:     r.desiredTargetPools(targets),
		PlanId:          &r.PlanId,
		Options:         r.desiredOptions(),
	}

	updated, err := r.LBClient.UpdateLoadBalancer(ctx, r.ResourceName, payload)
	if err != nil {
		return wrapLBAPIError("updating load balancer", err)
	}
	// Capture the post-write LB so readiness is evaluated against actual current status
	r.LoadBalancer = updated
	log.Info("Updated load balancer", "loadBalancer", r.ResourceName)
	return nil
}

// desiredNetworks returns the LB's network block. The control-plane exposure LB uses a single
// network acting as both listener and target network.
func (r *Resources) desiredNetworks() []loadbalancer.Network {
	return []loadbalancer.Network{{
		NetworkId: &r.NetworkID,
		Role:      new(lbNetworkRoleListenersAndTargets),
	}}
}

// desiredListeners returns the single TCP listener that fronts the kube-apiserver.
func (r *Resources) desiredListeners() []loadbalancer.Listener {
	return []loadbalancer.Listener{{
		DisplayName: new(listenerName),
		Port:        &r.SelfHostedShootExposure.Spec.Port,
		Protocol:    new(protocolTCP),
		TargetPool:  new(targetPoolName),
	}}
}

// desiredTargetPools returns the single target pool referenced by the listener.
func (r *Resources) desiredTargetPools(targets []loadbalancer.Target) []loadbalancer.TargetPool {
	return []loadbalancer.TargetPool{{
		Name:       new(targetPoolName),
		TargetPort: &r.SelfHostedShootExposure.Spec.Port,
		Targets:    targets,
	}}
}

// desiredOptions returns the LB options.
//
// Initial call: ephemeral (provides external IP)
// Subsequent calls (PUT updates): provide the initially provided IP, do no set ephemeral to true (error!).
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

func (r *Resources) planNeedsUpdate() bool {
	currentPlan := ""
	if r.LoadBalancer != nil && r.LoadBalancer.PlanId != nil {
		currentPlan = *r.LoadBalancer.PlanId
	}
	return currentPlan != r.PlanId
}

// accessControlNeedsUpdate reports whether the LB's currently configured source-IP allowlist
// differs from the desired set. The desired set is order-independent; we compare as sorted lists.
// An empty desired list means "no restriction" — detected by diff against whatever the LB reports.
func (r *Resources) accessControlNeedsUpdate() bool {
	var current []string
	if r.LoadBalancer != nil &&
		r.LoadBalancer.Options != nil &&
		r.LoadBalancer.Options.AccessControl != nil {
		current = r.LoadBalancer.Options.AccessControl.AllowedSourceRanges
	}
	return !stringSetsEqual(current, r.AllowedSourceRanges)
}

// stringSetsEqual compares two string slices as unordered sets, ignoring duplicates.
// The LB API may or may not de-duplicate (causing reconciliation loop), remove duplicates
// for a clean approach.
func stringSetsEqual(a, b []string) bool {
	return slices.Equal(sortedUnique(a), sortedUnique(b))
}

// sortedUnique returns the input as a sorted slice with consecutive duplicates removed,
// i.e. the canonical representation of the set.
func sortedUnique(s []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(s)))
}

// wrapLBAPIError classifies STACKIT LB API errors: 409 Conflicts are transient (another caller
// modified the LB between our GET and our write) and are retried via RequeueAfterError; anything
// else is returned as a regular error so Gardener can classify + surface it.
func wrapLBAPIError(op string, err error) error {
	if stackitclient.IsConflict(err) {
		return &reconcilerutils.RequeueAfterError{
			RequeueAfter: 15 * time.Second,
			Cause:        fmt.Errorf("load balancer is being modified while %s, retrying: %w", op, err),
		}
	}
	return fmt.Errorf("error %s: %w", op, err)
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
		switch *e.Type {
		case lbErrTypeTargetNotActive:
			// transient
		default:
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

	// Sort targets by IP (primary) and DisplayName (secondary) for deterministic ordering
	sort.Slice(targets, func(i, j int) bool {
		if *targets[i].Ip != *targets[j].Ip {
			return *targets[i].Ip < *targets[j].Ip
		}
		return *targets[i].DisplayName < *targets[j].DisplayName
	})

	return targets, nil
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

// targetsEqual compares two target lists for equality.
// Both lists should be sorted by IP for correct comparison.
func targetsEqual(spec, lb []loadbalancer.Target) bool {
	if len(spec) != len(lb) {
		return false
	}

	for i := range spec {
		if spec[i].Ip == nil || lb[i].Ip == nil {
			return false
		}
		if *spec[i].Ip != *lb[i].Ip {
			return false
		}
		// Also verify the display name matches
		if spec[i].DisplayName == nil || lb[i].DisplayName == nil {
			return false
		}
		if *spec[i].DisplayName != *lb[i].DisplayName {
			return false
		}
	}
	return true
}

// targetPoolNeedsUpdate checks if the target pool in the load balancer needs updating.
// specTargets should be pre-built to avoid double-building.
func (r *Resources) targetPoolNeedsUpdate(specTargets []loadbalancer.Target) (bool, error) {
	// If no load balancer exists yet, no update needed (will be created fresh)
	if r.LoadBalancer == nil {
		return false, nil
	}

	// If LB exists but has no target pools, check if spec has targets
	if len(r.LoadBalancer.TargetPools) == 0 {
		return len(specTargets) > 0, nil
	}

	// Validate that the load balancer has the expected target pool
	if r.LoadBalancer.TargetPools[0].Name == nil || *r.LoadBalancer.TargetPools[0].Name != targetPoolName {
		actualName := ""
		if r.LoadBalancer.TargetPools[0].Name != nil {
			actualName = *r.LoadBalancer.TargetPools[0].Name
		}
		return false, fmt.Errorf("unexpected target pool name: expected %q, got %q",
			targetPoolName, actualName)
	}

	// Get targets from the first target pool and copy before sorting
	lbTargets := make([]loadbalancer.Target, len(r.LoadBalancer.TargetPools[0].Targets))
	copy(lbTargets, r.LoadBalancer.TargetPools[0].Targets)

	// Sort the LB targets for comparison (same order as spec targets)
	sort.Slice(lbTargets, func(i, j int) bool {
		if *lbTargets[i].Ip != *lbTargets[j].Ip {
			return *lbTargets[i].Ip < *lbTargets[j].Ip
		}
		return *lbTargets[i].DisplayName < *lbTargets[j].DisplayName
	})

	// Compare semantically (order-independent after sorting)
	return !targetsEqual(specTargets, lbTargets), nil
}
