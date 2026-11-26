// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package placement is the tenant-placement feature. It decides a NEW tenant's
// cell, once, at bootstrap (RoundRobinEngine), resolves a cell id to its
// endpoints on every request (DynamicRouter), and holds the operations an
// operator performs on a tenant that already has a cell: pinning it to a cell
// (CellPinner — the only path that moves a tenant), reporting on cells
// (CellService) and evicting tenants (TenantEvictor). The operator endpoints
// live in placementhttp.
//
// Until api/internal was organized by feature (2026-09-25) this was two
// packages sharing one name, split by moment — deciding a cell versus acting on
// a tenant that has one — which put one feature under two paths.
//
// CellPinner (CHG-2026-04-30-INFRA-NORMALIZE, Phase 5 T5.5) pins a tenant to a
// cell after the store validates that the tenant exists, the cell exists and is
// 'active', and cells.capacity_tenants_max is not exceeded. The store runs those
// checks and the UPDATE as separate statements, not in one SERIALIZABLE
// transaction (db/store/cell_wrapper.go): two concurrent pins can both pass the
// capacity check for the last slot. Admin pins are operator-driven and rare,
// which is the trade-off that file records.
package placement

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	loopruntimeerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// TenantCRPatcher moves a tenant's Tenant CR to a cell (spec.cellID).
// adapters.TenantCRCellPatcher implements it against the mgmt cluster.
type TenantCRPatcher interface {
	PatchTenantCellID(ctx context.Context, tenantID, cellID string) error
	// Namespace is where the Tenant CRs live, quoted in the repair hint.
	Namespace() string
}

// CellPinnerStore is the persistence interface CellPinner depends on,
// implemented by store.PostgresCellStore (db/store/cell_wrapper.go).
type CellPinnerStore interface {
	// PinTenantToCell validates and moves the tenant, as described in the
	// package doc. Returns the previous cell_id, or one of:
	//   ErrTenantNotFound
	//   ErrCellNotFound
	//   ErrCellNotPinnable  (status != active)
	//   ErrCellAtCapacity   (capacity_tenants_max reached)
	PinTenantToCell(ctx context.Context, tenantID, cellID string) (previousCell string, err error)
}

// Sentinel errors for a pin the store refused. They ARE the store's values, not
// copies: the admin handler (routes/admin/admin_pin_cell.go) maps them to HTTP
// statuses with errors.Is, and the production store (store.PostgresCellStore)
// returns store.ErrCellPinner*. While this package declared its own values with
// the same text, errors.Is could not cross them and every refusal of a real pin
// was reported as an internal error.
//
// ErrCellAtTenantCapacity (note the longer name) disambiguates the
// tenant-population limit from the build-admission limit also called
// "cell at capacity" in services/build/admission.go.
var (
	ErrTenantNotFound       = store.ErrCellPinnerTenantNotFound
	ErrCellNotFound         = store.ErrCellPinnerCellNotFound
	ErrCellNotPinnable      = store.ErrCellPinnerNotPinnable
	ErrCellAtTenantCapacity = store.ErrCellPinnerAtCapacity
)

// ErrCellPinNotApplied means the Tenant CR could not be patched and the database
// move was reverted: the tenant did not move, and the pin may be retried.
var ErrCellPinNotApplied = &loopruntimeerrors.LoopRuntimeError{
	Code:    "CELL_PIN_NOT_APPLIED",
	Message: "tenant not moved: the Tenant CR could not be updated, so the database change was reverted",
}

// ErrCellPinDiverged is the partial failure: the database records the new cell,
// the Tenant CR still names the old one, and the revert was impossible or failed.
// The operator keeps reconciling the previous cell until the CR is patched.
var ErrCellPinDiverged = &loopruntimeerrors.LoopRuntimeError{
	Code:    "CELL_PIN_DIVERGED",
	Message: "tenant partially moved: the database records the new cell but the Tenant CR does not",
}

// CellPinner is the routes.CellPinner consumer (see admin_pin_cell.go).
// It's a thin orchestration over CellPinnerStore so the route layer
// doesn't depend on db internals.
//
// CHG-2026-04-30-DELETE-SINGLE-CELL: in addition to updating the DB
// (single-source for placement state), the pinner ALSO patches
// tenant.spec.cellID on the Tenant CR so the operator's next reconcile
// re-emits ArgoCD Application + AppProject with the new destination.
// Without the CR patch, the DB and the K8s state diverge and the
// operator continues writing the previous cell.
type CellPinner struct {
	store CellPinnerStore
	// crPatcher is optional: when nil, the pin is database-only.
	crPatcher TenantCRPatcher
	// patchBackoff is the wait before CR patch attempt n (n >= 1). Injected so
	// tests of the failure paths run without real sleeps.
	patchBackoff func(attempt int) time.Duration
}

// NewCellPinner constructs a database-only pinner. The production wireup
// (api/cmd/setup_server.go) adds a TenantCRPatcher so the CR patch happens on
// every successful pin.
func NewCellPinner(store CellPinnerStore) *CellPinner {
	return &CellPinner{store: store, patchBackoff: crPatchBackoff}
}

// WithTenantCRPatcher returns a copy of the pinner that also patches the
// Tenant CR after a successful database update. A nil patcher leaves the copy
// database-only.
func (p *CellPinner) WithTenantCRPatcher(patcher TenantCRPatcher) *CellPinner {
	cp := *p
	cp.crPatcher = patcher
	return &cp
}

