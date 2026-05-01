# Multi-cell Canary — Production Runbook (T7.1 + T7.2)

> CHG-2026-04-30-INFRA-NORMALIZE / Phase 7. Operational checklist for
> migrating production tenants to cell-2 with graduated rollout
> (stage 4 → 5a → 5b → 5c). All gates assume cell-2 is fully bootstrapped
> with cert-manager + traefik + ESO + ArgoCD cluster Secret registered.

## Pre-conditions

Before triggering ANY production migration, verify:

- [ ] **Phase 5 dev canary complete**: T5.6 (1h cell-2 synthetic) + T5.7 (24h, 10% dev) both GREEN, runbook updated with findings.
- [ ] **Cells table seeded**: `SELECT * FROM cells WHERE cell_id='cell-2'` returns `status='active'`.
- [ ] **ClusterCredential ready**: `kubectl --context k3d-loopzero-mgmt get clustercredential cell-2 -o jsonpath='{.status.connected}'` = `true`.
- [ ] **Dynamic router observes cell-2**: `loopzero_cell_discovery_count` ≥ 2.
- [ ] **`task k3d:heal`** dry-runned in staging (the heal script applies to local k3d only — production uses ADR-0021 prescriptive bootstrap path).
- [ ] **Customer comms templates** at `docs/customer-comms/cell-2-canary-template.md` reviewed by customer-success.

## Stage 4 (T7.1) — 1 customer, 7-day hold

### Day -3 (3 days before pinning)

1. **Identify customer.** Pick from low-risk segments:
   - tier ∈ {pro, team} (NOT enterprise — defer to GA stages).
   - active deploys ≥ 1/week (so we observe real traffic in the soak).
   - region matches cell-2 region (`region` column in `cells`).
   - opt-in via §1 of the email template.

2. **Pre-flight checks.**
   ```bash
   # Tenant must be Active in cells table
   kubectl --context k3d-loopzero-mgmt -n loopzero-system exec pod/postgresql-0 -c postgresql \
     -- psql -U loopzero -d loopzero -c "SELECT id, cell_id FROM tenants WHERE id='${TENANT_ID}';"

   # ArgoCD app must be Synced/Healthy on current cell
   kubectl --context k3d-loopzero-mgmt -n argocd get application "${TENANT_ID}-production"

   # Recent deploy was successful (last 7 days, no failures)
   kubectl --context k3d-loopzero-mgmt -n loopzero-system exec pod/postgresql-0 -c postgresql \
     -- psql -U loopzero -d loopzero \
     -c "SELECT id, status FROM builds WHERE tenant_id='${TENANT_ID}' ORDER BY created_at DESC LIMIT 5;"
   ```

3. **Send §1 (opt-in invitation).** BCC `infra-canary@loopzero.example`. Wait for explicit confirmation.

### Day -1

1. **Send §2 (T-24h notification).**
2. **Pre-stage observability.**
   ```bash
   # Pin baseline dashboard for the customer's tenant.
   # Snapshot 24h of cell-1 SLO metrics for comparison.
   ```

### Day 0 (pin)

1. **Pin tenant to cell-2.** Operator-only command:
   ```bash
   loopzero admin tenants pin --tenant "${TENANT_ID}" --cell cell-2 \
     --reason "T7.1 stage 4 canary — opt-in $(date -I)"
   ```
   Expected output (JSON): `{"tenant":"${TENANT_ID}","cell":"cell-2","status":"pinned"}`.

2. **Verify within 60s:**
   ```bash
   # New ArgoCD app destination
   kubectl --context k3d-loopzero-mgmt -n argocd get application "${TENANT_ID}-production" \
     -o jsonpath='{.spec.destination.server}'
   # Must equal cell-2 cluster API server URL.

   # Workload pods on cell-2
   kubectl --context k3d-loopzero-runtime-2 -n "loopzero-${TENANT_ID}-production" get pods
   ```

3. **Send §3 (completion notification).** Include link to per-tenant Grafana dashboard.

### Days 1-7 — soak

Daily operator routine (before send-out):

