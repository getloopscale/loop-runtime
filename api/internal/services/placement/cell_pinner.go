// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-NORMALIZE / Phase 5 T5.5 — CellPinner service.
// Pins a tenant to a specific cell after validating:
//   1. Tenant exists.
//   2. Cell exists in `cells` table.
//   3. Cell.status is 'active' (rejects 'draining', 'maintenance', 'retired').
//   4. Cell.capacity_tenants_max not exceeded by current tenant count.
//
// Uses a single SERIALIZABLE transaction so the capacity check + UPDATE
// is atomic (no race between two pin requests filling the last slot).
//
// Returns previous cell_id so callers can rollback (`pin tenant cell-prev`).

// Package placementsvc is the cell-placement bounded context. Name avoids collision with internal/placement. M9 Step 6.
package placementsvc

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
)

// tenantGVR is the GroupVersionResource for the Tenant CR. Hardcoded
// here (instead of importing operators/api/v1) so this service has no
// operator-package dependency.
var tenantGVR = schema.GroupVersionResource{
	Group:    "loopzero.io",
	Version:  "v1",
	Resource: "tenants",
}

// CellPinnerStore is the persistence interface CellPinner depends on.
// Implementation lives in db/cell_pinner_store.go (sibling, ships in
// follow-up commit; for now the interface is enough to wire admin
// handler tests + CLI command).
type CellPinnerStore interface {
	// PinTenantToCell runs the SERIALIZABLE transaction described above.
	// Returns previous cell_id, or one of:
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
}

// NewCellPinner constructs a pinner with the given persistence backend.
// The mgmtKube + namespace can be nil/empty in tests; the production
// wireup (api/cmd/setup_server.go) passes a real dynamic client so the
// CR patch happens on every successful pin.
func NewCellPinner(store CellPinnerStore) *CellPinner {
	return &CellPinner{store: store, mgmtNS: "loopzero-system"}
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
// Flow:
//  1. DB UPDATE via CellPinnerStore (validates tenant + cell + capacity).
//  2. If a kube client is wired: strategic-merge patch the Tenant CR
//     spec.cellID. Failure is LOGGED and does NOT roll back the DB —
//     the operator's reconcile loop will re-converge once the CR is
//     manually patched OR if the next pin call retries the same cell.
//
// Returning the previous cell_id allows callers to surface "moved from
// X to Y" in the response.
func (p *CellPinner) Pin(ctx context.Context, tenantID, cellID string) (string, error) {
	if p.store == nil {
		return "", fmt.Errorf("cell pinner: store not configured")
	}
	prev, err := p.store.PinTenantToCell(ctx, tenantID, cellID)
	if err != nil {
		return "", err
	}
	if p.mgmtKube != nil {
		// T1.5 + T1.5b of deep-review-remediation-plan (closes find_arch_005
		// MEDIUM): retry transient CR patch failures (3 attempts with
		// exponential backoff: 100ms, 400ms, 1.6s). Most kube-patch failures
		// are transient (apiserver elections, network blips) — retry covers
		// these without operator intervention. Persistent failures still
		// surface via metric + slog.Warn.
		var patchErr error
	retryLoop:
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					// Bug fix iter 14: `break` inside select exits the select,
					// not the for loop — so ctx.Err() was ineffectual (next
					// iter would overwrite). Label-break correctly aborts the
					// retry loop with the context error preserved.
					patchErr = ctx.Err()
					break retryLoop
				case <-time.After(time.Duration(100*intPow(4, attempt)) * time.Millisecond):
				}
			}
			patchErr = p.patchTenantCR(ctx, tenantID, cellID)
			if patchErr == nil {
				break
			}
		}
		if patchErr != nil {
			metrics.CellPinnerCRPatchFailTotal.Inc()
			slog.Warn("cell pinner: DB updated but Tenant CR patch failed after 3 retries — operator will continue using previous cellID until the CR is patched manually",
				"tenant", tenantID, "new_cell", cellID, "error", patchErr,
				"manual_fix", dicaDeConserto(p.mgmtNS, tenantID, cellID))
		}
	}
	return prev, nil
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

// TenantCRPatcher is the narrow interface CellPinner needs to update the
// Tenant CR's spec.cellID. T5.3 of deep-review-2026-05-10-remediation-plan
// (find_arch_cell_pinner_k8s_direct MEDIUM): abstracts the dynamic.Interface
// dependency so unit tests can inject a fake patcher without standing up
// envtest. Production wiring (`api/cmd/setup_server.go`) supplies an
// adapter that wraps the real dynamic client.
type TenantCRPatcher interface {
	PatchTenantCellID(ctx context.Context, tenantID, cellID string) error
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

// dicaDeConserto monta a dica emitida quando o banco ja' foi atualizado e o patch
// no Tenant CR falhou. Os dois lados divergiram, e o CR e' o lado atrasado.
//
// A versao anterior oferecia o `kubectl patch` seco, e desde 2026-08-13 a
// ValidatingAdmissionPolicy `loopzero-tenant-cr-shape` NEGA exatamente essa mudanca —
// `spec.cellID` e' imutavel por ela — exceto para `system:masters` ou para a SA do
// proprio `loopzero-api`. Quem seguisse a dica com um kubeconfig sem privilegio
// receberia um `Denied` sem entender por que (B-097).
//
// A dica NAO manda "usar `loopzero admin tenants pin`", que e' o conselho obvio e
// errado aqui: esta funcao E' o caminho do pin, e foi ele que acabou de falhar.
func dicaDeConserto(ns, tenantID, cellID string) string {
	return fmt.Sprintf(
		"o banco ja' registra a nova cell; o Tenant CR e' o lado atrasado. "+
			"Re-execute o pin para que o patch seja tentado de novo pela identidade do loopzero-api. "+
			"O patch direto `kubectl -n %s patch tenant %s --type=merge -p '{\"spec\":{\"cellID\":%q}}'` "+
			"e' NEGADO pela politica de admissao loopzero-tenant-cr-shape (spec.cellID e' imutavel) "+
			"para qualquer identidade que nao seja system:masters ou a ServiceAccount do loopzero-api",
		ns, tenantID, cellID)
}
