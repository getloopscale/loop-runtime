# Multi-cell Canary — Dev Runbook (T5.6 + T5.7)

> CHG-2026-04-30-INFRA-NORMALIZE / Phase 5. Operational checklist for the
> dev-environment canary that gates production rollout (T7.1+T7.2).
> All gates assume cell-2 is bootstrapped with cert-manager + traefik +
> ESO and the ClusterCredential is registered + connected on mgmt.
>
> Sibling: `docs/runbooks/multi-cell-canary-prod.md` (production stages 4-5).

## Pre-conditions (verified before either stage)

- [ ] `cells` table: `cell-2` row exists with `status='active'`.
- [ ] ClusterCredential: `kubectl --context k3d-loopzero-mgmt get clustercredential cell-2 -o jsonpath='{.status.connected}'` = `true`.
- [ ] DynamicRouter: `loopzero_cell_discovery_count` ≥ 2.
- [ ] Operator credentials available (`X-Service-Auth` + `LOOPZERO_ADMIN_API=true`).
- [ ] Prometheus reachable: `curl -fsS ${PROM_URL}/api/v1/query?query=up` returns 200.
- [ ] `loopzero admin tenants pin --help` works locally.

## Stage 1 — T5.6 (1 synthetic tenant on cell-2, 1h soak)

**Purpose.** Prove the routing path end-to-end: pin → CR MergePatch → operator reconcile → ArgoCD App `destination.name=cell-2` → Synced/Healthy → HTTP 200 from runtime-2.

**Driver.** `infra/tests/parity/cell2-canary-stage1.sh`.

### Step 1 — Pre-flight (`--dry-run` first)

```bash
SYNTH_TENANT_ID=tn-canary-cell2-$(date -u +%s)
bash infra/tests/parity/cell2-canary-stage1.sh --dry-run
```

Expected: exit 0, all 3 preflight checks "would query" lines emitted.

### Step 2 — Bootstrap synthetic tenant

The canary script ASSUMES the synthetic tenant is already bootstrapped with at least one ACTIVE release. Use the dogfood pipeline:

```bash
SYNTH_TENANT_ID=$SYNTH_TENANT_ID \
  bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy
```

This creates `${SYNTH_TENANT_ID}` on cell-1 (default) with a deployed `solo-server` project. Verify:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenant "$SYNTH_TENANT_ID" -o jsonpath='{.status.phase}'
# Expected: Active
```

### Step 3 — Pin + soak (live)

```bash
SYNTH_TENANT_ID=$SYNTH_TENANT_ID \
  SOAK_SECONDS=3600 \
  bash infra/tests/parity/cell2-canary-stage1.sh
```

Halt criteria (script auto-rolls back):
- 3 consecutive 5xx responses from `https://${SYNTH_TENANT_ID}-production.loopzero.local` → exit 3.
- cell-1 error budget consumed > 0.1% in any 5min window → exit 4.

### Step 4 — Cleanup (auto)

The trap on EXIT re-pins to cell-1 + deletes synthetic tenant. Verify:

```bash
kubectl --context k3d-loopzero-mgmt -n loopzero-system get tenant "$SYNTH_TENANT_ID"
# Expected: NotFound
```

### Step 5 — Update findings

Append to this runbook a "T5.6 stage 1 closure" section with:
- Run timestamp (UTC).
- Probe count + success count.
- Any 5xx blip (timestamp + URL).
- cell-1 EB delta (before/after).
- Anything operator-relevant for stage 2 readiness.

## Stage 2 — T5.7 (10% of dev tenants, 24h soak)

**Purpose.** Prove cell-2 handles real (non-synthetic) traffic at scale. 24h covers diurnal cycle. Halt threshold tighter than synthetic since real traffic has higher variance.

**Driver.** `infra/tests/parity/cell2-canary-stage2.sh`.

### Step 1 — Pre-flight (always after stage 1 GREEN)

