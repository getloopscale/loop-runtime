# Cell-2 Activation Runbook

> Operator-facing step-by-step procedure for activating `cell-2` as a routable cell in the dev environment. Implements the APPROVED ADR `MEET-INFRA-NORMALIZE-T3-multi-cell` (2026-04-30, conditions C1–C12).
>
> Plan: `docs/plans/cell-2-activation-plan.md`
> ADR: `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md`
> Sibling runbooks: `multi-cell-canary.md` (dev canary stages 1+2), `multi-cell-canary-prod.md` (production stages 4+5 — out of scope here per ADR D6), `multi-cell-alerts.md`, `multi-cell-rca-template.md`.

This runbook covers: **Pre-flight → Local Parity → Gates → Migration → Stage 1 → Stage 2 → Closure**. Production stages 4–5 require a SEPARATE readiness review (Sprint 9, per plan ADR D6).

---

## TL;DR — Commands in Order

```bash
# Phase 0: Local parity
task k3d:up                                                  # creates 4 clusters
task k3d:heal                                                # H1–H14 idempotent
bash infra/tests/parity/cell2-activation-gates.sh --gates c1 # discovery >= 3

# Phase 1: ADR gates (offline-doable subset — full set requires live env)
bash infra/tests/parity/cell2-activation-gates.sh --json     # all 12 gates
# Operator records JSON output in PR description.

# Phase 2: Sign-off + migration
# (Operator fills in checklist below; pastes JSON output into PR body)
task db:migrate                                              # applies 000007 (DEV ONLY)
bash infra/tests/parity/cell2-activation-gates.sh --post-flip # gate_postflip GREEN

# Phase 3: Stage 1 (1h synthetic)
SYNTH_TENANT_ID=tn-canary-cell2-stage1-$(date -u +%Y%m%d-%H%M%S) \
  bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy
SYNTH_TENANT_ID=$SYNTH_TENANT_ID \
  bash infra/tests/parity/cell2-canary-stage1.sh             # 1h soak

# Phase 4: Stage 2 (24h, 10% dev tenants)
bash infra/tests/parity/cell2-canary-stage2.sh --dry-run     # verify >=10 tenants
SOAK_SECONDS=86400 PERCENT=10 \
  bash infra/tests/parity/cell2-canary-stage2.sh             # 24h soak

# Phase 5: Closure
# Append T5.6 + T5.7 closure sections to multi-cell-canary.md
# Bump CHANGELOG + memory + maturity assessment.
```

---

## Phase 0 — Pre-flight & Local Parity

### Step 1: 4-cluster k3d topology

```bash
task k3d:up
```

Expected: `task k3d:status` reports 4/4 clusters reachable: `loopzero-mgmt`, `loopzero-build`, `loopzero-workload`, `loopzero-workload-2`.

If `loopzero-workload-2` is missing, the underlying cluster config was not picked up. Fix:
```bash
k3d cluster create --config infra/k3d/workload-2-cluster.yaml
```

### Step 2: Idempotent heal — bootstraps cell-1 + cell-2 ClusterCredentials

```bash
task k3d:heal
```

Expected log lines: `H12: ClusterCredential 'workload' (cellID=cell-1, ingress-domain=loopzero.local) created/refreshed` AND `H14: ClusterCredential 'workload-2' (cellID=cell-2, ingress-domain=loopzero.local) created/refreshed`. Re-running is a no-op (`already connected — skip`).

### Step 3: Discovery count gate (≥3)

```bash
bash infra/tests/parity/cell2-activation-gates.sh --gates c1
```

Expected: `C1 PASS: cells row present + CC present + discovery=3`. Allow up to 90s for DynamicRouter refresh after H14. If FAIL: check `kubectl --context k3d-loopzero-mgmt get clustercredentials -o yaml`.

---

## Phase 1 — ADR Conditions Verification

### All 12 gates in one go

```bash
bash infra/tests/parity/cell2-activation-gates.sh --json | tee /tmp/cell2-gates-$(date -u +%Y%m%d-%H%M%S).json
```

Expected JSON shape: `{"all_pass":true,"gates":{"c1":"PASS","c2":"PASS",...,"c12":"PASS"}}`.