// Pin satisfies the routes.CellPinner interface from admin_pin_cell.go.
//
// CLAUDE.md makes this the only path that moves a tenant, and requires the
// database update and the Tenant CR patch to land together. Flow:
//  1. Update the database via CellPinnerStore (validates tenant, cell, capacity).
//  2. If a TenantCRPatcher is wired, merge-patch the Tenant CR spec.cellID, retrying
//     transient apiserver failures.
//  3. If the patch still fails, revert the database to the previous cell and
//     return ErrCellPinNotApplied: nothing moved, and the caller may retry.
//     If the revert is impossible (no previous cell) or fails, return
//     ErrCellPinDiverged with the repair hint — the database and the CR now
//     disagree, and reporting success would hide exactly that.
//
// The previous cell_id is returned so callers can report "moved from X to Y".
func (p *CellPinner) Pin(ctx context.Context, tenantID, cellID string) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cell pinner: store not configured")
	}
	prev, err := p.store.PinTenantToCell(ctx, tenantID, cellID)
	if err != nil {
		return "", err
	}
	if p.crPatcher == nil {
		return prev, nil
	}
	patchErr := p.patchTenantCRWithRetry(ctx, tenantID, cellID)
	if patchErr == nil {
		return prev, nil
	}
	metrics.CellPinnerCRPatchFailTotal.Inc()
	return prev, p.revertAfterFailedPatch(ctx, tenantID, prev, cellID, patchErr)
}

// patchTenantCRWithRetry patches the Tenant CR up to three times (T1.5 + T1.5b
// of deep-review-remediation-plan): most patch failures are transient apiserver
// elections or network blips. It returns the last error, or ctx's error when the
// context ends during a backoff.
func (p *CellPinner) patchTenantCRWithRetry(ctx context.Context, tenantID, cellID string) error {
	var patchErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(p.patchBackoff(attempt)):
			}
		}
		if patchErr = p.crPatcher.PatchTenantCellID(ctx, tenantID, cellID); patchErr == nil {
			return nil
		}
	}
	return patchErr
}

// revertAfterFailedPatch restores the database to prevCell after the CR patch
// failed, so the move is all-or-nothing. It runs on a context detached from the
// caller's cancellation: the caller's deadline may be what failed the patch,
// and abandoning the revert for the same reason would leave the divergence the
// revert exists to prevent.
func (p *CellPinner) revertAfterFailedPatch(ctx context.Context, tenantID, prevCell, cellID string, patchErr error) error {
	if prevCell == "" || prevCell == cellID {
		return fmt.Errorf("%w: tenant %s is on %s in the database but the Tenant CR could not be patched (%w); %s",
			ErrCellPinDiverged, tenantID, cellID, patchErr, repairHint(p.crPatcher.Namespace(), tenantID, cellID))
	}
	revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, revertErr := p.store.PinTenantToCell(revertCtx, tenantID, prevCell); revertErr != nil {
		slog.Error("cell pinner: Tenant CR patch failed and the database could not be reverted",
			"tenant", tenantID, "new_cell", cellID, "previous_cell", prevCell,
			"patch_error", patchErr, "revert_error", revertErr)
		return fmt.Errorf("%w: tenant %s is on %s in the database but the Tenant CR could not be patched (%w), and reverting to %s failed (%w); %s",
			ErrCellPinDiverged, tenantID, cellID, patchErr, prevCell, revertErr, repairHint(p.crPatcher.Namespace(), tenantID, cellID))
	}
	slog.Warn("cell pinner: Tenant CR patch failed; database reverted, tenant not moved",
		"tenant", tenantID, "new_cell", cellID, "previous_cell", prevCell, "error", patchErr)
	return fmt.Errorf("%w: tenant %s stays on %s: %w", ErrCellPinNotApplied, tenantID, prevCell, patchErr)
}

// crPatchBackoff is the production wait before patch attempt n: 400ms, 1.6s.
func crPatchBackoff(attempt int) time.Duration {
	return time.Duration(100*intPow(4, attempt)) * time.Millisecond
}

// intPow computes base^exp for non-negative integers. KISS — only used by
// the retry backoff above. Using math.Pow would force float64 conversion.
func intPow(base, exp int) int {
	r := 1
	for i := 0; i < exp; i++ {
		r *= base
	}
	return r
}

// repairHint is the operator guidance attached to ErrCellPinDiverged, when the
// database already records the new cell and the Tenant CR is the stale side.
//
// It does NOT say "run `loopruntime admin tenants pin`" as if that were a new idea:
// this function IS that path, and it just failed. What unblocks it is re-running
// the pin, so the patch is retried under the loopruntime-api identity. The direct
// `kubectl patch` is quoted for the identities allowed to use it, and the
// warning that everyone else is denied is the point: since 2026-08-13 the
// ValidatingAdmissionPolicy `loopruntime-tenant-cr-shape` makes spec.cellID immutable
// except for system:masters and the loopruntime-api ServiceAccount, and a bare
// `kubectl patch` from an unprivileged kubeconfig gets `Denied` with no reason
// given (B-097).
func repairHint(ns, tenantID, cellID string) string {
	return fmt.Sprintf(
		"the database already records the new cell; the Tenant CR is the stale side. "+
			"Re-run the pin so the patch is retried under the loopruntime-api identity. "+
			"The direct patch `kubectl -n %s patch tenant %s --type=merge -p '{\"spec\":{\"cellID\":%q}}'` "+
			"is DENIED by the loopruntime-tenant-cr-shape admission policy (spec.cellID is immutable) "+
			"for every identity except system:masters and the loopruntime-api ServiceAccount",
		ns, tenantID, cellID)
}
