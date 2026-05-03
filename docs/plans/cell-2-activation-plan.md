# Plan: Cell-2 Activation (Dev Canary — Stages 1 & 2)

> **Version 1.0** — Activate `cell-2` as the second routable cell in the dev environment by (1) achieving local-k3d parity with the production runtime-2 cluster, (2) verifying every condition C1–C12 from the APPROVED `MEET-INFRA-NORMALIZE-T3-multi-cell` ADR before any data-plane flip, (3) flipping `cells.status` from `maintenance → active` for `cell-2`, and (4) executing the dev canary Stages 1 (1h synthetic) and 2 (24h, 10% dev tenants) with auto-rollback on halt criteria. Production Stages 4 and 5 (real customer) are explicitly out of scope and require a separate readiness review (Sprint 9). The expected outcome is `loopzero_cell_discovery_count ≥ 2` on dev with measured rollback SLO ≤ 5 min P50, advancing the maturity score from 3.7 toward 4.0 (path action: "Cell-2 activation + real chaos test").

## Context

**Today (2026-05-02).** The multi-cell platform is GREEN at 3 cells discovered locally (`cell-default + cell-1 + cell-2` per memory `reference_multicell_3cell_2026_05_01.md`), but `cell-2` is in `status='maintenance'` (migration `000006_cells.up.sql:124-129`), so `CellPinner.Pin` rejects every pinning attempt. Production has the runtime-2 DOKS cluster bootstrapped (`maturity-assessment.md:24` "4 DOKS bootstrapped fresh"), but the cell sits idle. Most of the load-bearing code already exists:

- **Database.** `cells` table seeded (cell-default, cell-1, cell-2-maintenance). `tenants.cell_id` populated by trigger.
- **Routing.** `DynamicRouter` (`api/internal/placement/dynamic_router.go`) refreshes 60s, has stale-data protection (MEET-053 A-03), emits `loopzero_cell_routing_decisions_total` + `loopzero_cell_discovery_count`.
- **Discovery.** `K8sCellStore` (`api/internal/placement/k8s_cell_store.go`) reads ClusterCredential CRs by `spec.cellID` (NOT CR name — MEET-053 B9).
- **Pinning.** `CellPinner.Pin` (`api/internal/services/cell_pinner.go:117-133`) updates `tenants.cell_id` in SERIALIZABLE TX + MergePatch on Tenant CR atomically.
- **API + CLI.** `POST /api/v1/admin/tenants/pin` (`api/internal/routes/admin_pin_cell.go`), `loopzero admin tenants pin --tenant <id> --cell <id>` (`cli/src/register/infra.ts:497-560`). Both gated behind `LOOPZERO_SERVICE_AUTH` + `LOOPZERO_ADMIN_API=true`.
- **Canary scripts.** `infra/tests/parity/cell2-canary-stage1.sh` (1h synthetic) + `cell2-canary-stage2.sh` (24h, 10%).
- **Runbooks.** `multi-cell-canary.md` (dev), `multi-cell-canary-prod.md` (prod), `multi-cell-alerts.md` (7 alerts), `multi-cell-rca-template.md`.
- **Heal script.** `task k3d:heal` codifies H1–H13 but NOT cell-2 ClusterCredential bootstrap (no H14).
- **Governance.** `docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md` APPROVED_WITH_CONDITIONS (2026-04-30) with 12 conditions C1–C12.

**Evidence motivating activation NOW.**

1. `docs/maturity-assessment.md:59` — "Cell-2 activation + real chaos test" listed as +0.2 path-to-4.0 action.
2. Memory `reference_multicell_activation_2026_04_30.md` — single-cell mode formally DELETED (-543 LOC); platform commits to multi-cell as the only mode. Cell-2 activation closes the loop.
3. `CLAUDE.md` "Not built" section explicitly lists "Cell-2 activation (cell-2 seeded as `maintenance`, ClusterCredential wired, but not routed — activation pending Sprint 9)".
4. Memory `reference_multicell_3cell_2026_05_01.md` — `count=3` GREEN as of 2026-05-01; cell-2 is reachable but rejecting pin requests.
5. `MEET-INFRA-NORMALIZE-T3-multi-cell` line 23 — "code path exists; data does not" — the activation work is data-plane (cells flip + canary), not code.

**Evidence of gaps that this plan must close.**

- `infra/k3d/` has 3 configs (mgmt, build, workload). **No `workload-2-cluster.yaml`** for local cell-2. Stage 1 canary script `cell2-canary-stage1.sh:90-95` requires `kubectl get clustercredential cell-2 -o jsonpath='{.status.connected}' = true` on mgmt — locally this returns empty because no second runtime k3d cluster exists.
- `infra/scripts/k3d-cross-cluster-heal.sh` H12 (cell-1) + H13 (cell-default) handle ClusterCredential drift but NOT cell-2. Local re-runs of `task k3d:heal` will not bootstrap cell-2.
- ADR conditions C1–C12 listed as governance requirement but never executed against running infra. C2 (routing determinism), C4 (rollback SLO), C7 (ingress-domain), C8–C12 (security) need explicit verification scripts/tests.

## Objective

**Done = `cells.status='active'` for cell-2 in dev, with cell-2 healthy across the full Stage 1 (1h) + Stage 2 (24h) canary, the rollback SLO measured ≤ 5 min P50, and 12/12 ADR conditions verified GREEN, with closure sections appended to the dev canary runbook and CHANGELOG bumped.**

Specific, measurable goals:

1. Local k3d cluster `loopzero-workload-2` exists, attached to `k3d-loopzero` shared network, with ClusterCredential CR `spec.cellID=cell-2` `status.connected=true`.
2. `loopzero_cell_discovery_count ≥ 2` on local dev (validates DynamicRouter discovers cell-2).
3. `task k3d:heal` includes H14 — idempotent cell-2 ClusterCredential bootstrap; matrix entry added.
4. All 12 ADR conditions C1–C12 GREEN per master verification script `infra/tests/parity/cell2-activation-gates.sh`; verbose output committed for audit trail.
5. Routing determinism regression test passes (C2): same `(tenant_id, cell_id)` resolves to identical `CellConfig` over 100 consecutive `Resolve` calls without re-pin.
6. Rollback SLO measured: `loopzero admin tenants pin --tenant X --cell cell-1` → first HTTP 200 from runtime-1 ≤ 300s P50, ≤ 480s P95.
7. Stage 1 (T5.6) GREEN: 1 synthetic tenant on cell-2, 1h soak, 0 5xx blips, cell-1 EB consumption < 0.05% delta.
8. Stage 2 (T5.7) GREEN: 10% dev tenants on cell-2, 24h soak, cell-2 5xx rate < 1.0% in any 10min window, cell-1 EB consumption < 1.0%.
9. CHANGELOG `[Unreleased]` `Added`: cell-2 activated as second routable cell in dev.
10. Memory `reference_cell2_activation_<date>.md` written with closure evidence.

## ADRs

### D1 — Implement APPROVED ADR; do NOT re-open governance

**Decision.** Treat `MEET-INFRA-NORMALIZE-T3-multi-cell` (APPROVED_WITH_CONDITIONS, 2026-04-30) as the binding architectural decision. This plan implements that ADR's Phase 5 (Stages 1 + 2) verbatim. No new T3 meeting; no scope changes; no re-litigation of trade-offs. Production Stages 4–5 require a SEPARATE readiness review and are out of scope here.

**Rationale.** The ADR consumed full T3 review (sponsor + challenger + 2 vetoes + 2 specialists), encoded 12 conditions, and captured trade-offs against options A/B. Re-opening would be governance churn. Pre-release governance rule (`.claude/rules/pre-release-governance.md`): "If T2/T3: STOP. Do not write code." But the ADR is APPROVED — implementation is the next step.

**Consequences.** Enables: implementing without coordination overhead; carries the 12 conditions as forced verification work. Constrains: any deviation from the canary stage definitions (durations, halt thresholds, rollback path) requires a new T3 meeting. If Stage 1 or Stage 2 surfaces a finding that contradicts the ADR (e.g., rollback SLO > 5 min consistently), HALT and escalate per `change-governor` flow.

### D2 — Local k3d parity is a hard prerequisite for dev canary

**Decision.** Provision a `loopzero-workload-2` k3d cluster locally before any cells.status flip. Do NOT proceed to Phase 2 (status flip) without local 3-cell GREEN.

**Rationale.** The dev canary scripts `cell2-canary-stage1.sh` + `cell2-canary-stage2.sh` were authored against the production-shape topology (mgmt + build + runtime + runtime-2). Running them against a local 3-cluster topology (mgmt + build + workload only) will fail preflight (`kubectl get clustercredential cell-2` returns empty). Skipping local validation and going straight to production violates Rule 10 ("Local equals production") and the `task k3d:heal` invariant (4 cells discoverable with `count >= 2`).

**Alternatives rejected.**
- (A) Mock cell-2 in tests only. Rejected: doesn't exercise routing determinism (C2) or rollback SLO (C4) under real network conditions.
- (B) Skip dev canary, go to prod Stage 4. Rejected: ADR mandates Stage 1+2 BEFORE Stage 4 (`MEET-INFRA-NORMALIZE-T3-multi-cell.md:34-38`); production traffic must not be the first thing cell-2 sees.

**Consequences.** Enables: real load on cell-2 before any tenant traffic; reproducible regression on every workstation. Constrains: developer disk usage (additional ~3GB k3d cluster); requires `task dev:up` to start 4 clusters (was 3); CI runtime grows ~30s per pipeline.

### D3 — Shared ingress domain for both cells (re-pin without DNS change)

**Decision.** Both cell-1 and cell-2 serve under the SAME wildcard domain in dev (`*.loopzero.local`) and production (`*.dev.loopzero.example` / `*.loopzero.app`). Routing happens at Cloudflare → mgmt API → DynamicRouter → cell, NOT via per-cell subdomains. This locks in the C7 condition from the ADR (production-design-reviewer veto).

**Rationale.** The ≤5 min rollback SLO (C4) is impossible if a re-pin requires DNS change (CF Tunnel route rewrite + propagation 5–60min). Shared-domain mode means a re-pin is purely an API/PG update — Cloudflare and CF Tunnel don't notice.

**Alternatives rejected.**
- Per-cell subdomain (`*.cell2.dev.loopzero.example`). Rejected: violates C4. SLO becomes "P50 ≤ 30 min" because of DNS propagation; ADR rejected this in Stage 2.

**Consequences.** Enables: rollback as a pure data-plane op. Constrains: tenant URL is identical across cells — observability dashboards MUST disambiguate by `cell_id` label, not by hostname (already enforced by `loopzero_cell_routing_decisions_total{cell, decision}`). If a future cell needs region-affinity DNS, this ADR must be re-opened.

### D4 — Heal script H14 codifies cell-2 bootstrap (no manual `kubectl`)

**Decision.** Add an idempotent `H14` block to `infra/scripts/k3d-cross-cluster-heal.sh` that creates the cell-2 ClusterCredential locally — symmetric to H12 (cell-1). Every `task k3d:heal` converges to {cell-default, cell-1, cell-2} ClusterCredentials; manual `kubectl apply` is forbidden.