If `all_pass:false`, do NOT proceed to Phase 2 — investigate the FAIL gate output. Each gate prints diagnostic info.

| Gate | Source | Recovery if FAIL |
|------|--------|------------------|
| c1 | discovery count | re-run `task k3d:heal` (H14) |
| c2 | routing determinism | `cd api && go test -race -run TestDynamicRouter_Resolve_Deterministic ./internal/placement/...` |
| c3 | Stage 5a tenant-count | dev: N/A (always PASS); prod: see `multi-cell-canary-prod.md` |
| c4 | rollback SLO | `SYNTH_TENANT_ID=... bash infra/tests/parity/cell2-rollback-slo.sh` (10 iterations, ~30 min) |
| c5 | PVC docs | confirm `multi-cell-canary.md` has "Stateful workload guidance" section |
| c6 | rollback indep | code review — `cell_pinner.go` mgmt-only |
| c7 | shared ingress | re-run heal so H12+H14 apply `INGRESS_DOMAIN=loopzero.local` |
| c8 | DOKS revocation | dev: N/A; prod: `doctl kubernetes cluster kubeconfig show loopzero-runtime-2` |
| c9 | log redaction | rotate any deploy that leaked credentials in logs (very rare) |
| c10 | tenant data isolation | only meaningful AFTER first cell-2 pin — re-run after Stage 1 first probe |
| c11 | authz 3 paths | `cd api && go test -run TestAdminPinCell_AuthzInvariantC11 ./internal/routes/...` |
| c12 | Secret CA hygiene | re-run heal — H12 + H14 + (rare) regenerate Secret manually |

---

## Phase 2 — Status Flip

### Step 1: Pre-flip sign-off (T2.1)

Operator fills in this template directly in the PR description:

```markdown
## Cell-2 Activation Sign-Off (cell-2-activation-plan T2.1)

Operator: <NAME>
Date: <YYYY-MM-DD>
PR: <link>

Pre-conditions verified:
- [ ] Local k3d 4-cluster topology — `task k3d:status` GREEN
- [ ] `task k3d:heal` exit 0 — H1–H14 codified
- [ ] `cell2-activation-gates.sh --json` returned `all_pass:true`
      <details><summary>JSON</summary>

      <PASTE JSON OUTPUT HERE>

      </details>
- [ ] Rollback SLO measurement (T1.2):
      P50 = ___ s, P95 = ___ s — within ADR budget (P50 ≤ 300s, P95 ≤ 480s)
      CSV path: /tmp/cell2-rollback-slo-<TIMESTAMP>.csv
- [ ] Synthetic tenant ready for Stage 1: tenant-id = ___

Sign-off: <SIGNATURE> <DATE>
```

### Step 2: Apply migration 000007 (DEV ONLY)

```bash
task db:migrate
```

The migration is idempotent (`WHERE status='maintenance'`), so re-running is a no-op.

> **WARNING**: This migration MUST NOT be applied to production until Sprint 9 prod canary readiness review APPROVES Stage 4 (per ADR D6 in cell-2-activation-plan). Pre-release governance: engineering applies migrations to prod manually via `task db:migrate` against the prod PG. Reviewer of the PR that introduces this file MUST verify it lands in dev only.

### Step 3: Verify post-flip — gate_postflip

```bash
bash infra/tests/parity/cell2-activation-gates.sh --post-flip
```

Expected: `POSTFLIP PASS: cells.cell-2.status=active + admin endpoint reachable`.

---

## Phase 3 — Stage 1 (T5.6 — 1h synthetic, ADR Stage 1)

See `multi-cell-canary.md` Stage 1 for the full procedure. Brief:

```bash
SYNTH_TENANT_ID=tn-canary-cell2-stage1-$(date -u +%Y%m%d-%H%M%S)
SYNTH_TENANT_ID=$SYNTH_TENANT_ID bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy
SYNTH_TENANT_ID=$SYNTH_TENANT_ID SOAK_SECONDS=3600 \
  bash infra/tests/parity/cell2-canary-stage1.sh
```

Halt criteria (auto-rollback): 3 consecutive 5xx → exit 3; cell-1 EB > 0.1% → exit 4.

On GREEN: append `## T5.6 stage 1 closure` section to `multi-cell-canary.md` per its versioning rule (line 178–180).

