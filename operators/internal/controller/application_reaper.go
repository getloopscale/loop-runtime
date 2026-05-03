// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// fix-all-platform-issues-plan T2.2 — Application reaper logic.
//
// Detects orphaned LoopZeroApplication CRs whose GitOps source no longer contains
// their manifest (typically after a "Remove project from production" commit
// to the tenant's gitops repo). Without the reaper, such CRs persist
// indefinitely in phase=Progressing, polluting list/audit queries.
//
// Detection rule (per ADR D3):
//   1. Phase == Progressing
//   2. age(CR) > gracePeriod (default 1h, configurable via LOOPZERO_REAPER_GRACE_PERIOD_SECONDS)
//   3. The expected manifest path in the tenant's gitops repo is ABSENT in
//      the latest commit
//
// Action: transition phase → Cleaned + remove finalizer + record audit reason.
//
// The reaper is invoked from the existing application controller's reconcile
// path; no separate goroutine. Each reconcile evaluates ShouldReap() before
// performing the normal Progressing→Serving transition logic.

package controller

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/loopzero/loopzero/pkg/statemachine"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

// DefaultReaperGracePeriod is the minimum age of a Progressing CR before the
// reaper considers it for cleanup. Conservative; can be tuned via env.
const DefaultReaperGracePeriod = 1 * time.Hour

// ReaperGracePeriod returns the configured grace period (seconds via env, or default).
func ReaperGracePeriod() time.Duration {
	v := os.Getenv("LOOPZERO_REAPER_GRACE_PERIOD_SECONDS")
	if v == "" {
		return DefaultReaperGracePeriod
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return DefaultReaperGracePeriod
	}
	return time.Duration(secs) * time.Second
}

// ManifestChecker abstracts the gitops repo lookup. Production uses the
// existing gitops client; tests inject a fake.
type ManifestChecker interface {
	// ManifestExists returns true if the manifest at <env>/<project>.yaml
	// exists in the tenant's gitops repo's latest commit on main.
	ManifestExists(ctx context.Context, tenantRef, project, environment string) (bool, error)
}

// ShouldReap returns true if the application is an orphan eligible for cleanup.
// Pure function — no side effects; testable without a live cluster.
//
// Reasons NOT to reap (returns false + reason):
//   - phase != Progressing (already terminal or pre-active state)
//   - age < gracePeriod (give the normal reconcile path a chance)
//   - manifest still in gitops (legitimate Progressing — wait for Serving)
//   - already Cleaned (idempotent — don't transition twice)
//   - manifest check error (avoid false-positive on transient gitops failures)
func ShouldReap(ctx context.Context, app *loopzerov1.LoopZeroApplication, checker ManifestChecker, gracePeriod time.Duration, now time.Time) (bool, string) {
	if app == nil {
		return false, "nil application"
	}
	if app.Status.Phase == loopzerov1.ApplicationPhaseCleaned {
		return false, "already Cleaned (terminal)"
	}
	if app.Status.Phase != loopzerov1.ApplicationPhaseProgressing {
		return false, fmt.Sprintf("phase %q is not Progressing", app.Status.Phase)
	}
	age := now.Sub(app.CreationTimestamp.Time)
	if age < gracePeriod {
		return false, fmt.Sprintf("age %s < grace period %s", age.Truncate(time.Second), gracePeriod)
	}
	if checker == nil {
		// Without a checker, the reaper cannot prove the manifest is gone.
		// Fail closed: do not reap.
		return false, "no manifest checker configured"
	}
	exists, err := checker.ManifestExists(ctx, app.Spec.TenantRef, app.Spec.Project, app.Spec.Environment)
	if err != nil {
		// Transient gitops error — do not reap; will be re-evaluated next reconcile.
		return false, fmt.Sprintf("manifest check error (transient): %v", err)
	}
	if exists {
		return false, "manifest still present in gitops"
	}
	return true, "orphan (manifest absent in gitops + age > grace)"
}

// MarkAsCleaned transitions the CR's phase to Cleaned, removes the finalizer
// (so kube-apiserver can GC the object), and updates the status. Idempotent —
// no-op if already Cleaned. Preserves the CR record for audit; the operator
// can choose to delete the CR later via list-and-delete.
//
// Uses a status sub-resource update + a metadata update for finalizer removal,
// in two separate calls to satisfy the controller-runtime semantics.
func MarkAsCleaned(ctx context.Context, c client.Client, app *loopzerov1.LoopZeroApplication, reason string) error {
	if app == nil {
		return fmt.Errorf("MarkAsCleaned: nil application")
	}
	if app.Status.Phase == loopzerov1.ApplicationPhaseCleaned {
		return nil // idempotent
	}

	// 1. Validate transition via state machine (CLAUDE.md Rule 8).
	// Compliance review fix 2026-05-03 — was direct assignment, now goes
	// through ApplicationOrphaned event registered in pkg/statemachine/
	// application.go. Empty/unknown phase from older CRs is normalized
	// via Empty→Progressing first, so reaper still fires on legacy data.
	machine := statemachine.NewApplicationMachine()
	current := statemachine.State(app.Status.Phase)
	if current == statemachine.ApplicationEmpty {
		current = statemachine.ApplicationProgressing
	}
	next, err := machine.TryTransition(current, statemachine.ApplicationOrphaned)
	if err != nil {
		return fmt.Errorf("MarkAsCleaned: invalid transition %s→Cleaned for %s: %w",
			current, app.Name, err)
	}

	// 2. Update phase + condition via status sub-resource.
	app.Status.Phase = loopzerov1.ApplicationPhase(next)
	cond := metav1.Condition{
		Type:               "Cleaned",
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             "OrphanedByGitOpsRemoval",
		Message:            reason,
		ObservedGeneration: app.Generation,
	}
	app.Status.Conditions = appendOrReplaceCondition(app.Status.Conditions, cond)
	if err := c.Status().Update(ctx, app); err != nil {
		return fmt.Errorf("MarkAsCleaned: status update: %w", err)
	}

	// 2. Remove finalizers so the object can be GC'd. Cleaned CRs intentionally
	//    don't keep finalizers — the gitops source is the SoT now.
	if len(app.Finalizers) > 0 {
		app.Finalizers = nil
		if err := c.Update(ctx, app); err != nil {
			return fmt.Errorf("MarkAsCleaned: finalizer removal: %w", err)
		}
	}
	// Defensive: also call RemoveFinalizer for any specific operator finalizer.
	controllerutil.RemoveFinalizer(app, "loopzero.io/application-cleanup")
	return nil
}

func appendOrReplaceCondition(conditions []metav1.Condition, newCond metav1.Condition) []metav1.Condition {
	for i, c := range conditions {
		if c.Type == newCond.Type {
			conditions[i] = newCond
			return conditions
		}
	}
	return append(conditions, newCond)
}
