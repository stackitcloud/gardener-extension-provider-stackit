// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infrastructure

import (
	"context"
	"fmt"
	"sync"
	"time"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/infrastructure"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
)

var (
	testAttemptsLock sync.Mutex
	testAttempts     = make(map[string]int)
	testLastAttempt  = make(map[string]time.Time)
)

type actuator struct {
	stackitActuator   infrastructure.Actuator
	openstackActuator infrastructure.Actuator
}

// NewActuator creates a new Actuator that updates the status of the handled Infrastructure resources.
func NewActuator(mgr manager.Manager, customLabelDomain string) infrastructure.Actuator {
	return &actuator{
		stackitActuator:   stackit.NewActuator(mgr, customLabelDomain),
		openstackActuator: openstack.NewActuator(mgr),
	}
}

// Reconcile the Infrastructure config.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	// ================= DUMMY TEST INJECTION =================
	infraKey := infra.Namespace + "/" + infra.Name

	testAttemptsLock.Lock()
	now := time.Now()
	attempt := testAttempts[infraKey] + 1
	testAttempts[infraKey] = attempt
	lastTime := testLastAttempt[infraKey]
	testLastAttempt[infraKey] = now
	testAttemptsLock.Unlock()

	if !lastTime.IsZero() {
		elapsed := now.Sub(lastTime).Round(time.Second)
		log.Info(">>> [TEST BACKOFF] Reconcile attempt",
			"infra", infraKey,
			"attempt", attempt,
			"elapsedSinceLastAttempt", elapsed.String())
	} else {
		log.Info(">>> [TEST BACKOFF] First reconcile attempt received",
			"infra", infraKey,
			"attempt", attempt)
	}

	// Fail the first 2 attempts to demonstrate the 1-minute and 2-minute delays in the logs,
	// then succeed on attempt 3 so the shoot continues without staying broken.
	if attempt <= 2 {
		log.Info(">>> [TEST BACKOFF] Simulating timeout error to trigger backoff delay",
			"infra", infraKey,
			"attempt", attempt,
			"nextRetryInApprox", (time.Duration(1<<(attempt-1)) * time.Minute).String())
		return fmt.Errorf("simulated timeout error for backoff test (attempt %d/2)", attempt)
	}

	log.Info(">>> [TEST BACKOFF] Simulation complete! Proceeding with real reconciliation",
		"infra", infraKey)
	// =========================================================

	if feature.UseStackitAPIInfrastructureController(cluster) {
		return a.stackitActuator.Reconcile(ctx, log, infra, cluster)
	}
	return a.openstackActuator.Reconcile(ctx, log, infra, cluster)
}

// Delete the Infrastructure config.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	if feature.UseStackitAPIInfrastructureController(cluster) {
		return a.stackitActuator.Delete(ctx, log, infra, cluster)
	}
	return a.openstackActuator.Delete(ctx, log, infra, cluster)
}

// ForceDelete forcefully deletes the Infrastructure.
func (a *actuator) ForceDelete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	if feature.UseStackitAPIInfrastructureController(cluster) {
		return a.stackitActuator.ForceDelete(ctx, log, infra, cluster)
	}
	return a.openstackActuator.ForceDelete(ctx, log, infra, cluster)
}

// Migrate deletes the k8s infrastructure resources without deleting the corresponding resources in the IaaS provider.
func (a *actuator) Migrate(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	if feature.UseStackitAPIInfrastructureController(cluster) {
		return a.stackitActuator.Migrate(ctx, log, infra, cluster)
	}
	return a.openstackActuator.Migrate(ctx, log, infra, cluster)
}

// Restore implements infrastructure.Actuator.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure, cluster *extensionscontroller.Cluster) error {
	if feature.UseStackitAPIInfrastructureController(cluster) {
		return a.stackitActuator.Restore(ctx, log, infra, cluster)
	}
	return a.openstackActuator.Restore(ctx, log, infra, cluster)
}