**Rationale.** Memory `feedback_no_tribal_knowledge.md` — "Every `kubectl`/`docker`/`helm` manual fix MUST become a committed idempotent script + matrix entry + memory before session ends. Trigger: stop after the THIRD repetition of the same logical fix." Bootstrapping cell-2 manually three times across sessions = signal to codify.

**Alternatives rejected.**
- (A) Document manual steps in runbook only. Rejected: violates `feedback_no_tribal_knowledge.md`. Operators forget.
- (B) Add to `wire-clusters.sh` only. Rejected: that script runs as Helmfile post-sync hook, not on every dev iteration. Heal must work even when Helmfile didn't run yet.

**Consequences.** Enables: idempotent recovery from cell-2 disconnect on every workstation. Constrains: `clustercredential-create.sh` must remain stable (third H-block depending on it after H12+H14). Matrix entry must be added to `validate-matrix.sh` (so CI catches drift if H14 silently breaks).

### D5 — Activation gates run as a single script (gate-or-don't, no partial)

**Decision.** All 12 ADR conditions C1–C12 verified via a single master script `infra/tests/parity/cell2-activation-gates.sh` that runs each gate, prints PASS/FAIL with evidence, and exits 0 only if 12/12 PASS. The cells flip CANNOT be done by hand — it's gated behind this script's exit 0.

**Rationale.** ADR conditions are not optional; partial verification is how production incidents start. A single gate script makes "did we verify everything?" a one-line answer (`bash gates.sh; echo $?`). Memory `reference_validation_sweep_2026_04_30.md`: "validation gates work but only when exercised."

**Alternatives rejected.**
- (A) 12 separate scripts, operator runs them ad-hoc. Rejected: tribal knowledge; operator may skip C9 if "looked obvious".
- (B) Make gates run automatically on `cells.status='active'` UPDATE (DB trigger). Rejected: gates require live K8s queries + Prometheus queries — not a DB-trigger concern.

**Consequences.** Enables: one-command pre-flight; CI can run it on PR that touches multi-cell code; audit trail is the script's stdout. Constrains: every new ADR condition added in future T3 meetings MUST add a gate function here (or the script becomes a lie).

### D6 — Out-of-scope: Production Stages 4 + 5 (require fresh readiness review)

**Decision.** This plan covers ONLY dev canary (Stages 1 + 2). Production stages — Stage 4 (1 customer, 7-day) and Stages 5a/5b/5c (10/50/100% production) — are EXPLICITLY out of scope and require a fresh `meeting-orchestrator` review with: customer-comms templates, customer recruitment, on-call rotation alignment, and the 7-day hold cadence. Plan to schedule that meeting AFTER Stage 2 GREEN.

**Rationale.** Production canary = different blast radius class (real revenue at risk). The 7-day hold per stage means total minimum time-to-100% is 7+7+7+7 = 28 days. That is a sprint-level undertaking with unique gates (e.g., "burn-rate alert tuned per customer SLO"), distinct from the dev canary which is a 25-hour technical exercise.

**Alternatives rejected.**
- (A) Bundle dev + prod canary. Rejected: dev surfaces technical bugs (routing, rollback timing); prod surfaces process bugs (comms, on-call). Different failure modes; different gates; distinct ownership.

**Consequences.** Enables: clean exit criterion ("Stage 2 GREEN → done"). Constrains: cell-2 sits idle in prod between Stage 2 close and Sprint 9 prod canary kickoff (cost: ~1 idle DOKS cluster ≈ $90/mo until prod canary fires).

## Dependency Graph

```
Phase 0: Pre-flight & Local Parity
  T0.1 (k3d workload-2 cluster) ──┐
  T0.2 (heal script H14) ─────────┤
  T0.3 (3-cell discovery verify) ─┘──▶ Phase 1
                                       │
Phase 1: ADR Conditions Verification ◀─┘
  T1.1 (C2 routing determinism test)        ┐
  T1.2 (C4 rollback SLO measure)            │
  T1.3 (C7 ingress-domain shared verify)    │── parallel
  T1.4 (C8/C9/C10/C11/C12 security gates)   │
  T1.5 (master gates script gates.sh)       ┘
                                       │
                                       ▼
Phase 2: Status Flip ◀──────────────────────┘
  T2.1 (pre-flip checklist sign-off)
       │
       ▼
  T2.2 (cells.status='active' UPDATE)
       │
       ▼
  T2.3 (verify discovery count ≥ 2)
       │
       ▼
Phase 3: Stage 1 (1h synthetic, T5.6)
  T3.1 (synthetic tenant bootstrap)
       │
       ▼
  T3.2 (canary-stage1.sh execute)
       │
       ▼
  T3.3 (closure section append)
       │
       ▼
Phase 4: Stage 2 (24h, 10% dev, T5.7)
  T4.1 (dry-run sample selection)
       │
       ▼
  T4.2 (canary-stage2.sh execute)
       │
       ▼
  T4.3 (closure or RCA append)
       │
       ▼
Phase 5: Closure & Documentation
  T5.1 (cell-2-activation runbook)
  T5.2 (CHANGELOG + memory + maturity bump) — parallel with T5.1
```

**Sequential blockers:** Phases 0 → 1 → 2 → 3 → 4. Within Phase 1, T1.1–T1.4 parallelize; T1.5 depends on all four. Within Phase 5, T5.1 and T5.2 parallelize.

**Parallelizable inside Phase 1:** T1.1 (Go test), T1.2 (Bash script), T1.3 (Bash + DNS check), T1.4 (5 sub-checks) — all read-only against running infra; can run concurrently.

---

## Phase 0: Pre-flight & Local Parity

**Objective:** Achieve local-k3d parity with production runtime-2 topology so dev canary scripts have a real cell-2 to talk to.

### T0.1 — Provision local k3d `loopzero-workload-2` cluster

#### Objective
Create the fourth k3d cluster (mgmt + build + workload + workload-2) so DynamicRouter can discover cell-2 locally. Mirrors the production runtime-2 topology required by `cell2-canary-stage1.sh` preflight checks.

#### Evidence
- `ls /home/paulo/loopzero/infra/k3d/` returns only `build-cluster.yaml`, `mgmt-cluster.yaml`, `workload-cluster.yaml` (no `workload-2-cluster.yaml`).
- `cell2-canary-stage1.sh:91-95` requires `kubectl get clustercredential cell-2 -o jsonpath='{.status.connected}' = true`. Without a workload-2 k3d cluster, that returns empty.
- ADR D2 above: "Local k3d parity is a hard prerequisite."

#### Files to edit
```
infra/k3d/workload-2-cluster.yaml — (NEW) k3d cluster config for cell-2 local
Taskfile.yml — modify k3d:up + k3d:down + k3d:status to include workload-2
infra/tests/parity/validate-matrix.sh — modify to expect 4 k3d clusters (was 3)
```

#### Deep file dependency analysis
- **`infra/k3d/workload-cluster.yaml` (existing)** — defines port mappings + node labels + agents. Today: 1 server + 1 agent. workload-2 must mirror this shape with distinct ports (avoid collision: workload uses 9080/9443 typically; workload-2 must use 9081/9444 or similar) and `k3d-loopzero-workload-2-server-0` container name. Downstream: `infra/scripts/k3d-cross-cluster-heal.sh:115-139` `attach_to_shared` loop iterates a hardcoded container list; H14 (T0.2) appends `k3d-loopzero-workload-2-server-0`.
- **`Taskfile.yml`** — `k3d:up` task currently creates 3 clusters via 3 `k3d cluster create` calls. Adding workload-2 = appending a 4th call. `k3d:status` lists 3; must list 4. `k3d:down` deletes 3; must delete 4. Downstream: `task dev:up` calls `k3d:up`; `task setup` is the entry point — must remain idempotent.
- **`infra/tests/parity/validate-matrix.sh`** — has matrix entries that count clusters; the 4-cluster expectation must be encoded so CI catches regressions if workload-2 silently disappears.

#### Deep Dives

**Port collision avoidance.** k3d binds host ports per cluster. `workload-cluster.yaml` typically maps `9080:80@loadbalancer` and `9443:443@loadbalancer`. Pick `9081:80@loadbalancer` + `9444:443@loadbalancer` for workload-2. If a future engineer changes workload's ports, this comment must remain visible (encode as YAML comment in workload-2-cluster.yaml referencing workload-cluster.yaml's ports).

**Resource sizing.** workload-2 in dev does NOT need to match production runtime-2 sizing (s-4vcpu-8gb × 2 nodes per Sprint 8 v3.2). For local k3d, the same resources as workload (1 server + 1 agent, default k3d sizing ~1 CPU 2GB each) suffice — canary scripts test routing correctness, not load.

**Network attachment.** All k3d clusters MUST share the `k3d-loopzero` docker network (per H1 in heal script). `network: k3d-loopzero` field in cluster YAML achieves this.

**Cluster context name.** k3d names contexts `k3d-loopzero-workload-2`. Heal script must read `RT2_CTX="${RT2_CTX:-k3d-loopzero-workload-2}"` and add to the preflight context loop.

**Ingress class.** Both workload and workload-2 use Traefik (default k3d). Both serve under `*.loopzero.local` per ADR D3 — no ingress-class divergence.

#### Tasks
1. Read `infra/k3d/workload-cluster.yaml` to capture exact shape (port map, server/agent count, registries, network).
2. Create `infra/k3d/workload-2-cluster.yaml` with `name: loopzero-workload-2`, ports `9081/9444`, `network: k3d-loopzero`, identical agents/server count.
3. Modify `Taskfile.yml` `k3d:up` to add `k3d cluster create --config infra/k3d/workload-2-cluster.yaml` (idempotent: skip if `k3d cluster list | grep -q loopzero-workload-2`).
4. Modify `Taskfile.yml` `k3d:down` to `k3d cluster delete loopzero-workload-2` (idempotent).
5. Modify `Taskfile.yml` `k3d:status` output to list 4 clusters with their context names.
6. Run `task k3d:up` and verify `kubectl --context k3d-loopzero-workload-2 cluster-info` returns 200 from API server.
7. Modify `infra/tests/parity/validate-matrix.sh` matrix entry for cluster count (was 3, now 4); add note pointing to D2.

#### TDD

```
RED:    test_k3d_up_creates_workload_2() — assert `k3d cluster list` includes "loopzero-workload-2" after `task k3d:up`
RED:    test_workload_2_on_shared_network() — assert `docker inspect k3d-loopzero-workload-2-server-0` shows `k3d-loopzero` in NetworkSettings.Networks
RED:    test_validate_matrix_expects_4_clusters() — assert `validate-matrix.sh` cluster-count gate value == 4
GREEN:  Create workload-2-cluster.yaml + Taskfile.yml edits + validate-matrix.sh update
REFACTOR: None expected (additive change — no abstraction opportunity)
VERIFY: task k3d:up && task k3d:status && bash infra/tests/parity/validate-matrix.sh
```

Tests live as bash assertions in `infra/tests/parity/cell2-activation-gates.sh` (T1.5) — not Go/TS tests, because the subject is k3d shell state.