```bash
test -f docs/runbooks/multi-cell-canary.md  # this file present
test "$(grep -c '^## T5.6 stage 1 closure' docs/runbooks/multi-cell-canary.md)" -ge 1
```

If T5.6 closure section is missing, stage 2 must NOT run.

### Step 2 — Sample selection (`--dry-run`)

```bash
bash infra/tests/parity/cell2-canary-stage2.sh --dry-run
```

Verifies: dev environment has ≥ 10 cell-1 tenants (otherwise 10% sample is meaningless).

### Step 3 — Bulk pin + soak (live)

```bash
SOAK_SECONDS=86400 PERCENT=10 \
  bash infra/tests/parity/cell2-canary-stage2.sh
```

Halt criteria:
- cell-2 aggregate 5xx rate > 1.0% in any 10min window → exit 3.
- cell-1 EB consumed > 1.0% (looser than stage 1 because traffic redistribution is the test) → exit 4.
- Any individual `loopzero admin tenants pin` returns non-zero → batch halts, partial rollback per `${PINNED_LIST}`.

### Step 4 — Stage 2 GREEN exit policy

Unlike stage 1, **stage 2 does NOT auto-rollback on success**. The 10% stays on cell-2 — that's the desired post-state going into T7.1.

If a manual rollback is needed (e.g., to free cell-2 for capacity testing):

```bash
PINNED_LIST=/tmp/cell2-canary-stage2-pinned-<TIMESTAMP>.txt
while IFS= read -r tid; do
  loopzero admin tenants pin --tenant "$tid" --cell cell-1 --reason "manual stage-2 rollback"
done < "$PINNED_LIST"
```

### Step 5 — RCA on partial halt

If stage 2 halted mid-soak (exit 3 or 4):
1. The script's rollback fired; verify with `psql -c "SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id"`.
2. Open RCA per `docs/runbooks/multi-cell-rca-template.md` (C17 mandatory template).
3. Stage 2 cannot retry until RCA is committed AND root cause is mitigated.

## Rollback playbook (manual, both stages)

```bash
# 1. List currently-on-cell-2 tenants
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
  psql -U loopzero -d loopzero -tAc "SELECT id FROM tenants WHERE cell_id='cell-2'" > /tmp/cell2-tenants.txt

# 2. Re-pin each to cell-1
while IFS= read -r tid; do
  [[ -z "$tid" ]] && continue
  loopzero admin tenants pin --tenant "$tid" --cell cell-1 --reason "manual rollback $(date -I)"
done < /tmp/cell2-tenants.txt

# 3. Confirm
kubectl --context k3d-loopzero-mgmt -n loopzero-system exec pod/postgresql-0 -c postgresql -- \
  psql -U loopzero -d loopzero -c "SELECT cell_id, COUNT(*) FROM tenants GROUP BY cell_id"
# Expected: only cell-1 (and possibly cell-default) populated
```

## Observability dashboard (panels to watch)

- `loopzero_cell_routing_decisions_total{cell="cell-2",decision="assigned"}` — should grow during soak.
- `loopzero_cell_routing_decisions_total{cell=~"cell-1|cell-2",decision="error"}` — should stay ~0.
- `loopzero_cell_health{cell="cell-2"}` — must be 1 throughout.
- `loopzero_argocd_token_age_seconds` — < 21h (KubeMintingTokenProvider proactive refresh).
- 7 alerts in `prometheusrule-multi-cell.yaml` — none firing.

## References

- Plan: `docs/plans/infra-normalize-multicell-plan.md` Phase 5 (T5.6+T5.7).
- Decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-multi-cell.md` (29 conditions, especially error-budget gates).
- Sibling runbook (production): `docs/runbooks/multi-cell-canary-prod.md`.
- Alerts runbook: `docs/runbooks/multi-cell-alerts.md`.
- RCA template: `docs/runbooks/multi-cell-rca-template.md`.
- Customer comms: `docs/customer-comms/cell-2-canary-template.md` (production stages only).
- Memory: `reference_multicell_activation_2026_04_30.md` (live activation evidence).
