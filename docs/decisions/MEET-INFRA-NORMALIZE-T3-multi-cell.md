# MEET-INFRA-NORMALIZE-T3 — Multi-cell Activation (T3 Governance Review)

**Date:** 2026-04-30
**Topic:** Tiered canary activation of `cell-2` (runtime-2 DOKS cluster) — `docs/plans/infra-normalize-multicell-plan.md` ADR D3 + Phase 5 (T5.1–T5.7) + Phase 7 (T7.1–T7.2)
**Type:** `architecture` + `controllers` + `security` + `gitops` + `observability` + `verification` (cross-cutting, tenant-isolation impact)
**Tier:** T3 (tenant isolation change — automatic T3 trigger per `.claude/rules/pre-release-governance.md`)
**Scope:** This meeting governs Phase 5 (dev canary stages 1–2) AND Phase 7 (prod canary stages 4–5). Stage 3 (staging) deferred — staging environment does not yet exist per CLAUDE.md "Production Infrastructure".
**Out of scope:** Operator topology cutover (D1, separate decision), Pulumi consolidation (D2, separate decision), CLI cluster-real tests (D4, separate decision), runtime-2 *capacity* sizing (already shipped per Sprint 8 v3.2 / ADR-0015).

**Agents convened:** platform-architect (Sponsor) · production-design-reviewer (Challenger) · platform-security-engineer (Veto) · sre-engineer (Veto) · distributed-systems-engineer · dx-engineer

**Owner (post-approval):** sre-engineer (lead, owns canary execution + EB gating) + infra-engineer (Pulumi/cells migration) + api-engineer (CLI flag + authz) + platform-security-engineer (co-signs cred rotation).

**Verdict:** **APPROVED_WITH_CONDITIONS**

---

## Stage 1 — Context

`runtime-2` DOKS cluster is **physically provisioned** in canonical Pulumi (`infra/pulumi/src/components/infra.ts:160-175`, `trustedK8sClusters: [..., runtimeCluster2.clusterId]` at line 205) but **not routed**:

- `infra/pulumi/Pulumi.dev.yaml` declares `mgmt*`, `runtime*`, `build*`, `db*` keys but **no `cellId` and no `runtime2*` keys** (verified by Read; lines 1–44 only).
- `api/internal/db/migrations/000001_initial_schema.up.sql` defines `cell_id TEXT NOT NULL DEFAULT 'cell-default'` columns + indexes + a populate-from-tenant trigger, but **the `cells` table itself is not in `000001` nor in any other migration** (verified by `grep -nE "CREATE TABLE.*cells\b"` returning zero hits across `000001..000005`). T5.3 of the plan creates `000003_cells_seed.up.sql` — but a seed cannot run against a missing table. **This is a load-bearing prerequisite gap surfaced by this review.**
- `api/internal/placement/dynamic_router.go` (169 lines) is fully coded with stale-data protection (MEET-053 A-03), discovery metric, and ClusterCredential-backed `K8sCellStore`. `api/cmd/setup_background.go:104-151` instantiates it: `cfg.DynamicRouter` defaults to true (`LOOPZERO_DYNAMIC_ROUTER` != "false"), but the K8sCellStore returns 0 cells today (no `ClusterCredential` CR for runtime-2 exists), so the live system falls back to `StaticRouter` with `cell-default` (line 137-150). **Code path exists; data does not.**
- `CLAUDE.md` Cell architecture section: "runtime-2 physically provisioned but not routed; cell-2 activation Sprint 9".
- `docs/runbooks/sprint8-l3-stabilization.md` B18 documents that even the *single* runtime cluster needed a 3→8 node bump for L3 burst (50 vCPU sustained at 100 tenants × 4 apps); cell-2 capacity gating must inherit that ratio.

### Decision required

Adopt **Option C** of plan ADR D3: tiered canary with explicit error-budget gates per stage, spanning dev (Phase 5) → production (Phase 7), with halt criteria, rollback procedure per stage, and customer comms for production stages.