---

## Phase 4 — Stage 2 (T5.7 — 24h, 10% dev, ADR Stage 2)

See `multi-cell-canary.md` Stage 2 for the full procedure. Brief:

```bash
bash infra/tests/parity/cell2-canary-stage2.sh --dry-run    # verify ≥10 cell-1 tenants
SOAK_SECONDS=86400 PERCENT=10 bash infra/tests/parity/cell2-canary-stage2.sh
```

On GREEN: append `## T5.7 stage 2 closure` section to `multi-cell-canary.md`.
On HALT: open RCA per `multi-cell-rca-template.md`. Stage 2 cannot retry until RCA committed.

---

## Phase 5 — Closure

1. CHANGELOG `[Unreleased]` `Added` entries for each task (see plan T5.2 for exact wording).
2. Memory file: `~/.claude/projects/-home-paulo-loopzero/memory/reference_cell2_activation_<DATE>.md`. Index in MEMORY.md.
3. Maturity assessment: bump 3.7 → 3.8 (Scale/Multi-tenancy 3.0 → 3.5, Infrastructure 3.8 → 4.0); update Path-to-4.0 row.
4. Update sibling runbook `multi-cell-canary.md` versioning per its policy.
5. Schedule prod canary readiness review for Sprint 9 (Stages 4 + 5).

---

## Troubleshooting

### `cell2-activation-gates.sh c1` fails with discovery count = 2

Check ClusterCredential CR existence:
```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system get clustercredentials -o jsonpath='{range .items[*]}{.spec.cellID}={.status.connected}{"\n"}{end}'
```
Expected: `cell-default=true`, `cell-1=true`, `cell-2=true`. If cell-2 missing or disconnected: `task k3d:heal` (H14 will reapply).

### `gate_c11` fails with `TestAdminPinCell_AuthzInvariantC11` not found

Stale binary. Rebuild + retest:
```bash
cd api && go test -count=1 -run TestAdminPinCell_AuthzInvariantC11 ./internal/routes/...
```

### Migration 000007 applied but `gate_postflip` reports status != active

Possible: migration UPDATE matched 0 rows because cells.status was already `draining` or `retired`. Check current status:
```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec pod/postgresql-0 -c postgresql -- psql -U loopzero -d loopzero -c "SELECT cell_id, status, updated_at FROM cells WHERE cell_id='cell-2';"
```
If status was changed manually outside migration, issue a NEW migration `000008_<purpose>.up.sql` (forward-only).

### Stage 1 canary halts on 3 consecutive 5xx

Auto-rollback re-pinned to cell-1 and deleted synthetic tenant. Investigate:
1. `kubectl --context k3d-loopzero-workload-2 -n loopzero-${SYNTH_TENANT_ID}-production logs -l app.kubernetes.io/component=runtime`
2. `kubectl --context k3d-loopzero-mgmt -n argocd get application ${SYNTH_TENANT_ID}-production -o yaml`
3. Open RCA per `multi-cell-rca-template.md`. Stage 2 must NOT run until RCA action items shipped.

---

## References

- Plan: `docs/plans/cell-2-activation-plan.md`
- ADR: `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md`
- Migration: `api/internal/db/migrations/000007_activate_cell_2.up.sql`
- Master gates: `infra/tests/parity/cell2-activation-gates.sh`
- Gate libs: `infra/tests/parity/lib/cell2-gate-c{1..12,postflip}.sh`
- Heal script: `infra/scripts/k3d-cross-cluster-heal.sh` (H12, H13, H14)
- Routing tests: `api/internal/placement/placement_test.go` (TestDynamicRouter_Resolve_Deterministic*)
- Authz tests: `api/internal/routes/admin_pin_cell_test.go` (TestAdminPinCell_AuthzInvariantC11*)
- Migration tests: `api/tests/unit/migration_007_activate_cell_2_test.go`
- Rollback SLO: `infra/tests/parity/cell2-rollback-slo.sh`
- Sibling: `docs/runbooks/multi-cell-canary.md` (dev canary procedure)
- Sibling: `docs/runbooks/multi-cell-alerts.md` (alert response)
- Sibling: `docs/runbooks/multi-cell-rca-template.md` (RCA template)
