// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package placement holds the cell-placement operations an operator performs on a
// tenant after it exists: pinning it to a cell (CellPinner), reporting on cells
// (CellService) and evicting tenants (TenantEvictor).
//
// It was named `placementsvc`, a suffix that existed only to avoid colliding with
// `api/internal/placement` (the one-time placement engine and the router).
// Measured 2026-08-19 (B-057): of the 21 files importing either package, exactly
// one imports both — the composition root, which now carries the alias.
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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// tenantGVR is the GroupVersionResource for the Tenant CR. Hardcoded
// here (instead of importing operators/api/v1) so this service has no
// operator-package dependency.
var tenantGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "tenants",
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

// Sentinel errors. The HTTP handler maps these to status codes via
// substring match (see admin_pin_cell.go); this package exposes the
// concrete values for unit tests against the service.
//
// T5.7 of deep-review-2026-05-10-remediation-plan
// (find_arch_routes_import_services MEDIUM) considered moving these to
// pkg/errors. Deferred per Princípio 3 (Honestidade Extrema): the
// canonical pkg/errors counterparts (`ErrTenantNotFound`, `ErrCellAtCapacity`)
// are `*LoopZeroError` types — not plain `error` — so a direct alias produces
// type conflicts at the existing call sites that wrap these with
// `fmt.Errorf("... %w", services.ErrTenantNotFound)`. The proper move
// requires either (a) converting all call sites to LoopZeroError or
// (b) renaming the pkg/errors versions. Both are larger refactors than
// T5.7's scope and risk regression of the existing admin_pin_cell error
// mapping. Tracked as future work; current cross-package dependency
// (routes → services) is legitimate top-down per T1.3 closure.
//
// ErrCellAtTenantCapacity (note the longer name) disambiguates the
// tenant-population limit from the build-admission limit also called
// "cell at capacity" in services/admission.go.
var (
	ErrTenantNotFound       = errors.New("tenant not found")
	ErrCellNotFound         = errors.New("cell not found")
	ErrCellNotPinnable      = errors.New("cell not pinnable")
	ErrCellAtTenantCapacity = errors.New("cell at capacity")
)

// ErrCellPinNotApplied means the Tenant CR could not be patched and the database
// move was reverted: the tenant did not move, and the pin may be retried.
var ErrCellPinNotApplied = &loopzeroerrors.LoopZeroError{
	Code:    "CELL_PIN_NOT_APPLIED",
	Message: "tenant not moved: the Tenant CR could not be updated, so the database change was reverted",
}

// ErrCellPinDiverged is the partial failure: the database records the new cell,
// the Tenant CR still names the old one, and the revert was impossible or failed.
// The operator keeps reconciling the previous cell until the CR is patched.
var ErrCellPinDiverged = &loopzeroerrors.LoopZeroError{
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
	store    CellPinnerStore
	mgmtKube dynamic.Interface // optional: when nil, CR patch is skipped
	mgmtNS   string            // namespace where Tenant CRs live (default "loopzero-system")
	// patchBackoff is the wait before CR patch attempt n (n >= 1). Injected so
	// tests of the failure paths run without real sleeps.
	patchBackoff func(attempt int) time.Duration
}

// NewCellPinner constructs a pinner with the given persistence backend.
// The mgmtKube + namespace can be nil/empty in tests; the production
// wireup (api/cmd/setup_server.go) passes a real dynamic client so the
// CR patch happens on every successful pin.
func NewCellPinner(store CellPinnerStore) *CellPinner {
	return &CellPinner{store: store, mgmtNS: "loopzero-system", patchBackoff: crPatchBackoff}
}

// WithKubeClient returns a copy of the pinner that also patches the
// Tenant CR after a successful DB update. Returns the receiver
// unchanged when client is nil — preserves test ergonomics.
func (p *CellPinner) WithKubeClient(client dynamic.Interface, namespace string) *CellPinner {
	cp := *p
	cp.mgmtKube = client
	if namespace != "" {
		cp.mgmtNS = namespace
	}
	return &cp
}