Stages (verbatim from plan ADR D3 lines 117–121, this meeting's scope):

1. **Stage 1 — dev:** 1 synthetic tenant on cell-2; 1h soak. Gate: `loopzero_cell_routing_decisions_total{cell="cell-2"}` > 0; `loopzero_tenant_availability{cell="cell-2"}` ≥ 99%.
2. **Stage 2 — dev:** 10% dev tenants on cell-2; 24h soak. Gate: cell-1 EB consumption < 0.1%.
3. **Stage 3 — staging:** repeat if/when staging exists. **Deferred — out of scope.**
4. **Stage 4 — prod:** 1 customer opt-in tenant on cell-2; 7-day hold.
5. **Stage 5 — prod:** 10% → 50% → 100%, 7-day holds between substages.

### Veto criteria from plan ADR D3 (lines 123–126) + rules

- **Production-design (line 126):** "rollback to cell-1 must be possible per-tenant within 5 min".
- **SRE (line 124):** "any stage with EB consumption > 1% halts rollout pending RCA".
- **Security (line 125):** "cell-2 cluster credentials rotated before activation".
- **Distributed-systems (this meeting, supplied by D6 observability rule):** dynamic router must handle cell unavailability without erroring user requests; routing decisions are deterministic (same tenant → same cell over time barring explicit re-pin).

### Pre-conditions inherited from sibling decisions

- **MEET-INFRA-NORMALIZE-T3-operator-split** (ADR D1) — operator topology must be GREEN before multi-cell. Multi-cell relies on the `clustercredential-controller` reconciling `ClusterCredential` CRs into ArgoCD cluster Secrets (split `loopzero-platform-operator` ClusterRole). If operator-split is not GREEN, K8sCellStore cannot discover cell-2.
- **MEET-INFRA-NORMALIZE-T3-pulumi-consolidation** (ADR D2) — only canonical `infra/pulumi/` may write Pulumi state for `runtime-2`. T5.2 changes are restricted to canonical only.
- **ADR-0015** — adopt-or-create policy applies to `runtime-2` cluster Secret + ESO ClusterSecretStore.

---

## Stage 2 — Analyses

### platform-architect (Sponsor)

Option C is architecturally correct. The cell-2 code (DynamicRouter + K8sCellStore + ClusterCredential CRD) was deliberately built with empty-set tolerance (`A-03 stale-data protection`) precisely so cell-2 introduction is non-disruptive: zero-cell discovery falls back to StaticRouter; non-zero discovery activates dynamic routing without restart. Activation is therefore a **data-plane** change (cells table + ClusterCredential CR) with feature-flag-equivalent behavior built into the router itself — no code redeploy required to turn cell-2 on or off.

**Risks I flag:**

1. **Missing `cells` table.** Plan T5.3 inserts seed rows but no migration creates the table. Either a prior migration creates it (and grep missed it — verified false: all 4 migrations grepped) or the system has been running on `cell_id` *columns* with no `cells` table to JOIN against. T5.3 must create the table AND seed in 000003, OR plan must be amended to split into 000003 (DDL) + 000004-renumbered (seed). **C1 mandatory.**
2. **Determinism of routing decisions.** With DynamicRouter's 60s refresh interval, a `cells` table change at T₀ propagates to API process at T₀+60s worst-case. Mid-deploy traffic could split: some requests routed by stale list (cell-default), some by fresh list (cell-2). Distributed-systems veto criterion (deterministic routing) requires either (a) router cache invalidation on `cells` table change, or (b) accept ≤60s split-routing window with documented invariant. **C2 mandatory** — declare invariant + test.
3. **Stage 4 → Stage 5a transition is risky.** Stage 4 is "1 customer". Stage 5a jumps to 10%. If production has 100 tenants, 10% = 10 tenants — a 10× blast-radius increase in one step. **C3 mandatory** — Stage 5a opens with a 24h hold at 1%/3%/5% if production tenant count > 50, before reaching 10%.

**Recommendation:** APPROVED with C1, C2, C3.

### production-design-reviewer (Challenger)

The 5-min per-tenant rollback claim (line 126) requires verification:

1. **Rollback path = `loopzero deploy --cell cell-1 <tenant>`.** This re-pins via T5.5's CLI flag. But the action propagates through: API persists cell_id update → ArgoCD application destination updated → ArgoCD sync → K8s reconcile → traffic shift via Traefik. Plan claims <5 min; under L3 stress (B18) ArgoCD sync alone took 2–4 min. **C4 mandatory** — rollback SLO measured end-to-end (API call → first HTTP 200 from new cell) on dev with realistic load before Stage 4. SLO budget: 5 min P50, 8 min P95.
2. **Rollback SAFETY: data loss.** Plan ADR D3 line 130 claims "no data loss because tenant data lives in shared PG (mgmt cluster), not in runtime cluster local storage". I challenge: tenant *application* data (deployed app's persistent state) — if the app uses StatefulSet PVC on runtime-2, switching to runtime-1 abandons that PVC. **C5 mandatory** — runbook explicitly enumerates: control-plane data (PG, mgmt-only — safe), platform metadata (cells table, mgmt-only — safe), runtime app data (PVCs on runtime-N — **NOT MIGRATED on cell re-pin**). Customer comms (C12) must disclose this for stateful apps; canary recruitment (Stage 4) must exclude tenants with PVCs unless a separate data-migration runbook exists.
3. **ArgoCD application reachability mid-rollback.** If runtime-2's ArgoCD cluster Secret is mid-rotation when rollback fires, sync might fail. **C6 mandatory** — rollback procedure independent of cell-2 cluster reachability (re-pin happens by API/PG update + ArgoCD app destination edit, not by reaching cell-2).
4. **Per-cell ingress domain isolation.** `K8sCellStore` reads `loopzero.io/ingress-domain` label per cell. Plan does not specify whether cell-2 has a distinct domain (e.g., `*.cell2.dev.loopzero.example`) or shares `*.dev.loopzero.example`. Shared domain means tenant URLs survive re-pin (Cloudflare → mgmt API → re-routed); distinct domain means re-pin requires DNS change (NOT 5-min rollback compatible). **C7 mandatory** — declare ingress-domain strategy; for ≤5 min rollback SLO, shared-domain path required.

**Recommendation:** APPROVED with C4, C5, C6, C7.

### platform-security-engineer (Veto holder — credentials + isolation)

**Credentials veto (line 125).** Plan T5.1 rotates kubeconfig + ESO ClusterSecretStore. Acceptable but incomplete:

1. **Old creds must be revoked, not merely replaced.** DOKS kubeconfig revocation is via DigitalOcean control panel "Reset cluster credentials" (rotates the cluster-CA-bound certificate). T5.1 doesn't currently invoke this. **C8 mandatory** — DOKS-side revocation evidence (`doctl kubernetes cluster kubeconfig show` returns the new cert; old cert returns 401 from API server).
2. **Cell-2 cluster credentials must NEVER appear in cell-1 logs/traces.** Multi-tenant log routing on mgmt cluster could leak. **C9 mandatory** — `kubectl logs` of API + operators on mgmt redacts kubeconfig payloads; a regression test asserts no `BEGIN CERTIFICATE` byte sequence appears in metric/log exports for 24h post-activation.
3. **Tenant data MUST NOT cross cells.** Tenant T pinned to cell-2 must never have traffic routed to cell-1 by mistake (or vice versa). Distributed-systems analysis below addresses determinism; my requirement is **isolation verification at the data plane**: pod logs on cell-1 do not contain tenant T's identifiers post-pin, and HTTP requests targeting tenant T receive responses originating from cell-2 IPs only. **C10 mandatory** — Stage 1 acceptance criteria expanded to include `kubectl logs -n <tenant-ns>` cross-cell grep returning 0 lines for the canary tenant on the wrong cell, AND CF Tunnel access logs confirming tenant T traffic egresses runtime-2 only.
4. **Authz on `--cell` flag (T5.5).** Plan acknowledges authz check; veto is on **bypass paths**: API direct call (curl with token) must be guarded equivalently to CLI; webhook-driven autodeploy must not silently override `cell_id`. **C11 mandatory** — authz tested in 3 paths: CLI, API direct, GitHub App webhook autodeploy. **VETO if any path bypasses authz.**
5. **ClusterCredential Secret hygiene.** ArgoCD cluster Secret for runtime-2 contains base64(kubeconfig). Per `clustercredential_controller.go` (operator-split MEET fix #6 lines), `tlsClientConfig.caData` must be base64(PEM). **C12** (renamed; was C11 in draft) — assert MEET-fix-6 invariant holds for runtime-2 Secret on Stage 1 entry.

**Veto position:** **PARTIAL VETO** lifted CONDITIONAL on **C8 + C9 + C10 + C11 + C12**. Without C11 (authz bypass test), security veto REINSTATED.

### sre-engineer (Veto holder — error budget + observability)

**Error-budget gates** are the load-bearing safety mechanism. Plan ADR D3 specifies "EB consumption < 0.1%" (Stage 2) and "> 1% halts rollout" — but lacks (a) the SLO definition that EB derives from, (b) the measurement window, (c) the multi-burn-rate alert spec. I require precise SLI/SLO spec per stage:

| Stage | SLI | SLO target | Window | Halt criterion |
|-------|-----|-----------|--------|----------------|
| Stage 1 (dev, 1h) | `loopzero_tenant_availability{cell="cell-2"}` (1 - 5xx_rate over 5m windows, synthetic tenant probe) | ≥ 99.0% | 1h rolling | Any 5m window < 95% **OR** `loopzero_cell_routing_decisions_total{cell="cell-2",decision="error"}` > 5 over the hour |
| Stage 2 (dev, 24h) | cell-1 EB consumption | < 0.1% of 28-day budget | 24h soak | (a) Cell-1 5xx_rate increases > 0.05 pp vs pre-canary 24h baseline, OR (b) cell-2 availability < 99.0% sustained 15m, OR (c) `MultiCellRouterErrorRate` fires (rate(error)>0.01 over 5m), OR (d) cell-2 P99 latency > 2× cell-1 P99 sustained 30m |
| Stage 4 (prod, 7d) | tenant-T availability | ≥ 99.5% (matches plan) | 7d | Any 1h window < 99.0%, OR EB consumption > 0.5% of 28-day budget for tenant T, OR customer reports incident, OR cell-2 cluster-wide availability < 99.5% |
| Stage 5a (prod 10%, 7d) | cell-2 fleet availability | ≥ 99.5% | 7d | Cell-2 EB consumption > 1% (per ADR D3 line 124), OR cell-1 EB consumption > 0.1% (cross-contamination), OR P99 latency divergence cell-2 vs cell-1 > 50% sustained 1h |
| Stage 5b (prod 50%) | same as 5a | 99.5% | 7d | same; multi-burn-rate alert (1h burn × 14.4 + 5m burn × 14.4 — Google SRE Workbook recipe) |
| Stage 5c (prod 100%) | overall fleet availability | ≥ 99.5% | 7d soak post-cutover | same |

**Other vetos:**

1. **Capacity gate (B18 inheritance).** Cell-2 must auto-scale identically to cell-1 (8-node max per current Pulumi). Stage 4+ blocked until `runtime-2` node-pool max ≥ 8 verified by `doctl kubernetes cluster node-pool list loopzero-infra-prod-sfo3-runtime-2`. **C13 mandatory.**
2. **Observability is a deliverable, not a follow-up.** Plan T5.4 metrics list is a starting point; my SRE acceptance bar adds: cross-cell P99 latency comparison panel; per-cell ArgoCD sync duration histogram; `loopzero_cell_router_refresh_failures_total` + alert. **C14 mandatory** — full metric/alert spec in deliverables.
3. **Halt mechanism must be machine-actionable.** "EB > 1% halts rollout" is a sentence. I need: (a) an alert that, when firing, automatically triggers a rollback runbook (PagerDuty + on-call ack within 15 min); (b) a `--paused` mode for the canary script that prevents promotion to next stage if any halt criterion is met. **C15 mandatory.**
4. **Pre-stage smoke.** Before each stage transition, run a 5-min synthetic load against cell-2 to confirm baseline; if synthetic fails, do not promote. **C16 mandatory.**
5. **Post-mortem template.** RCA template required for any halt event. **C17 mandatory** — `docs/runbooks/multi-cell-rca-template.md`.
6. **Independence: dev cell-2 GREEN ≠ prod cell-2 GREEN.** Per the user's explicit question: if prod Stage 4 fails canary, runbook must specify (a) immediate rollback per C4, (b) RCA before any retry, (c) **regression of dev cell-2 to verify the failure mode reproduces or is prod-specific** (capacity, traffic pattern, customer-specific config). **C18 mandatory** — prod-failure runbook explicit.

**Veto position:** LIFTED CONDITIONAL on **C13 + C14 + C15 + C16 + C17 + C18** AND on the SLI/SLO table above being committed verbatim into `docs/runbooks/multi-cell-canary.md`.

### distributed-systems-engineer

**Determinism of routing decisions.** DynamicRouter's `Resolve(cellID)` is deterministic given the cell map. The non-determinism is in *which* cell map (60s refresh window) and in *who calls Resolve with what cellID*. Today, callers pass `tenant.cell_id` from PG. That means determinism = (a) PG row stable, (b) router has the cell-2 entry. Both hold post-T5.3.

**Failure modes I require addressed:**

1. **cell-2 cluster unreachable mid-request.** API receives a deploy request for tenant pinned cell-2; cell-2 K8s API down. Today, `ArgoCD sync` would error out. The user-visible result: deploy 503 with cryptic error. **C19 mandatory** — API surfaces a clear `503 cell unavailable, retry in N seconds`; CLI translates to user-friendly "deploy temporarily unavailable, retrying"; metric `loopzero_cell_routing_decisions_total{decision="cell_unavailable"}` increments; **request is NOT silently re-routed to cell-1** (would break tenant-pin contract).
2. **Cell map empty (race on first refresh).** Stale-data protection exists (line 100). Acceptable.
3. **Cell map shrinks (cell-2 removed mid-flight).** `len(newCells) < previousCount` → warn but accept. A pinned tenant's cell could disappear; Resolve returns "unknown cell_id". Need explicit handling: **C20 mandatory** — API treats this as 503 with clear error, NOT 500; runbook step exists for emergency cell-removal (with comms).
4. **Two-router clock skew.** Multiple API replicas refresh independently; skew up to 60s. For ≤5-min rollback SLO this is fine, but the ≤5-min SLO must include this skew. **C21** — note skew in C4 SLO measurement.

**Recommendation:** APPROVED CONDITIONAL on **C19 + C20 + C21**.

### dx-engineer

**Customer comms + CLI ergonomics.**

1. **`loopzero deploy --cell <id>` is a power-user flag.** It must not be discoverable to typical users (no surfacing in `loopzero deploy --help` mainstream description) but must be documented for ops + canary customers. **C22 mandatory** — flag hidden by default (`--help` shows it only with `--advanced`); error if non-ops user attempts to set it; CLI error suggests "contact support@loopzero.example to enroll in canary".
2. **Customer comms template (Stage 4).** Subject + body + opt-in mechanism. Per plan T7.1 references `docs/customer-comms/cell-2-canary-template.md`. I require:
   - **Subject:** `[LoopZero] Action requested: opt-in for cell-2 canary on <tenant-name>`. Avoids forbidden internal terms (Rule 3): no mention of "ArgoCD", "GitOps", "K8s", "cluster". Use product language: "isolated infrastructure pool", "canary deployment".
   - **Body** (5 sections, ≤400 words): (1) what's changing, (2) what we're testing, (3) blast radius (1 tenant, 7-day hold, immediate rollback if issues), (4) opt-in mechanism + opt-out mechanism, (5) contact + status page link.
   - **Opt-in mechanism:** customer replies to the email AND signs an "Acknowledgment of canary participation" (1-page; releases nothing legally but makes the participation explicit). No silent opt-in. No web-form opt-in (too easy to misclick).
   - **Opt-out mechanism:** customer replies "opt out" at any time; rollback within 5 min per C4; no penalty.
   - **Status page note:** changelog entry at `Stage 5c GA`, NOT before (canary is ops-internal until production GA).
   **C23 mandatory.**
3. **CLI exit codes for cell-pin failures.** `loopzero deploy --cell` failure must respect Rule 4 exit codes: 1 (usage error if cell-id unknown locally), 4 (auth error if user lacks pin permission), 3 (deploy failure if cell unreachable). NOT a new code. **C24 mandatory.**
4. **`loopzero platform status` JSON.** Today's `--json` shape is a stability contract (Rule 5). Adding cell info MUST be a new field (e.g., `cells: [{id, status, tenant_count}]`), not a rename. **C25 mandatory** — schema migration documented in CHANGELOG `[Added]`.
5. **Rollback ergonomics.** `loopzero deploy --cell cell-1 <tenant>` is the rollback. CLI must echo a confirmation showing pre-state and post-state cell_id, and error if `<tenant>` is not a tenant the user owns/operates. **C26 mandatory.**

**Recommendation:** APPROVED CONDITIONAL on **C22 + C23 + C24 + C25 + C26**.

---

## Stage 3 — Confrontation

### Conflict 1 — Missing `cells` table DDL

All agents implicitly assumed table exists. Architect verified absent. **Resolution:** **C1 mandatory** — plan T5.3 amended: 000003 creates `cells (cell_id PK, status, ingress_domain, secret_ref_name, secret_ref_namespace, max_tenants, capacity_pods, created_at, updated_at)` AND seeds cell-1 + cell-2 idempotently (`ON CONFLICT DO NOTHING`).

### Conflict 2 — 60s refresh window vs. determinism veto

Distributed-systems requires deterministic routing; architect notes 60s window. Resolution: **C2 mandatory** — declared invariant: "tenant T's cell pin propagates to all API replicas within 60s; during that window, requests for T may execute on the prior cell or new cell, but never both for the same request lifecycle". A request, once routed, completes on its assigned cell. No mid-request re-routing. Test: `test_router_resolves_consistently_within_request()`.

### Conflict 3 — Stage 5a 1× → 10× jump

Architect concern accepted. **Resolution:** **C3 mandatory** — plan Phase 7 amended: Stage 5a sub-stages at 1% / 3% / 5% / 10% with 24h holds between if production has > 50 tenants. If ≤ 50 tenants, jump to 10% (effectively ≤ 5 tenants) acceptable.

### Conflict 4 — Per-cell domain vs shared domain

Production-design flagged DNS dependency. Resolution: **C7 mandatory** — shared-domain strategy adopted (`*.dev.loopzero.example`, `*.loopzero.app`). Cell-2 ingress operates under same wildcard cert; Cloudflare → mgmt API determines target cell via `cells` table lookup; runtime-N receives traffic via internal CF Tunnel routes. Re-pin = no DNS change = ≤5-min rollback compatible.

### Conflict 5 — Rollback SLO measurement under load

Production-design + SRE concur 5-min claim is unverified under L3 burst (B18). **Resolution:** **C4 mandatory** — pre-Stage-4 dev rehearsal: 100 dev tenants on cell-2; force re-pin of 10 tenants; measure end-to-end SLO; must achieve P50 ≤ 5 min, P95 ≤ 8 min. Failure → halt prod rollout pending optimization.

### Conflict 6 — Stateful tenant exclusion

**Resolution:** **C5 mandatory** — Stage 4 customer recruitment script excludes tenants with `kubectl get pvc -n <ns>` non-empty. Stateful migration is a separate (future) decision.

### Conflict 7 — Halt mechanism: alert vs. automation

SRE wants automation; production-design wants human-in-loop. **Resolution:** **C15 mandatory** — alerts auto-fire to PagerDuty (15-min ack SLO); on-call engineer manually executes rollback runbook; **no autonomous rollback** (human decision required, but rollback procedure is single-command). Promotion to next stage gated on a manual `--proceed` flag in canary script (no auto-promotion).

### Conflict 8 — What if dev cell-2 GREEN but prod cell-2 fails canary?

Explicit user prompt question. **Resolution:** **C18 mandatory** — runbook `docs/runbooks/multi-cell-prod-failure.md` covers:

1. **Immediate rollback (T+0 to T+15min):** PagerDuty fires; on-call rolls back tenant T to cell-1 per C4 procedure; verify cell-1 picks up traffic; status page incident posted.
2. **RCA (T+15min to T+24h):** RCA template (C17) populated. Categorize root cause: (a) **capacity** (cell-2 under-provisioned vs prod load profile), (b) **config drift** (cell-2 cluster missing component present on cell-1), (c) **traffic pattern** (prod has request shape dev didn't), (d) **customer-specific** (tenant T has unusual workload).
3. **Reproduction in dev (T+24h to T+72h):** Attempt to reproduce on dev cell-2 with synthetic load matching the failure profile. If reproduces → fix; re-run Phase 5 Stage 2 (24h dev soak) before re-attempting Stage 4. If does NOT reproduce → category (c) or (d); custom investigation; Stage 4 retry blocked until reproduction GREEN OR explicit T2 supplementary meeting approves a different remediation path.
4. **Customer comms:** opted-in customer notified within 1h of rollback; weekly RCA summary shared post-resolution. Status page incident closed only after RCA committed.
5. **Stage 4 retry gate:** at least 7-day green dev soak after fix before retry. No "fix it forward in prod".

### Self-challenge (orchestrator)

Three additional gaps:

1. **`cells` table seed must include capacity metadata.** Without `max_tenants` and `capacity_pods` fields, Stage 5b (50%) cannot enforce capacity gate. **C27** — DDL includes capacity columns; seed values match Pulumi `runtimeMaxNodes`.
2. **GitHub App autodeploy path.** Webhook → API → build → release → deploy: at deploy step, API reads `tenant.cell_id`. If autodeploy fires for tenant T (pinned cell-2) while cell-2 is in halt-mode, fallback behavior is undefined. **C28** — autodeploy honors halt-mode (returns 503 with `Retry-After`); webhook delivery retried by GitHub.
3. **Backup/recovery posture for `cells` table.** Standard PG backup covers it, but a corrupted cells table (e.g., cell-2 row with bad secret_ref) breaks all multi-cell routing. **C29** — pre-Stage-4: PG point-in-time recovery confirmed for cells table; runbook entry `docs/runbooks/cells-table-recovery.md`.

---

## Stage 4 — Decision

**Chosen approach.** **Option C — tiered canary with explicit error-budget gates** with mandatory conditions C1–C29.

**Alternatives considered & rejected.**

| Alternative | Rejection reason |
|-------------|------------------|
| **Option A — Activate all at once: tag all dev tenants → cell-2.** | Blast radius = 100% of dev. Violates production-design veto + zero EB headroom. **REJECTED.** |
| **Option B — 1-tenant canary only (1 dev → 1 prod, no graduation gates).** | Insufficient confidence at scale: 1 tenant cannot reveal multi-tenant interference, capacity contention, or tail latency under load. SRE veto criterion (line 124) fails — no EB consumption signal until volume sufficient to detect. **REJECTED.** |
| **Option C — Tiered canary with EB gates** (CHOSEN) | Stages provide graduated blast-radius increase with explicit halt criteria; EB gates at each stage allow early halt; rollback SLO ≤ 5 min preserved per stage. |
| **Option D — Defer cell-2 to post-launch.** | runtime-2 already provisioned and billed; deferral wastes ~$108/mo with no learning; pre-release governance prefers fixing forward over deferring debt. **REJECTED.** |

**Trade-offs consciously accepted.**

1. **Total Phase 5 + Phase 7 wall-clock ≈ 6–8 weeks** (1h + 24h dev + recruitment + 7d × 4 stages). Acceptable: cell-2 confidence depends on observed soak time, not engineering convenience.
2. **Customer involvement in production canary.** Email-based opt-in introduces friction; mitigated by clear comms (C23) and ≤5 min rollback SLO (C4).
3. **Stateful tenant exclusion (C5)** delays multi-cell coverage for stateful workloads. Acceptable; separate decision required for stateful migration.
4. **DynamicRouter 60s refresh skew accepted within ≤5 min rollback SLO** (C2 + C21). Tested in dev rehearsal (C4).
5. **Manual halt-and-rollback** (no autonomous rollback). Acceptable in pre-release; revisit at GA scale.

**Risks accepted (with mitigation + owner).**

| Risk | Sev | Prob | Mitigation | Owner |
|------|-----|------|------------|-------|
| Missing `cells` table breaks T5.3 seed | HIGH | confirmed | C1 DDL + seed in 000003 | infra + api |
| 60s refresh window non-determinism | MED | LOW | C2 invariant + test | dist-sys |
| Stage 5a 10× jump exceeds blast-radius tolerance | HIGH | MED | C3 sub-staging (1/3/5/10%) | sre |
| Rollback SLO unverified under load | HIGH | MED | C4 dev rehearsal pre-Stage-4 | sre |
| Stateful tenant data loss on re-pin | HIGH | MED if not excluded | C5 exclusion + C12 comms | prod-design |
| Rollback fails when cell-2 unreachable | HIGH | LOW | C6 cell-2-independent rollback | prod-design |
| Per-cell domain blocks 5-min SLO | HIGH | LOW | C7 shared-domain adopted | prod-design |
| Old cell-2 creds remain valid | HIGH | LOW | C8 DOKS-side revocation | security |
| Kubeconfig leak in logs | MED | LOW | C9 redaction + regression | security |
| Cross-cell tenant data leak | CRITICAL | LOW | C10 isolation verification at Stage 1 | security |
| Authz bypass on cell flag | CRITICAL | LOW | C11 3-path test | security |
| ArgoCD Secret format regression | MED | LOW | C12 invariant assert | security |
| Cell-2 capacity insufficient for Stage 5 | HIGH | MED | C13 node-pool max verified | sre |
| Observability gap | HIGH | MED | C14 full metric/alert spec | sre |
| Halt fires but rollback not actioned | HIGH | LOW | C15 PagerDuty + manual rollback runbook | sre |
| Stage transition at silent regression | MED | LOW | C16 pre-stage smoke | sre |
| Halt event without RCA | MED | LOW | C17 RCA template | sre |
| Prod fails after dev GREEN | HIGH | MED | C18 prod-failure runbook | sre |
| Cell unavailable returns 500 not 503 | MED | LOW | C19 explicit error code | dist-sys |
| Cell removal mid-flight breaks pinned tenants | MED | LOW | C20 503 + runbook | dist-sys |
| Multi-replica refresh skew | LOW | confirmed | C21 noted in C4 SLO | dist-sys |
| Unauthorized users discover --cell | LOW | confirmed | C22 hidden flag | dx |
| Customer comms ambiguous | MED | LOW | C23 template | dx |
| Exit code regression | LOW | LOW | C24 Rule 4 compliance | dx |
| JSON shape break | MED | LOW | C25 additive only | dx |
| Rollback wrong tenant | MED | LOW | C26 confirmation | dx |
| `cells` table missing capacity columns | MED | confirmed | C27 DDL covers | infra |
| Autodeploy fires during halt | MED | LOW | C28 503 + retry | api |
| Cells table corruption | MED | LOW | C29 PITR + runbook | sre + infra |

**Risks NOT accepted (block until mitigated):** none — every risk has mitigation, owner, validation test.

**Mandatory actions before implementation.**

1. **C1.** Migration 000003 creates `cells` table (DDL) AND seeds idempotently (cell-1 + cell-2). Forward-only; no `DROP`.
2. **C2.** Routing determinism invariant declared + tested (`test_router_resolves_consistently_within_request`).
3. **C3.** Phase 7 amended: Stage 5a sub-staged 1%/3%/5%/10% with 24h holds if prod tenant count > 50.
4. **C4.** Pre-Stage-4 dev rehearsal: 100 tenants × 10-rollback measurement. P50 ≤ 5 min; P95 ≤ 8 min. SLO blocks Stage 4 entry.
5. **C5.** Stateful tenant exclusion enforced in Stage 4 recruitment script.
6. **C6.** Rollback procedure cell-2-independent (re-pin via PG + ArgoCD app destination edit; does not require cell-2 reachability).
7. **C7.** Shared-domain strategy adopted (`*.dev.loopzero.example`, `*.loopzero.app`). No per-cell DNS.
8. **C8.** DOKS-side credential revocation evidence (old cert returns 401).
9. **C9.** Kubeconfig redaction + regression test.
10. **C10.** Stage 1 acceptance includes cross-cell isolation verification (logs + traffic).
11. **C11.** Authz tested in 3 paths (CLI, API direct, autodeploy); bypass = HARD VETO.
12. **C12.** ArgoCD cluster Secret invariant verified (base64 PEM caData) at Stage 1.
13. **C13.** Cell-2 node-pool max ≥ 8 verified pre-Stage-4.
14. **C14.** Metric set + alert set per SRE spec committed in `docs/runbooks/multi-cell-canary.md`.
15. **C15.** PagerDuty alert routing + manual rollback runbook + canary `--proceed` flag.
16. **C16.** Pre-stage smoke (5-min synthetic load) before each transition.
17. **C17.** RCA template `docs/runbooks/multi-cell-rca-template.md`.
18. **C18.** Prod-failure runbook `docs/runbooks/multi-cell-prod-failure.md` (4-step procedure above).
19. **C19.** Cell unavailable → 503 with `Retry-After`; not 500. Test: `test_api_returns_503_on_cell_unavailable`.
20. **C20.** Cell removal mid-flight → 503 + `docs/runbooks/cell-removal-emergency.md`.
21. **C21.** C4 SLO measurement includes refresh-skew (60s).
22. **C22.** `--cell` flag hidden by default; `--advanced` reveals; error message guides non-ops to support.
23. **C23.** `docs/customer-comms/cell-2-canary-template.md` matches spec (subject + 5-section body + opt-in/out + status page note); product language only (Rule 3).
24. **C24.** Exit codes 1/3/4 only on cell-pin failures (Rule 4).
25. **C25.** `loopzero platform status --json` adds `cells[]` field; CHANGELOG `[Added]`.
26. **C26.** CLI rollback echoes pre/post cell_id; errors if not authorized for tenant.
27. **C27.** `cells` table DDL includes `max_tenants`, `capacity_pods` columns.
28. **C28.** Autodeploy honors halt-mode (503 + Retry-After); test: `test_autodeploy_503_during_halt`.
29. **C29.** PG PITR confirmed for cells table; runbook published.

**SLI/SLO gates (per stage).** Per SRE table above, committed verbatim into `docs/runbooks/multi-cell-canary.md`.

**Rollback procedure (per stage).**

- **Stage 1 (1 synthetic dev tenant, 1h):** halt criterion fires → execute `loopzero deploy --cell cell-default <synthetic-tenant>` → verify HTTP 200 from cell-1 within 5 min → RCA → no further stages until fix.
- **Stage 2 (10% dev, 24h):** halt → batch re-pin all 10% to cell-default via canary script `--rollback` flag → verify cell-1 EB stable post-rollback → RCA → fix → re-run Stage 2 from start (no skip-ahead).
- **Stage 4 (1 prod customer, 7d):** halt → `loopzero deploy --cell cell-default <tenant>` → customer email + status page → RCA per C18 → 7-day dev re-soak before retry.
- **Stage 5a/b/c (prod 10/50/100%):** halt → batch re-pin via canary script → status page incident → RCA → 7-day dev re-soak + Stage 4 re-run before retry.
- **Universal:** rollback SLO ≤ 5 min P50, ≤ 8 min P95 (C4); cell-2 cluster reachability not required (C6); all rollback paths log to `docs/audits/multi-cell-rollback-log.md`.

**Customer comms plan (production canary).** Per C23, template requirements:

- **Subject:** `[LoopZero] Action requested: opt-in for cell-2 canary on <tenant-name>`.
- **Body sections (≤400 words):** (1) what's changing, (2) what we're testing, (3) blast radius (1 tenant, 7-day hold, immediate rollback), (4) opt-in/opt-out mechanism, (5) contact + status page link.
- **Opt-in:** explicit email reply + signed acknowledgment (1-page, no legal release). No silent or web-form opt-in.
- **Opt-out:** any time; rollback ≤ 5 min; no penalty.
- **Status page:** changelog entry only at Stage 5c GA. Canary stages are ops-internal.
- **Rule 3 language:** no "ArgoCD", "GitOps", "K8s", "cluster" — use "infrastructure pool", "isolated environment", "service tier".

---

## Stage 5 — Execution Contract

**Owner (joint).** sre-engineer (lead) + infra-engineer + api-engineer + cli-engineer + platform-security-engineer + dx-engineer.

**Deliverables.**

| # | Artifact | Path | Owner |
|---|----------|------|-------|
| D1 | This decision record | `docs/decisions/MEET-INFRA-NORMALIZE-T3-multi-cell.md` | orchestrator |
| D2 | `cells` table DDL + seed | `api/internal/db/migrations/000003_cells_table_and_seed.up.sql` | infra + api |
| D3 | Pulumi cell-2 config | `infra/pulumi/Pulumi.dev.yaml` (+ prod) — cellId, runtime2* keys | infra |
| D4 | Pulumi outputs | `infra/pulumi/src/index.ts` — runtime2Kubeconfig, runtime2CellId | infra |
| D5 | Pulumi component update | `infra/pulumi/src/components/infra.ts` — read cell-2 metadata | infra |
| D6 | Cred rotation script | `infra/scripts/rotate-cell-credentials.sh` | infra + security |
| D7 | Cred rotation runbook | `docs/runbooks/cell-credentials-rotation.md` | security |
| D8 | DynamicRouter metrics | `api/internal/placement/dynamic_router.go` instrumentation + `api/internal/metrics/cell_metrics.go` | api |
| D9 | Multi-cell dashboard | `infra/helm/monitoring/dashboards/multi-cell.json` | sre |
| D10 | Multi-cell alerts | `infra/helm/monitoring/templates/multi-cell-alerts.yaml` | sre |
| D11 | Alert runbooks | `docs/runbooks/multi-cell-alerts.md` | sre |
| D12 | CLI `--cell` flag | `cli/src/register/deploy.ts` + `cli/src/commands/deploy.ts` | cli |
| D13 | API authz | `api/internal/services/cell_authz.go` + `api/internal/routes/builds.go` | api |
| D14 | Canary stage 1 script | `infra/tests/parity/cell2-canary-stage1.sh` | sre |
| D15 | Canary stage 2 script | `infra/tests/parity/cell2-canary-stage2.sh` (with `--rollback` + `--proceed`) | sre |
| D16 | Canary stage 4-5 scripts | `infra/tests/parity/cell2-canary-prod-stage{4,5a,5b,5c}.sh` | sre |
| D17 | Canary runbook | `docs/runbooks/multi-cell-canary.md` (SLI/SLO table + halt criteria + rollback per stage) | sre |
| D18 | Prod canary runbook | `docs/runbooks/multi-cell-canary-prod.md` | sre |
| D19 | Customer comms template | `docs/customer-comms/cell-2-canary-template.md` (Rule 3 reviewed) | dx |
| D20 | RCA template | `docs/runbooks/multi-cell-rca-template.md` | sre |
| D21 | Prod-failure runbook | `docs/runbooks/multi-cell-prod-failure.md` | sre |
| D22 | Cell-removal runbook | `docs/runbooks/cell-removal-emergency.md` | sre |
| D23 | Cells PITR runbook | `docs/runbooks/cells-table-recovery.md` | infra + sre |
| D24 | Pre-stage smoke script | `infra/tests/parity/cell2-prestage-smoke.sh` | sre |
| D25 | Stateful exclusion script | `infra/scripts/list-stateful-tenants.sh` | infra |
| D26 | Rollback rehearsal | dev exercise on 100 tenants × 10-rollback measurement | sre |
| D27 | CLI status JSON update | `cli/src/installer/status.ts` (cells field) + tests | cli |
| D28 | CHANGELOG | `[Unreleased]` `[Added]` cells field; `[Added]` --cell flag (advanced); product language | doc |
| D29 | ADR-0023 | `docs/decisions/ADR-0023-multi-cell-activation.md` (prescriptive, finalized post-Stage-5c) | architect |
| D30 | Tests T-01..T-22 | per matrix below | per owner |

**Test matrix.**

| ID | Layer | What it validates | Owner |
|----|-------|-------------------|-------|
| T-01 | migration | 000003 creates cells table + seeds cell-1, cell-2; idempotent | api |
| T-02 | unit | DynamicRouter `Resolve` deterministic within request | dist-sys |
| T-03 | unit | Cell unavailable → 503 + Retry-After (C19) | api |
| T-04 | unit | Cell removal mid-flight → 503 (C20) | api |
| T-05 | unit | Authz bypass blocked CLI/API/webhook (C11) | security |
| T-06 | unit | Kubeconfig redacted in logs (C9) | security |
| T-07 | integration | ArgoCD Secret base64 PEM caData (C12) | security |
| T-08 | unit | Exit codes 1/3/4 on cell-pin failures (C24) | cli |
| T-09 | unit | `loopzero platform status --json` cells[] additive (C25) | cli |
| T-10 | unit | CLI rollback echo + ownership check (C26) | cli |
| T-11 | unit | `--cell` hidden by default (C22) | cli |
| T-12 | unit | Autodeploy 503 during halt (C28) | api |
| T-13 | parity | Stage 1 1h soak GREEN; cross-cell isolation 0 leaks (C10) | sre |
| T-14 | parity | Stage 2 24h soak; cell-1 EB < 0.1% (D17 SLI/SLO) | sre |
| T-15 | parity | Pre-Stage-4 rehearsal: rollback P50 ≤ 5min, P95 ≤ 8min (C4) | sre |
| T-16 | parity | Pre-Stage-4: cell-2 node-pool max ≥ 8 (C13) | sre |
| T-17 | parity | Stage 4 7d soak GREEN; tenant availability ≥ 99.5% | sre |
| T-18 | parity | Stage 5a 7d at 10% (or sub-staged); EB < 1% | sre |
| T-19 | parity | Stage 5b 7d at 50%; EB < 1% | sre |
| T-20 | parity | Stage 5c 7d at 100%; EB < 1% | sre |
| T-21 | observability | All cell metrics scraped + dashboards rendering | sre |
| T-22 | regression | All 1079 CLI tests + 41 Go packages pre + post activation PASS | ci |

**Validations.**

- `task test` GREEN (T-01..T-12).
- `task lint` + `task validate:manifests` GREEN.
- `promtool check rules` GREEN on D10 alerts.
- T-13 (Stage 1) GREEN before Stage 2 entry.
- T-14 (Stage 2) GREEN + T-15 (rehearsal) GREEN before Stage 4 entry.
- T-16 (capacity) GREEN before Stage 4.
- T-17 (Stage 4) GREEN before Stage 5a.
- T-18/T-19/T-20 (Stage 5a/b/c) GREEN sequentially.
- T-21 GREEN throughout.
- T-22 GREEN at every stage transition.

**Definition of Done.**

- [ ] All 29 conditions C1–C29 satisfied with committed evidence.
- [ ] Test matrix T-01..T-12 GREEN before any cell-2 traffic.
- [ ] T-13 GREEN before Stage 2.
- [ ] T-14 + T-15 GREEN before Stage 4.
- [ ] T-16 GREEN before Stage 4.
- [ ] T-17 GREEN before Stage 5a.
- [ ] T-18/T-19/T-20 GREEN sequentially.
- [ ] T-21 GREEN throughout.
- [ ] CHANGELOG `[Unreleased]` updated at Stage 5c GA (status-page note).
- [ ] ADR-0023 (prescriptive) committed at Stage 5c GA.
- [ ] Customer comms (D19) sent + acknowledged at Stage 4.
- [ ] No `OperatorTopologyMixed`, no `MultiCellRouterErrorRate`, no `CellUnhealthy` firing for 7 days post-Stage-5c.
- [ ] PR descriptions for Phase 5 + Phase 7 reference this decision record.
- [ ] Post-rollout: per-cell EB consumption normal; restart-rate per cell < 5/h; cross-cell isolation verified weekly for 4 weeks post-GA.

---

## Verdict — Final

**APPROVED_WITH_CONDITIONS.**

- **Production-design veto:** lifted CONDITIONAL on **C4 + C5 + C6 + C7 + C18**. Rollback SLO measured under load + stateful exclusion + cell-2-independent rollback + shared-domain + prod-failure runbook.
- **Security veto (PARTIAL):** lifted CONDITIONAL on **C8 + C9 + C10 + C11 + C12**. Without C11 (authz bypass test), security veto REINSTATED.
- **SRE veto:** lifted CONDITIONAL on **C13 + C14 + C15 + C16 + C17 + C18 + SLI/SLO table committed**.
- **Distributed-systems veto:** lifted CONDITIONAL on **C2 + C19 + C20 + C21**.
- **Architect veto:** none. Tiered canary is correct architecture.
- **DX veto:** lifted CONDITIONAL on **C22 + C23 + C24 + C25 + C26**.
- **Orchestrator self-challenge:** **C1 + C3 + C27 + C28 + C29** added.

**Phase 5 implementation may begin only after:**

1. This decision record committed to `docs/decisions/`.
2. Plan `infra-normalize-multicell-plan.md` updated to reference C1–C29 in T5.1–T5.7 + T7.1–T7.2 task lists.
3. Sibling decision **MEET-INFRA-NORMALIZE-T3-operator-split** GREEN (cell-2 discovery depends on split `loopzero-platform-operator`).
4. `cells` table DDL committed (C1).
5. Test matrix T-01..T-12 written in failing state (TDD red).
6. Cred rotation script + runbook drafted (D6 + D7).
7. SLI/SLO table committed in `docs/runbooks/multi-cell-canary.md`.

**Phase 7 (production rollout) implementation may begin only after:**

1. Phase 5 GREEN (Stage 1 + Stage 2).
2. T-15 dev rehearsal GREEN (rollback SLO under load).
3. T-16 capacity gate GREEN.
4. C13/C18/C23 deliverables committed.
5. Customer comms approved by DX + product.
6. PagerDuty + on-call rotation configured.

**This decision record is binding. Any deviation requires a new T3 supplementary meeting.**

---

## References

- **Plan:** `/home/paulo/loopzero/docs/plans/infra-normalize-multicell-plan.md` — ADR D3 (lines 107–135), Phase 5 / T5.1–T5.7 (lines 1278–1637), Phase 7 / T7.1–T7.2 (lines 1747–1813).
- **Sibling decisions:**
  - `/home/paulo/loopzero/docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (D1; cell-2 discovery dependency).
  - `/home/paulo/loopzero/docs/decisions/MEET-INFRA-NORMALIZE-T3-pulumi-consolidation.md` (D2; canonical Pulumi).
- **Code:**
  - `/home/paulo/loopzero/api/internal/placement/dynamic_router.go` (router, stale-data protection MEET-053 A-03).
  - `/home/paulo/loopzero/api/internal/placement/k8s_cell_store.go` (ClusterCredential discovery).
  - `/home/paulo/loopzero/api/internal/placement/router.go` + `placement_test.go` (existing tests).
  - `/home/paulo/loopzero/api/cmd/setup_background.go:104-151` (router instantiation; LOOPZERO_DYNAMIC_ROUTER default true).
  - `/home/paulo/loopzero/api/internal/config/config.go:199-205,374` (DynamicRouter + SingleCell flags).
  - `/home/paulo/loopzero/api/internal/metrics/metrics.go:291-302` (CellTenantCount + CellDiscoveryCount existing).
  - `/home/paulo/loopzero/operators/internal/controller/clustercredential_controller.go` (cluster Secret reconciler; MEET-fix-6 base64 PEM).
- **Migrations:**
  - `/home/paulo/loopzero/api/internal/db/migrations/000001_initial_schema.up.sql` (cell_id columns + indexes + trigger; **no cells table**).
  - `/home/paulo/loopzero/api/internal/db/migrations/000002_seed_data.up.sql`, `000004_preview_environments.up.sql`, `000005_release_app_attestations.up.sql` (no cells DDL).
- **Pulumi:**
  - `/home/paulo/loopzero/infra/pulumi/Pulumi.dev.yaml` (no cellId/runtime2* today).
  - `/home/paulo/loopzero/infra/pulumi/src/components/infra.ts:160-205` (runtime-2 provisioning + trustedK8sClusters).
- **Sibling ADRs:**
  - `/home/paulo/loopzero/docs/decisions/ADR-0015` — adopt-or-create (cell-2 cluster Secret + ESO ClusterSecretStore).
  - `/home/paulo/loopzero/docs/decisions/ADR-0017-argocd-auto-sync-evidence.md`.
  - `/home/paulo/loopzero/docs/decisions/ADR-0018-tenant-namespace-sot.md`.
  - `/home/paulo/loopzero/docs/decisions/ADR-0020-infra-parity-decisions.md` (D2 canonical Pulumi).
- **Capacity context:**
  - `/home/paulo/loopzero/docs/runbooks/sprint8-l3-stabilization.md` B18 (runtime pool sizing — 3→8 nodes for L3 burst).
- **Project rules:**
  - `/home/paulo/loopzero/CLAUDE.md` (Rule 3 product language, Rule 4 exit codes, Rule 5 JSON contract, Rule 10 local equals production).
  - `/home/paulo/loopzero/.claude/rules/workflow.md` (Git Rules — Inviolable; rollback via reverse-patch commit only).
  - `/home/paulo/loopzero/.claude/rules/pre-release-governance.md` (T3 trigger — tenant isolation; pre-release bias).
- **Meeting precedent:**
  - `/home/paulo/loopzero/docs/meetings/records/MEET-2026-04-18-053.md` (DynamicRouter A-03 stale-data protection origin).
