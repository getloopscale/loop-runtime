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

package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/getloopscale/loopzero/pkg/statemachine"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	loopzerov1 "github.com/getloopscale/loopzero/operators/api/v1"
	"github.com/getloopscale/loopzero/operators/internal/conditions"
	"github.com/getloopscale/loopzero/operators/internal/metrics"
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
	// The base snapshot, taken before any .Status mutation below.
	//
	// The caller owns the Get, so no finalizer write precedes this point and the
	// ordering constraint that shapes the controllers does not apply. (Worth
	// noting that this function has no production caller at all — grep finds only
	// tests — so the contract it reasons about is not exercised today.) What DOES
	// apply is the other half: a snapshot taken after a mutation would diff that
	// field against itself, and the patch would silently omit it.
	//
	// The re-fetch below is left in place but NOT because the object goes stale —
	// see its own comment (B-085).
	base := app.DeepCopy()

	machine := statemachine.NewApplicationTypedMachine()
	current := statemachine.ApplicationPhase(app.Status.Phase)
	if current == statemachine.APhEmpty {
		current = statemachine.APhProgressing
	}
	next, err := machine.TryTransition(current, statemachine.APEventOrphaned)
	if err != nil {
		return fmt.Errorf("MarkAsCleaned: invalid transition %s→Cleaned for %s: %w",
			current, app.Name, err)
	}

	// 2. Update phase + condition via status sub-resource.
	// Audit gap M03 #15 fix: use the canonical conditions.SetCondition
	// helper instead of the local appendOrReplaceCondition (deleted). The
	// canonical helper preserves LastTransitionTime when status is unchanged
	// across reconciles, which matters for kstatus correctness — the legacy
	// local helper always overwrote the whole Condition, losing transition
	// history every time the reaper re-ran.
	app.Status.Phase = loopzerov1.ApplicationPhase(next)
	conditions.SetCondition(
		&app.Status.Conditions,
		"Cleaned",
		metav1.ConditionTrue,
		"OrphanedByGitOpsRemoval",
		reason,
		app.Generation,
	)
	// The readiness summary is the LAST status mutation before any persist
	// (ADR-0025 D3). Without it this write lands a terminal Phase=Cleaned next to
	// a Ready condition computed on an earlier pass — the same shape as the defect
	// B-105 fixed one file over, and the reason the guard now sweeps the package
	// instead of a single file.
	//
	// The function has no production caller (see the note above), so this is not a
	// live bug being fixed; it is the invariant being made true everywhere it is
	// claimed, so the guard does not need an exemption nobody would revisit.
	conditions.SetReadinessSummaryViaPkg(&app.Status.Conditions, app.Generation, applicationDetailTypes)
	if err := c.Status().Patch(ctx, app, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("MarkAsCleaned: status patch: %w", err)
	}

	// T1.5 of deep-review-2026-05-10-remediation-plan (find_reap_409 HIGH):
	// re-fetch before the finalizer Update.
	//
	// CORRECTION, measured 2026-08-14 (B-085): the stated reason is wrong, and it
	// was wrong before this slice too. client.Status().Patch and Status().Update
	// BOTH decode the response back into the caller's object
	// (controller-runtime typed_client.go:274 and :305, `.Into(body)`), so `app`
	// carries the fresh ResourceVersion the moment the write returns. Probed
	// against a client: 999 before, 1000 in memory AND on the server after.
	//
	// So this Get is conservative, not necessary — one redundant API call per
	// reap. Left in place because removing it is a behaviour change that belongs
	// in a slice that can exercise this function, and it has no production caller
	// to exercise. The sibling environment controller already patches status and
	// removes its finalizer with no re-fetch between them
	// (environment_controller.go), which is the shape this could take.
	if err := c.Get(ctx, client.ObjectKeyFromObject(app), app); err != nil {
		return fmt.Errorf("MarkAsCleaned: re-fetch after status update: %w", err)
	}

	// 2. Remove finalizers so the object can be GC'd. Cleaned CRs intentionally
	//    don't keep finalizers — the gitops source is the SoT now.
	if len(app.Finalizers) > 0 {
		app.Finalizers = nil
		if err := c.Update(ctx, app); err != nil {
			return fmt.Errorf("MarkAsCleaned: finalizer removal: %w", err)
		}
	}

	// ADR-0027 D5 / plan T5.2 — emit L4 "Deep Insights" metrics.
	// Per ADR-0027 capability matrix: this transition IS the Application
	// controller's L5 evidence (in-process self-heal without operator
	// action). Counters surface the rate operationally.
	metrics.SelfHealTotal.WithLabelValues("application", "reaper_cleaned").Inc()
	metrics.OrphanCRTotal.WithLabelValues("LoopZeroApplication").Inc()

	return nil
}

// appendOrReplaceCondition was removed in favor of the canonical helper
// `operators/internal/conditions.SetCondition` (audit gap M03 #15). The
// legacy local helper duplicated logic, always overwrote LastTransitionTime,
// and prevented kstatus from reasoning about transition stability.