#### Acceptance Criteria
- [ ] `kubectl --context k3d-loopzero-workload-2 get nodes` returns ≥ 1 Ready node.
- [ ] `docker network inspect k3d-loopzero --format '{{range .Containers}}{{.Name}}{{"\n"}}{{end}}'` includes `k3d-loopzero-workload-2-server-0`.
- [ ] `task k3d:up` is idempotent — second run is a no-op.
- [ ] `task k3d:down` deletes all 4 clusters cleanly.
- [ ] `validate-matrix.sh` 4-cluster gate PASSES.
- [ ] No port collision with workload (9080/9443 free; 9081/9444 bound to workload-2 LB).
- [ ] Pass: code-audit lint check (zero new warnings in Taskfile.yml YAML lint).
- [ ] Pass: kubeconform on any new YAML resource files (workload-2-cluster.yaml is k3d config, NOT a Kubernetes manifest — kubeconform skips).

#### DoD
- [ ] All tasks completed and validated
- [ ] `task k3d:up && task k3d:status` shows 4 clusters READY
- [ ] `task validate:manifests` green (no manifest changes here, but smoke-check)
- [ ] CHANGELOG `[Unreleased]` `Added`: "Local k3d cluster `loopzero-workload-2` provisioned for cell-2 dev parity" with reference to this plan.

---

### T0.2 — heal script H14 — cell-2 ClusterCredential bootstrap

#### Objective
Add an idempotent `H14` block to `infra/scripts/k3d-cross-cluster-heal.sh` that creates/refreshes the cell-2 ClusterCredential for the workload-2 k3d cluster. Symmetric to H12 (cell-1).

#### Evidence
- `infra/scripts/k3d-cross-cluster-heal.sh:489-529` H12 handles cell-1 bootstrap.
- `infra/scripts/k3d-cross-cluster-heal.sh:531-` H13 handles cell-default SA token rotation.
- No H14 exists. After T0.1 lands, every `task k3d:heal` would still leave cell-2 ClusterCredential absent locally.
- Memory `feedback_no_tribal_knowledge.md` — "Every kubectl/docker/helm manual fix MUST become a committed idempotent script."

#### Files to edit
```
infra/scripts/k3d-cross-cluster-heal.sh — append H14 block after H13
infra/tests/parity/validate-matrix.sh — append matrix entry 155 (H14 cell-2 ClusterCredential)
infra/pulumi/src/__tests__/heal-script-H14.test.ts — (NEW) vitest spec for H14 invariants (mirror H12 test)
```

#### Deep file dependency analysis
- **`infra/scripts/k3d-cross-cluster-heal.sh`** — existing structure: H1 (network), H2–H10 (various), H11 (ArgoCD token), H12 (cell-1 CC), H13 (cell-default SA token). H14 follows the SAME pattern as H12: detect connected via `kubectl get clustercredentials`, if not connected scoped-kubeconfig + clustercredential-create.sh. The cluster-name argument is `workload-2` (so `scoped-kubeconfig.sh` finds `k3d-loopzero-workload-2-server-0` docker container per B82 invariant).
- **`infra/helmfile/scripts/lib/clustercredential-create.sh`** — already supports arbitrary cell-id + cluster-name pair (`$1=cell-2 $2=workload-2 $3=scoped-kc`). No edit required; its existing contract is the dependency.
- **`infra/helmfile/scripts/lib/scoped-kubeconfig.sh`** — rewrites SERVER URL from k3d localhost-trapped form to docker bridge IP. Handles arbitrary cluster-names per B82. No edit required.
- **`infra/pulumi/src/__tests__/heal-script-H12.test.ts`** — existing vitest spec validates 19 invariants of H12. H14 mirrors with its own spec file referencing the same invariants list.

#### Deep Dives

**Why cluster-name=`workload-2`, not `runtime-2`.** Per memory `reference_multicell_3cell_2026_05_01.md` and `infra/scripts/k3d-cross-cluster-heal.sh:498-501`: "K8sCellStore uses spec.cellID (not CR name). Using cluster-name='runtime-2' would fail B82 docker-resolution (container k3d-loopzero-runtime-2-server-0 doesn't exist locally — k3d named it `loopzero-workload-2`)." Cluster-name MUST match the k3d container name suffix.

**Idempotency.** First check `kubectl --context "$MGMT_CTX" -n loopzero-system get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-2")].status.connected}'`. If `true`, skip. Same shape as H12 line 502.