- [ ] Check `loopzero_cell_health{cell="cell-2"}` ≥ 0.99
- [ ] Check `loopzero_cell_routing_decisions_total{cell="cell-2",decision="error"}` = 0
- [ ] Check workload Pod CPU+memory within 10% of cell-1 baseline
- [ ] Check Prometheus burn-rate alerts on customer's SLO are NOT firing
- [ ] Check `loopzero_argocd_token_age_seconds` < 6h (confirms auto-refresh active)

If ALL green: send §4 daily update.
If ANY red: see Incident → Rollback below.

### Day 7 — graduate

- [ ] 7-day soak GREEN (no rollbacks, no SLO breach).
- [ ] Send §5 (success + thank-you).
- [ ] Update CHANGELOG: `Added` entry referencing CHG.
- [ ] Move customer's row in cells table audit log.
- [ ] Open T7.2 stage 5a invocation.

## Stage 5a — 10% dev tenants

1. **Pick 10% subset.** Same low-risk filters. Email §2 to ALL.
2. **Pin in batches of 5 with 30min gaps:**
   ```bash
   for tenant in $(cat tenants-stage-5a.txt); do
     loopzero admin tenants pin --tenant "$tenant" --cell cell-2
     sleep 1800  # 30min gap to surface anomalies before next batch
   done
   ```
3. **7-day soak.**
4. **Halt criterion**: error budget consumption > 1% on cell-2 → rollback the entire 10% via `loopzero admin tenants pin --cell cell-1 ...` and STOP.

## Stage 5b — 50% dev tenants

Same pattern. Batch size 20 with 60min gaps. 7-day soak. Same halt criterion.

## Stage 5c — 100%

Final batch. After 7-day GREEN: `cells.status='active'` for cell-2 + cell-1 stays `active` for redundancy. Customer pinning becomes operator-driven via loopzero admin tenants pin (no longer "automatic on next deploy").

## Incident → Rollback (any stage)

Trigger conditions (any one):
- P1 incident on cell-2 affecting any canary tenant.
- Burn-rate alert firing for > 5min.
- ArgoCD app `Degraded` for any canary tenant.
- Operator judgment.

### Rollback procedure (≤5 min target)

1. **Page on-call** in `#incident-${DATE}`.
2. **Re-pin to cell-1**:
   ```bash
   loopzero admin tenants pin --tenant "${TENANT_ID}" --cell cell-1 \
     --reason "T7.1 rollback — ${INCIDENT_ID}"
   ```
3. **Verify pods come up on cell-1** within 60s.
4. **Verify HTTP 200** at customer's domain.
5. **Send §6 (incident notification)** within 10 min.
6. **Send §7 (rollback complete)** when stable.

### Post-incident

- [ ] Full RCA within 5 business days (template at `docs/runbooks/multi-cell-rca-template.md`).
- [ ] Halt remaining stages until RCA action items shipped.
- [ ] Customer-success follows up at day +3 and day +7.

## Observability — required dashboards

| Dashboard | Purpose | Filter |
|---|---|---|
| Multi-cell health | top-level cell health gauge | `cell ∈ {cell-1, cell-2}` |
| Per-tenant SLO comparison | side-by-side cell-1 vs cell-2 for canary tenants | `tenant_id` |
| ArgoCD sync drift | sync status by cell | `app=${TENANT_ID}-production` |
| Token refresh health | confirms self-healing token alive | `loopzero_argocd_token_*` series |

## Prometheus alerts (must be live before stage 4)

```yaml
# Burn-rate on canary tenants — page on >2x normal
- alert: CanaryTenantBurnRateHigh
  expr: tenant_request_error_rate{cell="cell-2"} > 2 * tenant_request_error_rate{cell="cell-1"}
  for: 5m
  labels: { severity: page }

# Cell-2 health degraded
- alert: Cell2HealthDegraded
  expr: loopzero_cell_health{cell="cell-2"} < 0.99
  for: 3m
  labels: { severity: page }

# Token rotation stalled
- alert: ArgoCDTokenStale
  expr: loopzero_argocd_token_age_seconds > 22*3600  # 22h
  for: 10m
  labels: { severity: ticket }
```

## Versioning

Runbook v1.0 — 2026-04-30. Owner: SRE on-call. Updates require:
1. Reflect any T7.x findings in respective stage section.
2. Email diff to customer-success before any §1-§7 template change.
3. Bump version + CHANGELOG entry.