// Pin satisfies the routes.CellPinner interface from admin_pin_cell.go.
//
// CLAUDE.md makes this the only path that moves a tenant, and requires the
// database update and the Tenant CR patch to land together. Flow:
//  1. Update the database via CellPinnerStore (validates tenant, cell, capacity).
//  2. If a kube client is wired, merge-patch the Tenant CR spec.cellID, retrying
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
	if p.mgmtKube == nil {
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
		if patchErr = p.patchTenantCR(ctx, tenantID, cellID); patchErr == nil {
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
			ErrCellPinDiverged, tenantID, cellID, patchErr, repairHint(p.mgmtNS, tenantID, cellID))
	}
	revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, revertErr := p.store.PinTenantToCell(revertCtx, tenantID, prevCell); revertErr != nil {
		slog.Error("cell pinner: Tenant CR patch failed and the database could not be reverted",
			"tenant", tenantID, "new_cell", cellID, "previous_cell", prevCell,
			"patch_error", patchErr, "revert_error", revertErr)
		return fmt.Errorf("%w: tenant %s is on %s in the database but the Tenant CR could not be patched (%w), and reverting to %s failed (%w); %s",
			ErrCellPinDiverged, tenantID, cellID, patchErr, prevCell, revertErr, repairHint(p.mgmtNS, tenantID, cellID))
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

// patchTenantCR applies a strategic-merge patch to spec.cellID on the
// named Tenant CR. Idempotent — re-pinning to the same cell is a no-op
// from the operator's perspective.
func (p *CellPinner) patchTenantCR(ctx context.Context, tenantID, cellID string) error {
	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"cellID": cellID}})
	if err != nil {
		return fmt.Errorf("marshal patch: %w", err)
	}
	_, err = p.mgmtKube.Resource(tenantGVR).Namespace(p.mgmtNS).
		Patch(ctx, tenantID, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("patch tenant %s: %w", tenantID, err)
	}
	return nil
}

// pinTenantToCellSQL is the canonical query the store implementation
// runs in a SERIALIZABLE transaction. Exposed as a package-level
// constant so tests can reference it without coupling to pgx.
//
//revive:disable-next-line:unused-receiver
const pinTenantToCellSQL = `
WITH
-- 1) Tenant must exist.
target_tenant AS (
    SELECT id, cell_id AS prev_cell_id FROM tenants WHERE id = $1
),
-- 2) Cell must exist + be active. capacity_tenants_max is NULL = unlimited.
target_cell AS (
    SELECT cell_id, status, capacity_tenants_max FROM cells WHERE cell_id = $2
),
-- 3) Current tenant count on the target cell (excluding the moving tenant
--    so a re-pin to the same cell doesn't fail capacity off-by-one).
cell_population AS (
    SELECT COUNT(*) AS current_count FROM tenants
     WHERE cell_id = $2 AND id <> $1
)
UPDATE tenants
   SET cell_id = $2
 WHERE id = $1
   AND EXISTS (SELECT 1 FROM target_tenant)
   AND EXISTS (SELECT 1 FROM target_cell tc WHERE tc.status = 'active')
   AND (
       (SELECT capacity_tenants_max FROM target_cell) IS NULL
       OR (SELECT capacity_tenants_max FROM target_cell) >
          (SELECT current_count FROM cell_population)
   )
RETURNING (SELECT prev_cell_id FROM target_tenant);
`

// _ = pinTenantToCellSQL exists to prevent unused-import errors in tests
// that don't reference it. Concrete db implementation will use this
// SQL in db/cell_pinner_store.go (NEW file ships with the migration
// 000003_cells reconciler).
var _ = pinTenantToCellSQL

// _ Tx is a type-system check that the store-side will need pgx.Tx-like
// semantics. Documented here so any future driver swap doesn't drift.
type _ pgx.Tx

// repairHint is the operator guidance attached to ErrCellPinDiverged, when the
// database already records the new cell and the Tenant CR is the stale side.
//
// It does NOT say "run `loopzero admin tenants pin`" as if that were a new idea:
// this function IS that path, and it just failed. What unblocks it is re-running
// the pin, so the patch is retried under the loopzero-api identity. The direct
// `kubectl patch` is quoted for the identities allowed to use it, and the
// warning that everyone else is denied is the point: since 2026-08-13 the
// ValidatingAdmissionPolicy `loopzero-tenant-cr-shape` makes spec.cellID immutable
// except for system:masters and the loopzero-api ServiceAccount, and a bare
// `kubectl patch` from an unprivileged kubeconfig gets `Denied` with no reason
// given (B-097).
func repairHint(ns, tenantID, cellID string) string {
	return fmt.Sprintf(
		"the database already records the new cell; the Tenant CR is the stale side. "+
			"Re-run the pin so the patch is retried under the loopzero-api identity. "+
			"The direct patch `kubectl -n %s patch tenant %s --type=merge -p '{\"spec\":{\"cellID\":%q}}'` "+
			"is DENIED by the loopzero-tenant-cr-shape admission policy (spec.cellID is immutable) "+
			"for every identity except system:masters and the loopzero-api ServiceAccount",
		ns, tenantID, cellID)
}