**Failure path.** If `clustercredential-create.sh cell-2 workload-2 "$SCOPED"` fails (e.g., `kubectl apply` rejected by API), log `WARN` with `matrix entry 155 fix path`. Non-fatal — heal exits 0 even on H14 failure (matches H12's `|| warn` pattern). The next-level invariant "discovery count ≥ 2" is verified separately in T0.3.

**Trap order.** H14 creates a temp file `WORKLOAD2_KUBECONFIG=$(mktemp)` and `trap 'rm -f "$H14_TMP"' EXIT`. Trap chains correctly with H12's earlier trap because EXIT traps stack (each new trap appends).

**Operator override path.** If H14 misbehaves, operator can run `bash infra/helmfile/scripts/lib/clustercredential-create.sh cell-2 workload-2 <(k3d kubeconfig get loopzero-workload-2)` manually — the heal script is a wrapper, not a gatekeeper.

#### Tasks
1. Read existing H12 block (`heal-script.sh:489-529`) to capture the exact pattern.
2. Append H14 block after H13: same control flow (check connected → bootstrap if not).
3. Use `RT2_CTX="${RT2_CTX:-k3d-loopzero-workload-2}"` for k3d kubeconfig fetch.
4. Add `attach_to_shared k3d-loopzero-workload-2-server-0` (and `-agent-0`, `-serverlb`) to H1's container loop (line 136-139).
5. Update header comment block (`heal-script.sh:60-78`) to document H14 between H13 and H12 (preserve LIVE-discovery-date header style).
6. Add `RT2_CTX` line near `RT_CTX` declaration (line 91-92) so all 4 contexts probed in preflight loop (line 107-112).
7. Create `infra/pulumi/src/__tests__/heal-script-H14.test.ts` mirroring H12 test (assert: H14 string present, idempotency check shape, cluster-name="workload-2", uses `clustercredential-create.sh`).
8. Add matrix entry 155 to `validate-matrix.sh` referencing H14.

#### TDD

```
RED:    test_h14_present_in_heal_script() — grep `H14:.*ClusterCredential cell-2` in heal-script.sh; assert non-empty
RED:    test_h14_uses_workload_2_clustername() — grep `cell-2 workload-2` in heal-script.sh; assert hit
RED:    test_h14_idempotent_skip_when_connected() — grep `already connected — skip` near H14 block; assert hit
RED:    test_h14_attach_to_shared_includes_workload2_containers() — grep `k3d-loopzero-workload-2-server-0` in H1 loop; assert hit
RED:    test_h14_rt2_ctx_in_preflight() — grep `RT2_CTX` in for ctx loop; assert hit
RED:    test_validate_matrix_entry_155() — grep `^155:` in validate-matrix.sh; assert "H14 cell-2 ClusterCredential"
GREEN:  Implement H14 block + container attach + ctx preflight + matrix entry
REFACTOR: Consider extracting H12+H14 common code into bash function `ensure_cell_clustercredential <cell-id> <cluster-name>`. Defer to T0.3 closure if both blocks become >80 LOC each.
VERIFY: cd infra/pulumi && npx vitest run heal-script-H14.test.ts && bash infra/tests/parity/validate-matrix.sh
```

#### Acceptance Criteria
- [ ] `task k3d:heal` exits 0 with H14 LOG line "ClusterCredential 'workload-2' (cellID=cell-2) created/refreshed".
- [ ] Re-running `task k3d:heal` is a no-op for H14 (logs "already connected — skip").
- [ ] `kubectl --context k3d-loopzero-mgmt -n loopzero-system get clustercredentials -o jsonpath='{.items[?(@.spec.cellID=="cell-2")].status.connected}'` = `true` within 60s of first heal.
- [ ] vitest `heal-script-H14.test.ts` 100% PASS.
- [ ] `validate-matrix.sh` matrix entry 155 GREEN.
- [ ] Pass: code-audit complexity check (heal-script function `H14` cyclomatic complexity ≤ 10 — should be ≤ 6 per pattern).
- [ ] Pass: shellcheck on heal-script.sh (zero new warnings).

#### DoD
- [ ] All tasks completed and validated
- [ ] `task k3d:heal` GREEN end-to-end with 4 ClusterCredentials connected
- [ ] vitest heal-script-H14.test.ts passing
- [ ] CHANGELOG `[Unreleased]` `Added`: "heal script H14 codifies cell-2 ClusterCredential bootstrap (matrix entry 155)"

---

### T0.3 — Verify local 3-cell discovery (`loopzero_cell_discovery_count ≥ 2`)

#### Objective
Confirm DynamicRouter discovers all three cells (cell-default + cell-1 + cell-2) on local dev. This is the blocking precondition for Phase 2 status flip.

#### Evidence
- `api/internal/placement/dynamic_router.go:113-115` — `metrics.CellDiscoveryCount.Set(float64(len(newCells)))` emitted after every refresh.
- `cell2-canary-stage1.sh:101-105` — gates on `loopzero_cell_discovery_count >= 2`.
- Memory `reference_multicell_3cell_2026_05_01.md` — "count=2→3" achieved 2026-05-01 via H12 fix; this task is the regression check.

#### Files to edit
```
infra/tests/parity/validate-matrix.sh — add matrix entry 156 (cell-2 discovery)
(no production code edits — verification-only task)
```

#### Deep file dependency analysis
- **`infra/tests/parity/validate-matrix.sh`** — collects offline gates. Entry 156 = "After `task k3d:heal`, `loopzero_cell_discovery_count` must be ≥ 3 (cell-default + cell-1 + cell-2)." Queries Prometheus on `localhost:9090` (k3d port-forward via `task obs:prom-pf` or in-cluster Service).
- Downstream consumers: `cell2-canary-stage1.sh` will read this same metric in preflight; matching matrix entry pre-empts canary failure.

#### Deep Dives

**Metric label cardinality.** `loopzero_cell_discovery_count` is a Gauge (no labels) — single value reflecting last refresh. Three cells → value `3`. The check is value ≥ 3 (not == 3) because future cells (cell-3, etc.) shouldn't break this gate.

**Refresh timing.** DynamicRouter refresh interval is 60s (`api/cmd/setup_background.go` constructs `NewDynamicRouter(..., 60*time.Second, ...)`). After `task k3d:heal` lands H14, the metric updates within 60s. The matrix entry should poll for up to 90s before failing.

**Cold-start protection.** Per `dynamic_router.go:100-106` MEET-053 A-03: empty-set refresh after a non-empty cache is REJECTED (logs "refusing to clear N known cells"). So if Prometheus scraping is flaky, the metric stays at the last known good value — no false negatives in this gate.

**ClusterCredential `status.connected=true` lag.** The clustercredential-controller probes the cluster API server every 30s (or on Watch events). After H14 creates the CR, `connected` flips to `true` within 60s typically. Combined with DynamicRouter's 60s refresh, total propagation ≤ 120s.

#### Tasks
1. Add matrix entry 156 to `validate-matrix.sh` with title "Cell discovery includes cell-2 (count ≥ 3)".
2. Implement gate as: poll Prometheus `up to 90s, expect_value ≥ 3`. Fall back to direct API query if Prometheus unreachable (`curl -s "$LOOPZERO_API/api/v1/admin/cells/discovery" | jq .count`).
3. Verify gate PASSES on a fresh `task setup && task dev:up && task k3d:heal` cycle.
4. Document the gate in `validate-matrix.sh` comment with reference to this plan + ADR.

#### TDD

```
RED:    test_validate_matrix_entry_156_present() — grep `^156:` in validate-matrix.sh
RED:    test_entry_156_polls_for_90s() — grep `for i in $(seq 1 90)` near entry 156 (or equivalent timeout pattern)
RED:    test_entry_156_threshold_3() — grep `>= 3` or `-ge 3` near entry 156
GREEN:  Add entry 156 with poll + threshold + comment
REFACTOR: None expected
VERIFY: bash infra/tests/parity/validate-matrix.sh && echo "exit=$?"
```

#### Acceptance Criteria
- [ ] `validate-matrix.sh` entry 156 PASSES on a fresh dev environment (after k3d:up + k3d:heal).
- [ ] Polling logic handles Prometheus port-forward race conditions (≤ 90s timeout).
- [ ] Failure mode: clear error message including current count + which cells are missing.
- [ ] Pass: code-audit lint check (shellcheck on validate-matrix.sh).

#### DoD
- [ ] Entry 156 GREEN
- [ ] On `task k3d:heal && bash infra/tests/parity/validate-matrix.sh`, all gates pass

---

## Phase 1: ADR Conditions Verification

**Objective:** Verify each of the 12 conditions C1–C12 from `MEET-INFRA-NORMALIZE-T3-multi-cell` ADR is GREEN before any data-plane flip.

### T1.1 — C2: Routing determinism regression test

#### Objective
Prove that for a fixed `(tenant_id, cell_id)`, `DynamicRouter.Resolve(cellID)` returns the same `CellConfig` deterministically across 100+ consecutive calls without re-pin. Closes ADR condition C2.

#### Evidence
- `MEET-INFRA-NORMALIZE-T3-multi-cell.md:64` C2 — "router cache invalidation on `cells` table change, OR accept ≤60s split-routing window with documented invariant. C2 mandatory — declare invariant + test."
- `api/internal/placement/dynamic_router.go:133-149` — `Resolve` is a pure map lookup under RWMutex; deterministic by construction. But no automated test asserts this.

#### Files to edit
```
api/internal/placement/dynamic_router_test.go — add Test_Resolve_DeterministicAcrossCalls
api/internal/placement/dynamic_router.go — add doc comment "INVARIANT: Resolve is deterministic — same cellID returns same CellConfig until next refresh."
```

#### Deep file dependency analysis
- **`api/internal/placement/dynamic_router_test.go`** — existing tests cover `Resolve` for found/not-found cases. New test asserts: "100 consecutive `Resolve('cell-2')` calls return *the same struct value* (DeepEqual)" — this catches any future change that introduces non-determinism (e.g., LB across multiple replicas).
- **`api/internal/placement/dynamic_router.go`** — godoc for `Resolve` line 119-132 already explains decisions. Add explicit invariant statement above. Downstream: `cellmgr/client_manager.go:85` and `argocd/sync_client_factory.go:72` consume `Resolve` and rely on this determinism implicitly.

#### Deep Dives

**Why 100 calls.** Larger N (1000+) would test microbenchmark behavior, not determinism. 100 is enough to catch any introduced randomness or RWMutex starvation patterns (`sync.RWMutex` is FIFO-ish, but `Resolve` is read-only — should never starve).

**60s refresh interaction.** The invariant holds *between refreshes*. Test must NOT trigger a refresh during the 100 calls. Use `NewDynamicRouter(store, 24*time.Hour, ...)` so the ticker doesn't fire during the test.

**Concurrent Resolve safety.** While testing determinism, also assert no data races. Run 100 goroutines × 10 Resolves each = 1000 calls; all must return same struct. Use `-race` flag.

**What "same struct" means.** `CellConfig` is a value type (not pointer). DeepEqual compares all fields: `CellID`, `APIServer`, `IngressDomain`, `SecretRefName`, `SecretRefNamespace`. Test fixture: stub CellStore returns 3 cells with stable values.

#### Tasks
1. Read existing `dynamic_router_test.go` to capture test setup pattern.
2. Add `Test_Resolve_DeterministicAcrossCalls` (sequential 100 calls).
3. Add `Test_Resolve_DeterministicConcurrent` (1000 calls across 100 goroutines).
4. Both tests assert `reflect.DeepEqual(prev, current)` for every call after the first.
5. Add doc comment on `Resolve` declaring the invariant verbatim.
6. Run `go test -race ./api/internal/placement/...` and assert zero races.

#### TDD

```
RED:    Test_Resolve_DeterministicAcrossCalls — fails before invariant test added (currently no such test exists)
RED:    Test_Resolve_DeterministicConcurrent — same
GREEN:  Add both tests in dynamic_router_test.go; add invariant doc comment
REFACTOR: None expected (tests add coverage; production code unchanged)
VERIFY: go test -race -run "Test_Resolve_Deterministic" ./api/internal/placement/...
```

#### Acceptance Criteria
- [ ] Both new tests PASS with `-race` flag.
- [ ] `Resolve` godoc includes explicit "INVARIANT" line.
- [ ] Pass: code-audit complexity check (test functions ≤ 10 cyclomatic complexity each).
- [ ] Pass: code-audit coverage check on `placement` package (≥ 90%).
- [ ] Pass: code-audit lint check (`go vet`, `golangci-lint run` — zero new warnings).

#### DoD
- [ ] All tasks completed and validated
- [ ] `go test -race ./api/internal/placement/...` GREEN
- [ ] Invariant documented in godoc + this plan + ADR cross-reference

---

### T1.2 — C4: Rollback SLO measurement script + run

#### Objective
Measure end-to-end rollback time (re-pin → first HTTP 200 from new cell) on dev with realistic load. Establish baseline ≤ 5 min P50, ≤ 8 min P95. Closes ADR condition C4 (production-design-reviewer veto).

#### Evidence
- `MEET-INFRA-NORMALIZE-T3-multi-cell.md:73` C4 — "rollback SLO measured end-to-end (API call → first HTTP 200 from new cell) on dev with realistic load before Stage 4."
- Rollback today is implemented via `loopzero admin tenants pin --cell cell-1 <tenant>`. No measured SLO data exists.

#### Files to edit
```
infra/tests/parity/cell2-rollback-slo.sh — (NEW) measurement script, 10 iterations
docs/runbooks/multi-cell-canary.md — append "Rollback SLO baseline" section with measured values
```

#### Deep file dependency analysis
- **`cell2-rollback-slo.sh`** — driver script. Loop: bootstrap test tenant on cell-1 → pin to cell-2 → wait HTTP 200 from cell-2 → pin back to cell-1 → measure time-to-first-200 from cell-1 → record. 10 iterations to get P50/P95 distribution.
- Depends on: `loopzero admin tenants pin` CLI (`cli/src/register/infra.ts:497-`), `cellmgr/client_manager.go` (cell selection at API layer), ArgoCD reconcile (re-emits Application destination), Traefik routing (handles ingress on the new cell).
- **`docs/runbooks/multi-cell-canary.md`** — append a new section "Rollback SLO baseline (T1.2)" with table: iteration, time-to-200-cell1, time-to-200-cell2, decision (PASS/FAIL).

#### Deep Dives

**What "first HTTP 200 from new cell" means.** After re-pin, the operator's `loopzero admin tenants pin` call returns 200 within ~1s (DB UPDATE + CR MergePatch). But traffic doesn't shift until: (1) operator reconciles Tenant CR → re-emits ArgoCD Application + AppProject with new `destination.name`, (2) ArgoCD syncs (Helm template + apply), (3) Traefik picks up new Ingress on new cell, (4) DNS/CF routing already correct (D3 shared domain). Time-to-first-200 measured against `https://${TENANT}-production.loopzero.local` from a probe inside mgmt.

**Realistic load.** "Realistic" = at least 1 RPS background traffic during the rollback. Implemented as a parallel `wrk -c 1 -d 600s -R 1 https://${TENANT}-production.loopzero.local` job started before the re-pin, running for the full measurement.

**Sample size = 10.** Statistical floor for P95 estimation. ADR mentions "P95" — strictly need 20+ samples for tight P95 CI, but for SLO-establishment (not SLO-validation) 10 is enough. If results show high variance, T1.2 escalates: append iterations to 20 before passing.

**Halt criterion in measurement.** If any iteration takes > 600s (10 min), abort the script and mark FAIL — exceeds the ADR's P95 budget by 25% and indicates a non-deterministic blocker.

**Why dev, not staging.** Per ADR scope: "Stage 3 (staging) deferred — staging environment does not yet exist". Dev k3d is the only multi-cell environment available. Production measurement requires Stage 4 entry, which is gated by THIS measurement.

#### Tasks
1. Read `cell2-canary-stage1.sh` to capture the bash patterns (preflight, probe, log).
2. Create `cell2-rollback-slo.sh` with: 10-iteration loop, parallel `wrk` background, time-to-200 measurement (millisecond precision via `date +%s%N`), CSV output.
3. Run script against local k3d (after Phase 0 GREEN — depends on cell-2 reachable).
4. Compute P50, P95 from 10 samples.
5. Decision: P50 ≤ 300s AND P95 ≤ 480s → PASS; else FAIL.
6. Append "Rollback SLO baseline" section to `multi-cell-canary.md` with table + verdict.

#### TDD

```
RED:    test_rollback_slo_script_executable() — assert -x infra/tests/parity/cell2-rollback-slo.sh
RED:    test_script_supports_dry_run() — script with --dry-run exits 0 without making API calls
RED:    test_script_iteration_count_default_10() — grep `IT_COUNT=${IT_COUNT:-10}` in script
RED:    test_script_emits_p50_p95() — script output includes "P50:" and "P95:" lines
GREEN:  Implement script with --dry-run path + iteration loop + percentile calc
REFACTOR: Consider extracting probe-time-to-200 helper if used in future canary scripts. Defer to T3.2 if pattern recurs.
VERIFY: bash infra/tests/parity/cell2-rollback-slo.sh --dry-run && bash infra/tests/parity/cell2-rollback-slo.sh
```

#### Acceptance Criteria
- [ ] Script supports `--dry-run` (no API calls, validates pre-conditions).
- [ ] Live run produces 10 measurements + P50 + P95 to stdout + CSV file `/tmp/cell2-rollback-slo-<timestamp>.csv`.
- [ ] P50 ≤ 300s AND P95 ≤ 480s (per ADR C4).
- [ ] Failure mode: any iteration > 600s aborts script with exit code 4 (auth-class-fail).
- [ ] Closure section appended to `multi-cell-canary.md` with verdict + raw measurements.
- [ ] Pass: shellcheck on `cell2-rollback-slo.sh` (zero warnings).
- [ ] Pass: code-audit complexity check (any function in script ≤ 10 cyclomatic).

#### DoD
- [ ] All tasks completed and validated
- [ ] 10 measurements committed to runbook
- [ ] P50/P95 within ADR budget
- [ ] If FAIL: triggers HALT to plan and escalates to `change-governor` (do not proceed to Phase 2)

---

### T1.3 — C7: Ingress-domain shared verification

#### Objective
Document that cell-1 and cell-2 share the same wildcard ingress domain (no per-cell subdomain). Closes ADR condition C7.

#### Evidence
- `MEET-INFRA-NORMALIZE-T3-multi-cell.md:76` C7 — "declare ingress-domain strategy; for ≤5 min rollback SLO, shared-domain path required."
- ADR D3 above codifies the shared-domain decision.
- `api/internal/placement/k8s_cell_store.go:91` — `IngressDomain` read from CR label `loopzero.io/ingress-domain` (operational metadata).

#### Files to edit
```
docs/runbooks/cell-2-activation.md — (NEW; created in T5.1) document shared-domain invariant
infra/tests/parity/cell2-activation-gates.sh — (NEW; created in T1.5) gate function: ingress domains match
```

#### Deep file dependency analysis
- **`cell2-activation-gates.sh`** — gate function:
  ```bash
  gate_c7() {
    domain1=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get clustercredential -l loopzero.io/cell-id=cell-1 -o jsonpath='{.items[0].metadata.labels.loopzero\.io/ingress-domain}')
    domain2=$(kubectl --context k3d-loopzero-mgmt -n loopzero-system get clustercredential -l loopzero.io/cell-id=cell-2 -o jsonpath='{.items[0].metadata.labels.loopzero\.io/ingress-domain}')
    [[ "$domain1" == "$domain2" ]] || fail "C7: ingress domains differ ($domain1 vs $domain2)"
  }
  ```
- ClusterCredential CR label `loopzero.io/ingress-domain` must be set IDENTICALLY on cell-1 and cell-2 CRs. The `clustercredential-create.sh` script must apply the same value to both.

#### Deep Dives

**Why labels, not spec.** Per `k8s_cell_store.go:93` — "Ingress domain from label (operational metadata, not spec)". Domain isn't part of the connectivity contract; it's how the platform routes to apps after connection is established. Storing in label keeps spec strictly to credentials.

**Dev domain.** Local k3d uses `*.loopzero.local` (per `dynamic_router.go:115` log fields and CLAUDE.md "domains: loopzero.local"). Both cell-1 and cell-2 CRs must have label `loopzero.io/ingress-domain=loopzero.local`.

**Production domain.** Production uses `*.dev.loopzero.example` (or `*.loopzero.app` depending on env). Same label, same value across cell-1 + cell-2 CRs.

**Future cell expansion.** When cell-3 lands (Sprint 9+), it MUST also use the same domain unless a future ADR explicitly deprecates D3.

#### Tasks
1. Verify `clustercredential-create.sh` applies the `loopzero.io/ingress-domain` label.
2. If absent, modify `clustercredential-create.sh` to accept an INGRESS_DOMAIN env var (default `loopzero.local` for dev, `dev.loopzero.example` for prod) and apply as label.
3. Re-run `task k3d:heal` to refresh both cell-1 and cell-2 CRs with labels.
4. Add `gate_c7` function to `cell2-activation-gates.sh` (T1.5).
5. Document in `cell-2-activation.md` (T5.1).

#### TDD

```
RED:    test_clustercredential_create_applies_ingress_domain_label() — bash unit test (using bats or pure bash trap) asserting label applied after create
RED:    test_gate_c7_passes_when_domains_match() — call gate_c7 in mock kubectl env where both labels = "loopzero.local"; expect exit 0
RED:    test_gate_c7_fails_when_domains_differ() — mock different labels; expect exit 1 with C7 message
GREEN:  Modify clustercredential-create.sh + add gate_c7
REFACTOR: None expected
VERIFY: task k3d:heal && kubectl get clustercredentials -A -o jsonpath='{range .items[*]}{.spec.cellID}={.metadata.labels.loopzero\.io/ingress-domain}{"\n"}{end}'
```

#### Acceptance Criteria
- [ ] All ClusterCredential CRs have label `loopzero.io/ingress-domain` set to identical value.
- [ ] `gate_c7` PASSES on local dev environment.
- [ ] Pass: shellcheck on `clustercredential-create.sh` (zero new warnings).

#### DoD
- [ ] All tasks completed and validated
- [ ] gate_c7 GREEN
- [ ] Documented in cell-2-activation.md

---

### T1.4 — C8/C9/C10/C11/C12: Security gate matrix

#### Objective
Verify the five security conditions from ADR Stage 2 (platform-security-engineer veto) before any flip. Each becomes a gate function in `cell2-activation-gates.sh`.

#### Evidence
- `MEET-INFRA-NORMALIZE-T3-multi-cell.md:84-90` C8–C12.

#### Files to edit
```
infra/tests/parity/cell2-activation-gates.sh — (NEW; full file in T1.5) add gate_c8..gate_c12 functions
api/internal/middleware/redact_kubeconfig_test.go — verify log redaction (C9)
api/internal/routes/admin_pin_cell_test.go — verify authz on 3 paths (C11)
```

#### Deep file dependency analysis

**C8 — DOKS-side credential revocation.** Local k3d does not have DOKS, so this gate runs only against production preflight. For dev: gate trivially passes with note "N/A — dev k3d uses kubernetes service accounts; no DOKS revocation surface". For production (deferred to Sprint 9 prod canary): gate calls `doctl kubernetes cluster kubeconfig show` and asserts new cert ID differs from pre-rotation.

**C9 — Cell-2 creds don't leak in cell-1 logs.** Test:
1. Trigger a deploy on cell-1.
2. Capture `kubectl --context k3d-loopzero-workload -n loopzero-* logs <pod>` for 30s window.
3. Search for `BEGIN CERTIFICATE`, `apiVersion: v1\nkind: Config`, base64 kubeconfig markers.
4. Assert ZERO hits.

**C10 — Tenant data isolation.** Test:
1. Pin synthetic tenant T to cell-2.
2. Wait 30s.
3. Assert `kubectl --context k3d-loopzero-workload -n loopzero-${T}-production get pods` returns NotFound (cell-1 should not host T's pods).
4. Assert `kubectl --context k3d-loopzero-workload-2 -n loopzero-${T}-production get pods` returns ≥1 pod.
5. Assert HTTP probe to `${T}-production.loopzero.local` resolves to cell-2 IP (CF Tunnel + DynamicRouter routing). Locally: assert response header `X-LoopZero-Cell: cell-2` (NEW header to add for forensics; encoded in operator).

**C11 — Authz on `--cell` flag in 3 paths.** Three sub-gates:
1. CLI: `loopzero admin tenants pin --tenant X --cell cell-1` without `LOOPZERO_SERVICE_AUTH` → exit 4 (auth error).
2. API direct: `curl -X POST $API/api/v1/admin/tenants/pin -d '{"tenant_id":"X","cell_id":"cell-1"}'` without `X-Service-Auth` header → HTTP 403.
3. GitHub App webhook: simulate webhook payload that tries to set `cell_id` in body → API ignores (autodeploy NEVER pins; pin endpoint is separate). Assert by code-grep: no `cell_id` field in `api/internal/routes/github_*` route handlers.

**C12 — ClusterCredential Secret hygiene.** Assert: for every ClusterCredential `<name>-kubeconfig` Secret, the kubeconfig YAML's `clusters[0].cluster.certificate-authority-data` is base64-decodable to PEM starting with `-----BEGIN CERTIFICATE-----`. Per MEET-fix-6 invariant.

#### Deep Dives

**X-LoopZero-Cell header for C10.** Operator reconcile loop already emits `loopzero.io/cell-id=<cellID>` annotation on tenant pods (per `tenant_controller.go`). To make HTTP-level isolation visible: add Traefik IngressRoute middleware that copies pod label `loopzero.io/cell-id` into response header `X-LoopZero-Cell`. Out of scope for THIS plan IF the kubectl-pod assertion is sufficient. Decision: rely on `kubectl get pods --context` evidence for C10; defer header injection to Sprint 9.

**False-positive avoidance in C9 grep.** Pod logs may legitimately contain "BEGIN CERTIFICATE" if a developer logs a TLS handshake error. Refine grep to: `BEGIN CERTIFICATE.*RUNTIME-2|cell-2`. Combined with: `kubeconfig` literal as full word (\b boundaries).

**C11 webhook bypass — code-only verification.** No live test possible without spinning up a fake GitHub. Use static code grep: assert no `req.Body...cell_id` JSON path parsing in `github_webhook.go`. This is a brittle test — also add an explicit godoc comment on the github webhook handler: "INVARIANT: webhook MUST NOT set cell_id; placement is operator-only via admin/tenants/pin."

**Unit-test coverage requirement.** Per `.claude/rules/tdd-workflow.md`: every business logic must have a unit test. C9 + C10 are mostly integration tests (require live cluster). C11 path 1 + path 2 = unit-testable in `admin_pin_cell_test.go`. Add explicit cases.

#### Tasks
1. Implement `gate_c8` in `cell2-activation-gates.sh` (env-aware: skip with N/A on dev k3d, run doctl on prod).
2. Implement `gate_c9` (kubectl logs grep against cell-1 workload pods).
3. Implement `gate_c10` (kubectl get pods cross-context + HTTP probe).
4. Implement `gate_c11` (3 sub-gates: CLI exit code, API HTTP status, code-grep).
5. Implement `gate_c12` (kubectl get secret + base64 -d + grep BEGIN CERTIFICATE).
6. Add unit-test cases in `admin_pin_cell_test.go` for C11 path 1+2.
7. Add godoc invariant comment on `github_webhook.go` for C11 path 3.

#### TDD

```
RED:    Test_AdminPinCell_RejectsCallWithoutXServiceAuth — POST without header → 403
RED:    Test_AdminPinCell_RejectsCallWithoutAdminApiFlag — endpoint disabled when LOOPZERO_ADMIN_API!=true → 404
RED:    test_gate_c8_skips_on_dev_with_na_marker
RED:    test_gate_c9_passes_on_clean_logs (mock kubectl returning logs without BEGIN CERTIFICATE)
RED:    test_gate_c9_fails_on_dirty_logs (mock kubectl returning logs WITH BEGIN CERTIFICATE)
RED:    test_gate_c10_passes_after_pin
RED:    test_gate_c11_three_paths_pass
RED:    test_gate_c12_kubeconfig_ca_data_pem_check
GREEN:  Implement gates + unit tests
REFACTOR: Extract `kubectl_logs_grep_count` helper if used in C9 + future gates. Defer.
VERIFY: cd api && go test ./internal/routes/... && bash infra/tests/parity/cell2-activation-gates.sh --gates c8,c9,c10,c11,c12
```

#### Acceptance Criteria
- [ ] All 5 gates PASS on local dev (with C8 marked N/A).
- [ ] Unit tests `admin_pin_cell_test.go` cover C11 paths 1+2.
- [ ] Godoc on `github_webhook.go` declares C11 path 3 invariant.
- [ ] `cell2-activation-gates.sh --gates c8,c9,c10,c11,c12` exit 0.
- [ ] Pass: code-audit complexity check (each gate function ≤ 15 cyclomatic — slightly higher than usual because of multi-branch checks; document where exceeded).
- [ ] Pass: code-audit coverage check on `admin_pin_cell.go` (≥ 90% with new tests).

#### DoD
- [ ] All tasks completed and validated
- [ ] All 5 security gates GREEN on dev
- [ ] Unit tests in `admin_pin_cell_test.go` passing
- [ ] godoc invariants documented

---

### T1.5 — Master verification script `cell2-activation-gates.sh`

#### Objective
Single-entrypoint script that runs C1–C12 gates and exits 0 only if 12/12 PASS. Per ADR D5 above.

#### Evidence
- ADR D5: "All 12 ADR conditions C1–C12 verified via a single master script."
- T1.1–T1.4 produce gate functions; T1.5 wires them.

#### Files to edit
```
infra/tests/parity/cell2-activation-gates.sh — (NEW) main script
infra/tests/parity/validate-matrix.sh — append entry 157 (gates.sh as a master gate)
```

#### Deep file dependency analysis
- **`cell2-activation-gates.sh`** — top-level structure:
  ```bash
  #!/usr/bin/env bash
  set -uo pipefail
  GATES=("c1" "c2" "c3" "c4" "c5" "c6" "c7" "c8" "c9" "c10" "c11" "c12")
  declare -A RESULTS

  # Source individual gate functions (each in lib/cell2-gate-cN.sh)
  for g in "${GATES[@]}"; do
    source "$(dirname "$0")/lib/cell2-gate-${g}.sh"
  done

  # Run each
  for g in "${GATES[@]}"; do
    if "gate_${g}"; then RESULTS[$g]="PASS"; else RESULTS[$g]="FAIL"; fi
  done

  # Print summary table
  printf '%-5s %s\n' "GATE" "RESULT"
  for g in "${GATES[@]}"; do printf '%-5s %s\n' "$g" "${RESULTS[$g]}"; done

  # Exit 0 only if all PASS
  for r in "${RESULTS[@]}"; do [[ "$r" == "PASS" ]] || exit 1; done
  ```
- Each gate is a sourced bash function; clean separation; easy to add C13+ in future ADRs.

#### Deep Dives

**C1 — cells table exists.** Trivial gate: `psql -c "SELECT 1 FROM cells WHERE cell_id='cell-2'" | grep -q 1`.

**C3 — Stage 5a tenant-count gate.** This is a production-only gate (needs `count > 50`). On dev: PASS with N/A marker (dev never reaches Stage 5a).

**C5 — PVC enumeration runbook.** This is a documentation gate. Assert `multi-cell-canary.md` includes a "Stateful workload guidance" section listing PVCs as non-migrating.

**C6 — Rollback independent of cell-2 reachability.** Test: simulate cell-2 unreachable (drop iptables on its IP), run `loopzero admin tenants pin --tenant X --cell cell-1` for a tenant currently on cell-2. The pin must succeed (DB UPDATE works; CR MergePatch may need to retry if mgmt cluster→cell-2 connectivity is unrelated). Restore iptables. This gate is INVASIVE — guard with `--invasive` flag, default skip.

**Output style.** Use color in TTY (`\033[32m PASS \033[0m` for green); plain text in CI (detect via `[[ -t 1 ]]`).

**JSON output.** Add `--json` flag emitting `{ "gates": { "c1": "PASS", ... }, "all_pass": true }` for CI consumption. Pre-release governance T2/T3: `--json` is a stability contract (Rule 5) — but this script is internal tooling, not user-facing; still, follow the convention.

**Halt-on-FAIL.** Default behavior: continue all gates, then summary. With `--halt` flag: exit on first FAIL (faster CI feedback). Parameterized.

#### Tasks
1. Create `infra/tests/parity/lib/cell2-gate-c1.sh` through `c12.sh` — one file per gate, each sources `gate_cN()` function.
2. Create `cell2-activation-gates.sh` main entry orchestrating the 12 gates.
3. Support `--gates c1,c2,c5` flag (run subset for debugging).
4. Support `--json` flag for CI consumption.
5. Support `--invasive` flag (gates that perturb infra; default skip).
6. Add validate-matrix.sh entry 157.

#### TDD

```
RED:    test_gates_sh_exists_and_executable
RED:    test_gates_sh_runs_all_12_by_default
RED:    test_gates_sh_supports_subset_flag
RED:    test_gates_sh_supports_json_flag
RED:    test_gates_sh_exit_0_when_all_pass
RED:    test_gates_sh_exit_1_when_any_fail
RED:    test_validate_matrix_157_invokes_gates_sh
GREEN:  Implement script + 12 gate libs + matrix entry
REFACTOR: Consider extracting a `_pass()` / `_fail()` helper if gate logging duplicates. Defer.
VERIFY: bash infra/tests/parity/cell2-activation-gates.sh --json
```

#### Acceptance Criteria
- [ ] Running `bash cell2-activation-gates.sh` on a fresh dev (Phase 0 complete) outputs a 12-row summary, all PASS.
- [ ] `--json` produces parseable JSON with `all_pass: true`.
- [ ] `--gates c1,c2` runs subset only.
- [ ] Pass: shellcheck on all 13 files (1 main + 12 gate libs).
- [ ] Pass: code-audit complexity check (each gate function ≤ 15).
- [ ] validate-matrix entry 157 GREEN.

#### DoD
- [ ] All tasks completed and validated
- [ ] 12/12 gates PASS on dev
- [ ] Script is the single source of truth for "is cell-2 ready to flip?"

---

## Phase 2: Status Flip

**Objective:** Change `cells.status` from `maintenance` → `active` for `cell-2`, gated behind 12/12 gates GREEN.

### T2.1 — Pre-flip checklist execution + sign-off

#### Objective
Operator runs the master gates script + reviews evidence + signs off in the PR description. No automated flip — human-in-the-loop required for irreversible state change in dev environment (production-equivalent rigor per Rule 10).

#### Evidence
- ADR D5: "Cells flip CANNOT be done by hand — it's gated behind this script's exit 0."
- Pre-release governance: T2 changes need traceability; sign-off in PR is the audit trail.

#### Files to edit
```
docs/runbooks/cell-2-activation.md — (NEW; full file in T5.1) add "Pre-flip sign-off checklist" section
(no code edits)
```

#### Deep file dependency analysis
- **`cell-2-activation.md`** — checklist section template:
  ```markdown
  ## Pre-flip sign-off

  Operator: ${NAME}
  Date: ${DATE}
  Local k3d 4-cluster topology: [ ] verified
  task k3d:heal exit 0: [ ] verified
  cell2-activation-gates.sh --json all_pass=true: [ ] attached output
  Rollback SLO measurement (T1.2): P50=___s, P95=___s — within budget [ ]
  Synthetic tenant ready for Stage 1: [ ] tenant-id=___

  Sign-off: ${OPERATOR_SIGNATURE} ${DATE}
  ```

#### Deep Dives

**Why human sign-off.** Even with 12/12 gates GREEN, the engineer running the flip is the last line of defense. They visually correlate metrics dashboards, recall recent infra changes, and apply institutional context the automated gates lack.

**Audit trail.** Sign-off lives in the PR that authors the cells flip migration (T2.2). The PR description includes the full gates.sh JSON output as a code block.

#### Tasks
1. Draft sign-off section in `cell-2-activation.md` (T5.1).
2. Operator runs `cell2-activation-gates.sh --json > /tmp/gates-${DATE}.json`.
3. Operator pastes JSON into PR description.
4. Operator fills in P50/P95 from T1.2 output.
5. Operator names a synthetic tenant for Stage 1.

#### TDD
N/A — checklist task. Verification is via PR template lint (future).

#### Acceptance Criteria
- [ ] Sign-off section present in `cell-2-activation.md`.
- [ ] Operator can complete the checklist without ambiguity (each checkbox traceable to a gate or a measurement).
- [ ] PR cannot be merged without checklist + JSON evidence (enforced by code-owner review, not automation — pre-release governance scope).

#### DoD
- [ ] Section drafted in runbook
- [ ] Sample sign-off committed for the dev activation

---

### T2.2 — Status flip migration

#### Objective
UPDATE `cells SET status='active' WHERE cell_id='cell-2'`. Forward-only migration. Reversible via subsequent UPDATE.

#### Evidence
- `cells` table seed `status='maintenance'` (`000006_cells.up.sql:124-129`).
- `services/cell_pinner.go:117-133` rejects pin if status != active.

#### Files to edit
```
api/internal/db/migrations/000007_activate_cell_2.up.sql — (NEW) UPDATE cells statement
docs/runbooks/cell-2-activation.md — append "Migration applied" timestamp
```

#### Deep file dependency analysis
- **`000007_activate_cell_2.up.sql`** — single statement migration:
  ```sql
  -- CHG-2026-05-XX-CELL2-ACTIVATION — flip cell-2 from maintenance to active.
  -- Pre-conditions verified: cell2-activation-gates.sh 12/12 PASS.
  -- ADR: docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md APPROVED_WITH_CONDITIONS 2026-04-30.
  -- This plan: docs/plans/cell-2-activation-plan.md
  -- Idempotent: re-running this migration is a no-op.
  UPDATE cells SET status='active', updated_at=now()
   WHERE cell_id='cell-2' AND status='maintenance';
  ```
- Migration runner: `task db:migrate` runs forward-only `.up.sql` files in order.
- Downstream: `CellPinner.Pin` will start accepting cell-2 as a target within the next request after the UPDATE commits.

#### Deep Dives

**No `.down.sql`.** The platform follows forward-only migrations (CLAUDE.md Rule + memory `feedback_no_tribal_knowledge.md`). Reverting cell-2 to `maintenance` is a forward operation: a new migration `000008_deactivate_cell_2.up.sql`.

**Idempotency via WHERE clause.** `WHERE cell_id='cell-2' AND status='maintenance'` makes the UPDATE a no-op on second run. If status is already 'active' or any other value, no rows are touched.

**Audit trail.** `cells.updated_at` is set by trigger `trg_cells_set_updated_at` (000006:92-95). The migration ALSO sets it explicitly so the audit log shows the intent ("migration touched this row") not just trigger side-effect.

**Production rollout sequencing.** Per ADR D6: production stages 4-5 are out of scope. So this migration runs ON DEV ONLY in this plan. Production rollout requires a separate migration (e.g., `000010_activate_cell_2_prod.up.sql`) tied to that future readiness review. To keep dev/prod migrations divergent, this migration is gated by env in CI: PR reviewer ensures it doesn't auto-apply to prod via the standard `task db:migrate` flow during this plan's execution.

Wait — reconsider. Migrations are forward-only, environment-agnostic. If this migration lands on `develop` and ArgoCD syncs to prod, it would auto-flip cell-2 to active in prod TOO. That breaks D6 (out of scope: production).

**MITIGATION:** The migration must be explicitly gated. Options:
(A) Add `WHERE current_database() = 'loopzero_dev'` — fragile, breaks reproducibility.
(B) Gate via env var checked in migration runner — not how golang-migrate works.
(C) Two-stage: first PR adds the migration but reviewer ensures it's NOT bundled in a prod release; second PR (future) re-applies same UPDATE in prod via separate `000010_activate_cell_2_prod.up.sql`.

**Decision:** Use option (C). The migration FILE encodes "intent to activate cell-2", but the MIGRATION ID `000007` is dev-only. For prod, a future plan emits a NEW migration with a higher number that runs the same UPDATE statement. This is acceptable because: (1) UPDATE is idempotent; (2) two migrations with the same effect on different environments is a common pattern in golang-migrate when env-specific.

Actually, even simpler: if migration 000007 runs in BOTH dev and prod (after develop merges to main and ArgoCD syncs), and prod is then activated automatically — that VIOLATES D6.

**Better mitigation:** Add a SAFETY check at the top of the migration:
```sql
DO $$
BEGIN
  -- Refuse to run in production unless LOOPZERO_ALLOW_CELL2_ACTIVATION is set.
  IF current_setting('app.env', true) = 'production'
     AND current_setting('app.allow_cell2_activation', true) IS DISTINCT FROM 'true' THEN
    RAISE EXCEPTION 'CELL2 activation in production requires explicit setting (Sprint 9 prod canary).';
  END IF;
END$$;
UPDATE cells SET status='active', updated_at=now()
 WHERE cell_id='cell-2' AND status='maintenance';
```

This requires the API to set `SET app.env='production'` on the connection in prod, which... is not standard. But it's a clear marker.

**Even simpler:** Pre-release dev environment has only ONE PG database. There IS no auto-sync to prod for migrations during pre-release (per CLAUDE.md "pre-release"). Engineering manually applies migrations to prod via `task db:migrate` against the prod DB. So the migration lands in `develop` → applied manually to dev PG → eventually applied manually to prod PG (controlled by Sprint 9 prod canary).

**FINAL decision:** Migration 000007 lands as-is. Pre-release ops governance ensures it's NOT applied to prod until Sprint 9. Document this explicitly in PR description. Add comment to migration:
```sql
-- WARNING: This migration MUST NOT be applied to production until Sprint 9
-- prod canary readiness review APPROVES Stage 4. See ADR D6 in cell-2-activation-plan.
```

#### Tasks
1. Create `000007_activate_cell_2.up.sql` with idempotent UPDATE + warning comment.
2. Run `task db:migrate` on local dev PG.
3. Verify `psql -c "SELECT status FROM cells WHERE cell_id='cell-2'"` returns `active`.
4. Append migration timestamp to `cell-2-activation.md`.

#### TDD

```
RED:    Test_Migration_000007_FlipsCell2_ToActive — TestContainers Postgres, run all migrations, assert cell-2 status == 'active'
RED:    Test_Migration_000007_Idempotent — run twice, assert no error, status stays 'active'
RED:    Test_Migration_000007_NoOpOnNonMaintenanceStatus — set cell-2 to 'draining' first, run migration, assert status stays 'draining' (WHERE clause filtering)
GREEN:  Create migration file
REFACTOR: None expected
VERIFY: cd api && go test ./internal/db/...
```

#### Acceptance Criteria
- [ ] Migration file exists with WHERE clause + warning comment.
- [ ] Three migration tests PASS.
- [ ] On dev PG: cell-2 status = 'active' post-migrate.
- [ ] PR description explicitly states "DEV ONLY — do not apply to prod".
- [ ] Pass: `task validate:manifests` (no manifest changes here, smoke).
- [ ] Pass: code-audit on migration SQL (length ≤ 50 LOC excluding comments).

#### DoD
- [ ] All tasks completed and validated
- [ ] Migration applied on dev PG
- [ ] Three tests passing
- [ ] PR description documents prod gating

---

### T2.3 — Verify post-flip discovery

#### Objective
Within 90s of migration commit, `loopzero_cell_discovery_count` ≥ 3 AND DynamicRouter accepts cell-2 in Resolve calls without error. Closes activation prerequisite.

#### Evidence
- DynamicRouter refresh interval = 60s (`dynamic_router.go:41`). Worst case to observe new state: 60s + minor jitter.
- After migration: `cells.status='active'` for cell-2 → `K8sCellStore.LoadCells` (existing) returns cell-2 in next refresh → `loopzero_cell_discovery_count` updates → routing accepts cell-2.

Wait — actually `K8sCellStore` reads ClusterCredential CRs (not the `cells` table). The discovery count was already 3 in Phase 0 (after H14). The migration changes `cells.status`, which is read by `CellPinner` (rejects non-active), not by DynamicRouter directly.

So T2.3 is actually: verify `CellPinner.Pin --tenant X --cell cell-2` SUCCEEDS post-migration (it would have FAILED with `ErrCellNotPinnable` pre-migration).

#### Files to edit
```
infra/tests/parity/cell2-activation-gates.sh — add gate_pin_accepts_cell2 (post-flip verification gate)
```

#### Deep file dependency analysis
- **`cell2-activation-gates.sh`** — new gate function `gate_pin_accepts_cell2` that:
  1. Picks a throwaway tenant (e.g., from existing dev tenants).
  2. Notes its current cell.
  3. Runs `loopzero admin tenants pin --tenant ${T} --cell cell-2`.
  4. Asserts exit 0.
  5. Rolls back: `loopzero admin tenants pin --tenant ${T} --cell ${ORIGINAL_CELL}`.

#### Deep Dives

**Why a separate post-flip gate.** Phase 1 gates ran BEFORE migration. They verify discovery + isolation but cannot verify pinning success because pre-migration cell-2 status was `maintenance` (Pinner rejects). Post-flip gate is the first observable evidence of activation.

**Throwaway tenant.** Use `tn-canary-cell2-${EPOCH}` synthetic tenant created via dogfood loop (mirror `cell2-canary-stage1.sh:39-41`). After this gate, the same tenant is reused for Stage 1 (T3.1).

**Risk: gate runs and immediately rolls back, but Stage 1 expects tenant ON cell-2.** Mitigation: rename gate to NOT roll back; just leave the tenant on cell-2 and pass it to Stage 1. Or: use ephemeral tenant for gate, separate fresh tenant for Stage 1.

**Decision:** Use ephemeral. Easier to reason about; Stage 1 has its own bootstrap (T3.1).

#### Tasks
1. Add `gate_pin_accepts_cell2` to gates.sh (or run as standalone for Phase 2 specifically — `cell2-activation-gates.sh --post-flip`).
2. Re-run `cell2-activation-gates.sh --post-flip` after migration commits.
3. Append result to `cell-2-activation.md`.

#### TDD

```
RED:    test_gate_pin_accepts_cell2_passes_post_flip — mock CellPinner.Pin returns 200; assert gate exit 0
RED:    test_gate_pin_accepts_cell2_fails_pre_flip — mock returns 409 ErrCellNotPinnable; assert gate exit 1
GREEN:  Implement gate + --post-flip flag
REFACTOR: None expected
VERIFY: bash infra/tests/parity/cell2-activation-gates.sh --post-flip
```

#### Acceptance Criteria
- [ ] Gate `gate_pin_accepts_cell2` PASSES within 90s of migration commit.
- [ ] Discovery count ≥ 3 maintained.
- [ ] Throwaway tenant cleaned up (deleted post-gate).

#### DoD
- [ ] Gate passing
- [ ] Result appended to runbook

---

## Phase 3: Stage 1 — Synthetic Tenant Canary (T5.6)

**Objective:** 1 synthetic tenant on cell-2; 1h soak. Per ADR Stage 1 verbatim.

### T3.1 — Bootstrap synthetic tenant

#### Objective
Create a dedicated synthetic tenant `tn-canary-cell2-stage1-${DATE}` with 1 deployed `solo-server` project on cell-1 (default placement), at least 1 ACTIVE release.

#### Evidence
- `cell2-canary-stage1.sh:39-41` — "Use the dogfood pipeline... `bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy`".

#### Files to edit
```
docs/runbooks/cell-2-activation.md — append "Stage 1 tenant bootstrap" log
(no production code edits — uses existing dogfood loop)
```

#### Deep file dependency analysis
- **`infra/tests/dogfood/full-loop.sh`** — orchestrator for dogfood journeys. Currently 12/12 GREEN per memory. `--only journey-01-solo-deploy` runs single-tenant deploy.
- Downstream: synthetic tenant ends up in `tenants` table on cell-1. cell2-canary-stage1.sh:128 verifies `phase=Active`.

#### Deep Dives

**Why dedicated tenant.** Don't reuse the throwaway tenant from T2.3 (deleted post-gate). Don't reuse a real dev tenant — soak might affect their data. Synthetic tenants are disposable and named-by-date.

**Why `solo-server`.** Smallest, fastest-to-deploy template. Single replica, no PVCs (per C5 disclosure: stateful workloads excluded from canary).

#### Tasks
1. Set `SYNTH_TENANT_ID=tn-canary-cell2-stage1-$(date -u +%Y%m%d-%H%M%S)`.
2. Run `SYNTH_TENANT_ID=$SYNTH_TENANT_ID bash infra/tests/dogfood/full-loop.sh --only journey-01-solo-deploy`.
3. Verify `kubectl get tenant ${SYNTH_TENANT_ID} -o jsonpath='{.status.phase}'` = `Active`.
4. Verify ≥1 ACTIVE release in `releases` table.
5. Log SYNTH_TENANT_ID to runbook.

#### TDD
N/A — execution task. Verification via existing dogfood gates.

#### Acceptance Criteria
- [ ] Synthetic tenant created with phase=Active.
- [ ] ≥1 ACTIVE release.
- [ ] SYNTH_TENANT_ID logged in runbook.

#### DoD
- [ ] Tenant ready
- [ ] ID committed to runbook

---

### T3.2 — Execute `cell2-canary-stage1.sh`

#### Objective
Pin synthetic tenant to cell-2; soak 1h with HTTP probes every 30s; auto-rollback on halt criteria.

#### Evidence
- Script is already implemented (`cell2-canary-stage1.sh`). This task is execution + observation.

#### Files to edit
```
(execution-only — no file edits during run)
```

#### Deep file dependency analysis
- **`cell2-canary-stage1.sh:226-242`** — main flow: preflight → verify tenant → pin → soak → cleanup.
- Halt criteria (lines 186-204): 3 consecutive 5xx → exit 3; cell-1 EB > 0.1% → exit 4.
- Trap on EXIT (line 230): always re-pin to cell-1 + delete tenant.

#### Deep Dives

**Why 1h.** Per ADR Stage 1 spec verbatim. Long enough to surface state-machine edge cases (deploy retries, ArgoCD sync flaps); short enough to fit in a workday.

**Probe interval 30s.** Default. With 1h soak = 120 probes. Observable in Prometheus dashboards.

**Cell-1 EB threshold 0.1%.** Tighter than Stage 2 because the test is "moving 1 tenant doesn't perturb cell-1 baseline."

**On HALT.** Trap re-pins to cell-1; gate operator inspects logs + Prometheus; opens RCA per `multi-cell-rca-template.md`.

#### Tasks
1. Run `bash infra/tests/parity/cell2-canary-stage1.sh --dry-run` first.
2. Confirm dry-run exits 0.
3. Run `SYNTH_TENANT_ID=$SYNTH_TENANT_ID bash infra/tests/parity/cell2-canary-stage1.sh`.
4. Monitor Grafana dashboards during soak.
5. On exit 0: append GREEN closure to runbook (T3.3).
6. On exit > 0: open RCA, do NOT proceed to Phase 4.

#### TDD
N/A — execution task. Script's own halt criteria + Prometheus alerts are the verification.

#### Acceptance Criteria
- [ ] Script exit code = 0 after 1h.
- [ ] `loopzero_cell_routing_decisions_total{cell="cell-2",decision="error"}` = 0 throughout.
- [ ] `loopzero_cell_health{cell="cell-2"}` = 1 throughout.
- [ ] Cell-1 EB consumption < 0.1% in any 5min window.
- [ ] No multi-cell alert fired.

#### DoD
- [ ] All criteria GREEN
- [ ] Probe data + EB data captured

---

### T3.3 — Append Stage 1 closure to runbook

#### Objective
Document Stage 1 results in `multi-cell-canary.md` per its existing "T5.6 stage 1 closure" section requirement (line 73-78).

#### Evidence
- `multi-cell-canary.md:88-89` — "If T5.6 closure section is missing, stage 2 must NOT run."

#### Files to edit
```
docs/runbooks/multi-cell-canary.md — append "## T5.6 stage 1 closure" section
```

#### Deep file dependency analysis
- Closure includes: run timestamp UTC, probe count + success count, any 5xx blip, cell-1 EB delta, anything operator-relevant for stage 2.
- Stage 2 script (`cell2-canary-stage2.sh:90`) checks for this section's existence as a precondition.

#### Tasks
1. Open `multi-cell-canary.md`.
2. Append `## T5.6 stage 1 closure` section with structured fields.
3. Bump runbook version per its versioning rule (line 178-180).

#### TDD
N/A — documentation task.

#### Acceptance Criteria
- [ ] Closure section present.
- [ ] Includes timestamp, probe count, EB delta.
- [ ] Runbook version bumped.

#### DoD
- [ ] Section committed

---

## Phase 4: Stage 2 — 10% Dev Tenants Canary (T5.7)

**Objective:** 10% of dev tenants on cell-2; 24h soak. Per ADR Stage 2 verbatim.

### T4.1 — Stage 2 dry-run + sample selection

#### Objective
Verify dev environment has ≥10 cell-1 tenants (so 10% sample is meaningful). Generate the pinned-list file.

#### Evidence
- `cell2-canary-stage2.sh:91-101` — dry-run preflight.

#### Files to edit
```
(no file edits — execution + log only)
```

#### Tasks
1. Run `bash infra/tests/parity/cell2-canary-stage2.sh --dry-run`.
2. Confirm exit 0.
3. Capture sample list to `/tmp/cell2-canary-stage2-pinned-${TIMESTAMP}.txt`.

#### Acceptance Criteria
- [ ] Dry-run exit 0.
- [ ] Sample list has ≥1 tenant (10% of ≥10).
- [ ] Sample biased to low-risk: tier ∈ {pro, team}, not enterprise; recent deploy success.

#### DoD
- [ ] Sample list committed to runbook (anonymized if PII)

---

### T4.2 — Execute `cell2-canary-stage2.sh`

#### Objective
Pin 10% dev tenants in batches of 5 with 30min gaps; 24h soak.

#### Evidence
- Script existing. Halt: cell-2 5xx > 1.0%/10min OR cell-1 EB > 1.0%.

#### Files to edit
```
(execution-only)
```

#### Deep Dives

**24h covers diurnal cycle.** Different load profiles morning/evening surface different bugs. 24h is the minimum for diurnal coverage.

**Batch + 30min gap.** Surfaces anomalies before next batch. 5 tenants × 30min = 2.5h for first 25% of the 10% sample (assuming sample ≥ 25). Then watch 24h.

**On HALT.** Stage-2 partial rollback per `${PINNED_LIST}` (re-pin in reverse order).

#### Tasks
1. Run `SOAK_SECONDS=86400 PERCENT=10 bash infra/tests/parity/cell2-canary-stage2.sh`.
2. Monitor Grafana dashboards.
3. On HALT (exit 3 or 4): partial rollback completes; open RCA.
4. On GREEN: 10% stays on cell-2 (no auto-rollback per stage 2 exit policy `multi-cell-canary.md:115-117`).

#### Acceptance Criteria
- [ ] Script exit 0 after 24h.
- [ ] Cell-2 5xx rate < 1.0% in any 10min window.
- [ ] Cell-1 EB consumption < 1.0%.
- [ ] No batch halt.

#### DoD
- [ ] Stage 2 GREEN
- [ ] 10% dev tenants stay on cell-2

---

### T4.3 — Append Stage 2 closure or RCA

#### Objective
Mirror T3.3 for Stage 2.

#### Files to edit
```
docs/runbooks/multi-cell-canary.md — append "## T5.7 stage 2 closure" OR open RCA per multi-cell-rca-template.md
```

#### Tasks
1. On GREEN: append `## T5.7 stage 2 closure` (timestamp, batches, EB deltas, 24h dashboard screenshots).
2. On HALT: copy `multi-cell-rca-template.md` → `cell2-stage2-rca-${DATE}.md`; fill in.
3. Bump runbook version.

#### Acceptance Criteria
- [ ] Closure OR RCA present.
- [ ] Runbook version bumped.

#### DoD
- [ ] Section/RCA committed

---

## Phase 5: Closure & Documentation

### T5.1 — Cell-2 activation runbook

#### Objective
Single operator-facing runbook stitching Phases 0–4 into a step-by-step procedure that any engineer can follow without prior context.

#### Files to edit
```
docs/runbooks/cell-2-activation.md — (NEW) operator runbook
docs/runbooks/multi-cell-canary.md — cross-link to cell-2-activation.md
```

#### Tasks
1. Draft runbook structure: Pre-flight → Local Parity → Gates → Migration → Stage 1 → Stage 2 → Post-Activation.
2. Each section: command + expected output + troubleshooting.
3. Cross-reference: ADR, this plan, alerts runbook, RCA template.

#### Acceptance Criteria
- [ ] Runbook ≤ 500 LOC.
- [ ] Every step has expected exit code or output.
- [ ] Troubleshooting for each likely failure.
- [ ] References ADR + plan + alerts + RCA + sibling runbooks.

#### DoD
- [ ] Runbook committed

---

### T5.2 — CHANGELOG + memory + maturity bump

#### Objective
Record activation in CHANGELOG, write memory entry, bump maturity assessment.

#### Files to edit
```
CHANGELOG.md — Added entries
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/MEMORY.md — index entry
/home/paulo/.claude/projects/-home-paulo-loopzero/memory/reference_cell2_activation_<date>.md — (NEW) memory file
docs/maturity-assessment.md — bump 3.7 → 3.8 (if Stage 2 GREEN); update Path-to-4.0 ("Cell-2 activation" entry)
```

#### Tasks
1. CHANGELOG `[Unreleased]` `Added`:
   - "Local k3d cluster `loopzero-workload-2` provisioned (T0.1)"
   - "Heal script H14 codifies cell-2 ClusterCredential bootstrap (T0.2)"
   - "Master gates script `cell2-activation-gates.sh` for ADR C1–C12 verification (T1.5)"
   - "Migration 000007 activates cell-2 in dev (T2.2)"
   - "Stage 1 + Stage 2 dev canary GREEN (T3+T4)"
2. Memory file: closure evidence (timestamps, probe counts, EB deltas, RCA refs if any).
3. MEMORY.md index entry (1 line under 150 chars).
4. Maturity assessment: bump 3.7 → 3.8 (Scale/Multi-tenancy 3.0 → 3.5, Infrastructure 3.8 → 4.0); update Path-to-4.0 row.

#### Acceptance Criteria
- [ ] CHANGELOG entries present, each with `(this plan)` reference.
- [ ] Memory file written, MEMORY.md index updated.
- [ ] Maturity bumped.

#### DoD
- [ ] All updates committed

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Local k3d has no cell-2 cluster | T0.1 | Create `infra/k3d/workload-2-cluster.yaml`, modify `Taskfile.yml` |
| 2 | Heal script doesn't bootstrap cell-2 ClusterCredential | T0.2 | H14 block in `k3d-cross-cluster-heal.sh`, vitest spec, matrix entry 155 |
| 3 | Discovery count ≥ 2 not regression-tested | T0.3 | validate-matrix entry 156 polling Prometheus |
| 4 | ADR C2 (routing determinism) not tested | T1.1 | Two new Go tests in `dynamic_router_test.go`, godoc invariant |
| 5 | ADR C4 (rollback SLO) not measured | T1.2 | `cell2-rollback-slo.sh` measurement script, runbook closure |
| 6 | ADR C7 (ingress-domain shared) not enforced | T1.3 | `clustercredential-create.sh` label + gate_c7 |
| 7 | ADR C8 (DOKS revocation) not gated | T1.4 | gate_c8 (N/A on dev, doctl on prod) |
| 8 | ADR C9 (creds in logs) not gated | T1.4 | gate_c9 (kubectl logs grep) |
| 9 | ADR C10 (tenant data isolation) not gated | T1.4 | gate_c10 (cross-context kubectl get pods) |
| 10 | ADR C11 (authz on 3 paths) not gated | T1.4 | gate_c11 + unit tests + godoc invariant |
| 11 | ADR C12 (Secret CA hygiene) not gated | T1.4 | gate_c12 (base64 decode + PEM check) |
| 12 | No master "is cell-2 ready?" script | T1.5 | `cell2-activation-gates.sh` |
| 13 | Pre-flip sign-off process undefined | T2.1 | Sign-off checklist in cell-2-activation.md |
| 14 | cells.status flip not migrated | T2.2 | `000007_activate_cell_2.up.sql` + 3 tests |
| 15 | Post-flip pinning not verified | T2.3 | gate_pin_accepts_cell2 + --post-flip flag |
| 16 | Stage 1 (T5.6) not executed on real cell-2 | T3.1+T3.2+T3.3 | Bootstrap synthetic tenant; run script; runbook closure |
| 17 | Stage 2 (T5.7) not executed | T4.1+T4.2+T4.3 | Dry-run; 24h run; closure or RCA |
| 18 | No operator-facing runbook stitching the whole flow | T5.1 | `cell-2-activation.md` |
| 19 | Maturity assessment Path-to-4.0 entry not closed | T5.2 | Bump to 3.8; update path entry |
| 20 | CHANGELOG missing activation record | T5.2 | `[Unreleased]` `Added` entries |
| 21 | No memory entry for activation | T5.2 | `reference_cell2_activation_<date>.md` + index |

**Coverage: 21/21 gaps covered (100%).**

## Global Definition of Done

- [ ] All 5 phases (and 14 tasks) completed
- [ ] All tests passing: Go (`api/internal/placement`, `api/internal/db`, `api/internal/routes/admin_pin_cell_test.go`), TypeScript (`infra/pulumi/src/__tests__/heal-script-H14.test.ts`)
- [ ] Zero `go vet` / `golangci-lint` / `shellcheck` / `tsc` warnings introduced
- [ ] Backward compatibility preserved (cell-default + cell-1 behavior unchanged; only cell-2 activated)
- [ ] code-audit checks PASS across all modified packages (complexity ≤ 10 except gates ≤ 15 with justification, coverage ≥ 90% on new code, file size ≤ 500 LOC)
- [ ] `cell2-activation-gates.sh --json` returns `all_pass: true` on dev
- [ ] `loopzero_cell_discovery_count >= 2` measured on local + production
- [ ] Stage 1 + Stage 2 dev canary GREEN (or escalated to RCA per template)
- [ ] Rollback SLO P50 ≤ 300s, P95 ≤ 480s measured
- [ ] CHANGELOG `[Unreleased]` `Added` entries present
- [ ] Memory `reference_cell2_activation_<date>.md` written, MEMORY.md index updated
- [ ] Maturity assessment bumped 3.7 → 3.8 (or higher) with Path-to-4.0 entry closed for cell-2
- [ ] Production Stages 4-5 explicitly deferred to Sprint 9 (D6) — NOT implemented in this plan
- [ ] PR description includes operator sign-off + gates.sh JSON + measurements
