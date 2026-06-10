# Plan: Track A — Bootstrap → Helmfile Values + ESO Generators

> **Version 1.0** — The SMALLEST-yield, HIGHEST-risk track. Delegate the 1 clean bootstrap script (`bootstrap-patch-admission-policies.sh` → chart values) and — gated on an **ESO-generator T2/T3 governance MEET** — migrate the 3 credential-generator scripts (`generate-secrets.sh`, `generate-build-secrets.sh`, `generate-runtime-secrets.sh`) to ESO `Password` generator CRDs while PRESERVING the load-bearing determinism contract. All other bootstrap scripts (clustercredential-create, scoped-kubeconfig, hydra-mint, argocd-register, wire-clusters, etc.) are KEEP-OPERATOR (LIVE kubectl/API orchestration). **Track A of the Script Minimization Program** (`2026-05-24-script-minimization-program-plan.md`) — runs LAST (program D3). Starts ONLY after Track C DoD + DOKS drill GREEN.

## Context

`infra/helmfile/scripts/` + `infra/scripts/bootstrap-*` hold 24 scripts (`unhomed-logic` bootstrap=32 incl. lib + tests). The `helmfile-delegation-map.md` (task #176, read-only audit) already proved the honest yield:

| Script | Delegatable? | Disposition |
|---|---|---|
| `bootstrap-patch-admission-policies.sh` (29 LOC) | **YES** — fold registry prefix into `admission-policies` chart `.Values.registries.*` per-env | DELEGATE → values, delete |
| `generate-secrets.sh` (391) | **Partial** — needs ESO generators (Option B), can't `set:` a minted random/deterministic secret | KEEP until ESO MEET |
| `generate-build-secrets.sh` (120) | **Partial** — same generator gap | KEEP until ESO MEET |
| `generate-runtime-secrets.sh` (140) | **Partial** — same generator gap | KEEP until ESO MEET |
| `k3d-cross-cluster-heal.sh` (1005) | **NO** — operator LIVE recovery (one helm line among H1-H18) | KEEP-OPERATOR |
| clustercredential-create, scoped-kubeconfig, hydra-mint, argocd-register, wire-clusters, bootstrap-cell-cc, bootstrap-oauth-clients, create-wiring-configmap, migrate-database, regression-checks, seed-local-user, install-local-path-provisioner, pulumi-output, health-check-postsync, bootstrap-patch-* (4), bootstrap-platform, bootstrap-seed-tenant-cr | **NO** — LIVE kubectl/API/pulumi orchestration | KEEP-OPERATOR |

**The load-bearing constraint:** per the 2026-05-22 cleanroom session, **credential determinism is the dominant cleanroom-regression theme** — PG password, MinIO root, gitea admin MUST be deterministic across rebuilds or charts that hardcode them break (SOURCE_UPLOAD_FAILED, clone signature mismatch). Any ESO-generator migration MUST preserve deterministic seeds (NOT random-per-sync). This is why the 3 generators are a T2/T3 architecture decision, not an autonomous `set:`.

**Realistic yield:** 1 script autonomously (admission-policies), + up to 3 after the ESO MEET. The rest (~20) are KEEP-OPERATOR. So Track A may only bring bootstrap from 32 → ~28, OR → ~25 if the ESO MEET succeeds. The program reaches ≤30 primarily via Tracks B+C; Track A is the cleanup tail + the ESO decision.

## Objective

**Done = the 1 clean bootstrap script is delegated to chart values (cleanroom-validated + deleted); the ESO-generator MEET is held and either (a) ratifies the determinism-preserving design → 3 generators migrated + cleanroom-validated + deleted, or (b) formally defers with a documented ADR keeping them as KEEP-OPERATOR.**

Measurable:
- `bootstrap-patch-admission-policies.sh` deleted; registry prefix in `admission-policies` chart values per-env; cleanroom re-converges.
- ESO-generator MEET held → ADR ratified (migrate OR defer-with-rationale).
- If migrate: 3 generators → ESO `Password` generators with deterministic seeds; `/cleanroom` + `/dogfood full` GREEN (determinism preserved — zero SOURCE_UPLOAD_FAILED / clone-signature regressions).
- ~20 bootstrap scripts formally reclassified KEEP-OPERATOR in the allowlist (no further deletion attempts).

## ADRs

- **D1 — admission-policies registry prefix → chart values (the 1 clean delegation).**
  - *Decision:* Move the `k3d-loopzero-registry` allow-list prefix that `bootstrap-patch-admission-policies.sh` patches into the `admission-policies` chart's `.Values.registries.*`, set per-environment (k3d/standalone adds the local registry; prod doesn't). Delete the patch script.
  - *Rationale:* Per the delegation map, this is the ONLY bootstrap script that is a clean values-fold (no secret minting). The chart already uses `.Values.registries.*` (unified 2026-05-06).
  - *Consequences:* One less bootstrap script + the registry allow-list becomes declarative per-env. Cleanroom must re-converge (the warn-mode ClusterImagePolicy must still allow k3d-loopzero-registry).

- **D2 — The 3 credential-generators require an ESO-generator T2/T3 MEET BEFORE any code change.**
  - *Decision:* Do NOT touch `generate-*secrets.sh` until a governance MEET ratifies the design. The MEET decides: ESO `Password` generator CRDs with deterministic seeds (preserving the contract) vs defer-keep-bash. Invoke `/meeting` (Change Governor → Production Design + Security & Blast Radius + Change Validation Planner) per pre-release-governance.
  - *Rationale:* These mint credentials consumed by charts that HARDCODE the deterministic value; a random-per-sync ESO generator would break the cleanroom (the 2026-05-22 regression theme). This is a credential-handling + multi-cluster + GitOps-flow change = automatic T2+ (pre-release-governance triggers). Security has absolute veto.
  - *Consequences:* Phase A2 is BLOCKED on the MEET. If the MEET defers, the 3 stay KEEP-OPERATOR and Track A's yield is just the 1 admission-policies script — and that is an ACCEPTABLE honest outcome (don't force a determinism-breaking migration to hit a count).

- **D3 — ESO determinism: if migrated, the Password generator MUST use a deterministic seed (e.g., derived from a per-cluster master secret), NOT `generateName`/random.**
  - *Decision:* The ESO `Password` generator (if ratified) seeds deterministically per (cluster, secret-name) so rebuilds produce the SAME password, matching the charts that hardcode `loopzero-dev-password` (standalone) / consume the externalized prod secret.
  - *Rationale:* The cleanroom proved random-per-rebuild breaks consumers. ESO's `Password` generator defaults to random — the design MUST add deterministic seeding or it's a non-starter.
  - *Consequences:* If ESO can't seed deterministically in our version, the MEET defers (D2 option b). The `pulumi-required-secrets` + `credentials-sot` gates (wired in the prior session) validate the externalization stays correct.

- **D4 — ~20 LIVE-orchestration bootstrap scripts are formally KEEP-OPERATOR (no deletion attempts).**
  - *Decision:* Reclassify the LIVE kubectl/API/pulumi bootstrap scripts (clustercredential-create, scoped-kubeconfig, hydra-mint, argocd-register, wire-clusters, bootstrap-cell-cc, bootstrap-oauth-clients, create-wiring-configmap, migrate-database, regression-checks, seed-local-user, pulumi-output, health-check-postsync, the 4 bootstrap-patch-*, bootstrap-platform, bootstrap-seed-tenant-cr, install-local-path-provisioner) as KEEP-OPERATOR in the allowlist with the LIVE-orchestration rationale. Stop counting them as migration candidates.
  - *Rationale:* They run `kubectl create token`, `helm upgrade`, `pulumi`, hydra `exec`, ArgoCD API — LIVE orchestration that Go/helmfile would only shell-wrap (no-shell-wrap). They are the cleanroom bring-up path (validated this session: setup → 41/0).
  - *Consequences:* The allowlist reflects reality; `unhomed-logic` bootstrap domain stops being treated as reducible. The ≤30 program target is met by B+C+the-1-admission-policies, NOT by deleting the bootstrap orchestration.

## Dependency Graph

```
Track C DoD + DOKS drill GREEN
        │
        ▼
A0: reclassify ~20 LIVE bootstrap scripts KEEP-OPERATOR (allowlist, read+label only)
        │
        ▼
A1: bootstrap-patch-admission-policies → chart values (cleanroom LIVE) → delete  [autonomous-after-cleanroom]
        │
        ▼
A2: ESO-generator MEET  ──▶ (a) RATIFY → migrate 3 generators (cleanroom+dogfood LIVE) → delete
        │                  └▶ (b) DEFER → ADR; 3 stay KEEP-OPERATOR
        ▼
Program close-out: script-count ≤30 confirmed + /dogfood full
```
A0 → A1 → A2 strictly sequential (A1's cleanroom must converge before touching credentials in A2).

---

## Phase A0: Reclassify LIVE Bootstrap Scripts KEEP-OPERATOR

### TA0.1 — Allowlist reclassification (read + label only, no deletion)

#### Objective
Reclassify the ~20 LIVE-orchestration bootstrap scripts to `KEEP-OPERATOR` in the allowlist with the per-script LIVE-orchestration evidence (which pulumi/kubectl/hydra/argocd call makes it non-delegatable), so the program stops treating the bootstrap domain as reducible beyond admission-policies + the ESO 3.

#### Evidence
The delegation map already proved only 1/5 DELEGATE-HELMFILE is clean. The remaining bootstrap scripts grep positive for `pulumi |doctl |helm |kubectl create token|hydra ... exec|argocd ... token` = LIVE orchestration. Labeling them KEEP-OPERATOR prevents future sessions re-hunting them (the false-orphan trap).

#### Files to edit
```
.claude/rules/shell-scripts-allowlist.md — change ~20 bootstrap rows REVIEW-NEEDED/grandfathered → KEEP-OPERATOR with rationale
docs/operations/helmfile-delegation-map.md — append the ~20 LIVE-orchestration classifications (extend the existing map)
```

#### Deep file dependency analysis
- The allowlist is the SoT for `no-new-bash` + the `unhomed-logic` disposition source. Marking KEEP-OPERATOR (vs REVIEW-NEEDED) tells `unhomed-logic` these are HOMED (operator tools), reducing the un-homed count to reflect reality (they have a home: operator-run LIVE bring-up).
- Downstream: `unhomed-logic` recount; future audits read these dispositions.

#### Deep Dives
- **Invariant:** a KEEP-OPERATOR row MUST cite the LIVE call (e.g., `clustercredential-create.sh` → `kubectl create token` + ClusterCredential CR apply). No bare "KEEP" — the rationale is the gate against gaming.
- **Edge case:** `bootstrap-patch-admission-policies.sh` is NOT in this set (it's the A1 delegation). `k3d-cross-cluster-heal.sh` is already KEEP-OPERATOR.

#### Tasks / TDD / Acceptance / DoD
1. Grep each bootstrap script for orchestration markers; record the load-bearing LIVE call. 2. Update ~20 allowlist rows → KEEP-OPERATOR + rationale. 3. Extend the delegation map. 4. Recount unhomed.
```
RED:     N/A (allowlist/doc) — verification: every reclassified row cites a specific LIVE call; unhomed-logic still PASS (KEEP-OPERATOR counts as homed)
GREEN:   allowlist + map updated
VERIFY:  /tmp/loopzero-ops quality unhomed-logic --check PASS; grep -c KEEP-OPERATOR shows the ~20 added
```
- [ ] ~20 rows reclassified with LIVE-call evidence; map extended; `unhomed-logic` PASS; `no-new-bash` PASS.

---

## Phase A1: admission-policies → Chart Values

### TA1.1 — Fold registry allow-list prefix into admission-policies chart values

#### Objective
Move the `k3d-loopzero-registry` registry-prefix that `bootstrap-patch-admission-policies.sh` patches into the warn-mode ClusterImagePolicy at runtime, into the `admission-policies` chart `.Values.registries.*` (set per-environment via helmfile), so the policy ships correct without a post-sync patch. Delete the script after cleanroom re-converges.

#### Evidence
Per the delegation map, this is the 1 clean delegation: the chart already uses `.Values.registries.*` (unified 2026-05-06); the patch script (B13) only adds the local-registry prefix to the warn-mode policy. Folding it into per-env values eliminates the patch.

#### Files to edit
```
infra/helm/admission-policies/templates/cluster-image-policy.yaml — consume .Values.registries.local for the warn-mode allow-list (if not already)
infra/helmfile/environments/{standalone,standalone-multi}.yaml — set admission-policies registries.local = k3d-loopzero-registry:5000
infra/helmfile/environments/{dev,prod}.yaml — NO local registry (prod uses DOCR)
charts/loopzero-admission-policies/... — apply the SAME change (dual-chart system: infra/helm + charts/ overlap)
infra/helmfile/scripts/bootstrap-patch-admission-policies.sh — DELETE after cleanroom validates
infra/helmfile/*.gotmpl — remove the presync hook that calls the patch script (if any)
.claude/rules/shell-scripts-allowlist.md + baselines — drop row, ratchet
```

#### Deep file dependency analysis
- `bootstrap-patch-admission-policies.sh` runs post-sync to add the k3d registry to the warn-mode policy (B13). The chart template must read `.Values.registries.local` and include it in the allow-list when set. The DUAL chart system (CLAUDE.md § Helm Chart Dual System) means BOTH `infra/helm/admission-policies/` AND `charts/loopzero-admission-policies/` need the change (the 3-chart overlap rule).
- Downstream: cleanroom bring-up (`task setup`) must produce a warn-mode policy that allows k3d-loopzero-registry WITHOUT the patch.

#### Deep Dives
- **Invariant:** prod MUST NOT get the local-registry prefix (security — only DOCR + allowed registries in prod). Per-env values: standalone/standalone-multi set it; dev/prod don't.
- **Edge case:** if the patch also restarts/reconciles the policy-controller, the chart change must ensure the policy is applied at sync time (sync-wave) so the warn-mode policy is present before tenant deploys.
- **Dual-chart:** forgetting `charts/loopzero-admission-policies/` = drift (the overlap trap). Both updated in this PR.

#### Tasks / TDD / Acceptance / DoD
1. Add `.Values.registries.local` consumption to BOTH admission-policies chart templates (RED: render test asserts k3d prefix present when value set, absent when not). 2. Set per-env values. 3. Remove the presync hook. 4. **Operator: `/cleanroom` → confirm warn-mode policy allows k3d-loopzero-registry + a deploy succeeds (no FROM_REGISTRY_NOT_ALLOWED).** 5. Delete script + row + ratchet.
```
RED:     Terratest render: admission-policies with registries.local=k3d-loopzero-registry → allow-list contains it; without → absent (both charts)
RED:     standalone env value set; prod env value absent
GREEN:   template consumes .Values.registries.local; per-env values
REFACTOR: none
VERIFY:  task helm:test (render); operator /cleanroom → deploy succeeds without the patch script
```
- [ ] Both charts render the k3d prefix per-env; prod excluded.
- [ ] **Cleanroom LIVE: platform converges + a deploy passes admission WITHOUT the patch script (OPERATOR).**
- [ ] Script deleted; baselines ratcheted; `dangling-script-refs` GREEN.
- [ ] Pass: /code-audit render-test coverage.

---

## Phase A2: ESO-Generator MEET + (conditional) Migration

### TA2.1 — Governance MEET: ESO Password generators for the 3 credential-generators

#### Objective
Hold the T2/T3 governance MEET (Change Governor → Production Design Reviewer + Security & Blast Radius Reviewer + Change Validation Planner + SRE) to decide whether to migrate `generate-secrets.sh` / `generate-build-secrets.sh` / `generate-runtime-secrets.sh` to ESO `Password` generator CRDs with deterministic seeding, OR formally defer (keep bash) with a ratified ADR.

#### Evidence
The delegation map: these 3 are "blocked on a design decision (Option B / ESO generators) — NOT a simple set: and CANNOT be eliminated without a T2/T3 MEET that also preserves the determinism contract." Credential-handling + multi-cluster + GitOps = automatic T2+ (pre-release-governance). Security has absolute veto.

#### Files to edit
```
docs/decisions/2026-05-24-eso-credential-generator-adr.md (NEW) — the MEET's ratified decision (migrate OR defer)
.claude/knowledge-base/meetings/MEET-2026-05-24-eso-credential-generators.md (NEW) — the meeting record
```

#### Deep Dives
- **The MEET must resolve:** (1) Can ESO `Password` generator seed deterministically per (cluster, secret) in our ESO version? (2) Does the determinism survive cluster rebuild (cleanroom)? (3) Blast radius if a generator drifts (all consumers break — SOURCE_UPLOAD_FAILED). (4) Security: is the seed-master-secret handling safe? Security veto applies.
- **Decision branches:** (a) RATIFY → TA2.2 migrates; (b) DEFER → ADR documents why (e.g., ESO version can't seed deterministically), 3 stay KEEP-OPERATOR, Track A yield = just admission-policies. Both are valid honest outcomes.

#### Tasks / TDD / Acceptance / DoD
1. Invoke `/meeting` with the ESO-generator context (delegation map + determinism evidence). 2. Produce MEET record + ADR. 3. If RATIFY → proceed TA2.2; if DEFER → reclassify the 3 KEEP-OPERATOR + close Track A.
```
RED:     N/A (governance) — verification: ADR has alternatives compared (ESO-deterministic vs keep-bash vs Sealed Secrets), security sign-off, determinism-preservation criterion, blast-radius analysis
GREEN:   MEET record + ratified ADR
VERIFY:  ADR exists with explicit decision + security reviewer sign-off
```
- [ ] MEET held; ADR ratified (migrate or defer) with security sign-off + determinism criterion + alternatives.

### TA2.2 — (CONDITIONAL on TA2.1=RATIFY) Migrate 3 generators to ESO Password generators

#### Objective
If ratified: replace the 3 generator scripts with ESO `Password` generator CRDs (deterministic seed per D3) + PushSecret to consuming namespaces; validate determinism via `/cleanroom` + `/dogfood full` (zero SOURCE_UPLOAD_FAILED / clone-signature regressions); delete the 3 scripts.

#### Evidence
Conditional — only if TA2.1 ratifies a determinism-preserving ESO design.

#### Files to edit
```
infra/helm/external-secrets/templates/password-generator-*.yaml (NEW) — ESO Password generator CRDs (deterministic seed)
infra/helm/*/...PushSecret CRs — push generated secrets to consuming namespaces (loopzero-system, argocd, velero-system, build, runtime)
infra/helmfile/scripts/generate-secrets.sh + generate-build-secrets.sh + generate-runtime-secrets.sh — DELETE after cleanroom+dogfood GREEN
.claude/rules/shell-scripts-allowlist.md + baselines — drop 3 rows, ratchet
docs/operations/credentials-required.md — update SoT (credentials-sot gate must stay GREEN)
```

#### Deep Dives
- **Determinism (D3):** the Password generator seeds from a per-cluster master (e.g., a bootstrap-time deterministic value), so rebuild → same password. MUST match `loopzero-dev-password` semantics in standalone (or update the consuming charts in lockstep).
- **Invariant:** `credentials-sot` + `pulumi-required-secrets` gates (wired prior session) MUST stay GREEN — the SoT doc + Pulumi externalization reflect the new ESO-generated secrets.
- **Edge case (EC-3, MUST FIX):** the Terratest render-test is STATIC — it proves the generator CR renders, NOT that it produces the SAME password across rebuilds. A random-seeded generator would pass the render-test and only break on the SECOND `task clean:all && task setup` (different password → SOURCE_UPLOAD_FAILED). Therefore the determinism criterion is NOT the render-test — it is a **two-rebuild password diff** (`kubectl get secret <name> -o jsonpath='{.data.password}'` after rebuild #1 == after rebuild #2). This is a BLOCKING DoD checkbox, not prose.

#### Tasks / TDD / Acceptance / DoD
1. Define ESO Password generators + PushSecrets (deterministic). 2. Render-test (Terratest) the generator/PushSecret CRs. 3. **Operator: `/cleanroom` + `/dogfood full` — zero credential regressions (deterministic across rebuild).** 4. Delete 3 scripts; update SoT; ratchet.
```
RED:     Terratest: ESO Password generator CR renders with deterministic seed config; PushSecret targets correct namespaces
RED:     credentials-sot + pulumi-required-secrets gates GREEN with new ESO secrets
GREEN:   implement generators + PushSecrets
VERIFY:  operator /cleanroom (deterministic creds across rebuild) + /dogfood full GREEN; 0 SOURCE_UPLOAD_FAILED
```
- [ ] **(EC-3 BLOCKING) Two-rebuild determinism proof:** `kubectl get secret <each> -o jsonpath='{.data.password}'` is IDENTICAL after rebuild #1 and rebuild #2 (`task clean:all && task setup` ×2). The render-test passing is NOT sufficient.
- [ ] (If ratified) 3 generators → ESO; cleanroom+dogfood GREEN (determinism preserved); 3 scripts deleted; SoT + gates GREEN; baselines ratcheted.
- [ ] (If deferred) 3 reclassified KEEP-OPERATOR; ADR documents the deferral; Track A closes with admission-policies as the sole deletion.

---

## Coverage Matrix

| # | Bootstrap script(s) | Task | Resolution |
|---|---|---|---|
| 1 | bootstrap-patch-admission-policies (1) | TA1.1 | → chart .Values.registries.local per-env; delete (cleanroom-validated) |
| 2 | generate-secrets + generate-build-secrets + generate-runtime-secrets (3) | TA2.1+TA2.2 | ESO MEET → migrate (cleanroom+dogfood) OR defer-KEEP |
| 3 | ~20 LIVE-orchestration bootstrap scripts | TA0.1 | reclassify KEEP-OPERATOR with LIVE-call evidence |
| 4 | k3d-cross-cluster-heal (1) | (already) | KEEP-OPERATOR (delegation map) |

**Coverage: 24/24 bootstrap scripts addressed. Expected removal: 1 (admission-policies) autonomously; +3 IF ESO MEET ratifies. ~20 formally KEEP-OPERATOR.**

## Global Definition of Done

- [ ] A0: ~20 bootstrap scripts reclassified KEEP-OPERATOR with LIVE-call evidence
- [ ] A1: admission-policies registry-prefix in chart values (both charts, per-env); script deleted; **cleanroom LIVE GREEN (deploy passes admission without the patch)**
- [ ] A2.1: ESO-generator MEET held; ADR ratified (migrate or defer) with security sign-off
- [ ] A2.2 (if ratified): 3 generators → ESO deterministic; **`/cleanroom` + `/dogfood full` GREEN (zero credential regressions)**; 3 deleted
- [ ] `credentials-sot` + `pulumi-required-secrets` gates GREEN
- [ ] `dangling-script-refs` + `taskfile-interface` + `script-count` GREEN; baselines ratcheted
- [ ] `/cross-validation 2026-05-24-bootstrap-helmfile-eso-migration` PASSED
- [ ] N/A — does not affect maturity scores
- [ ] **`/dogfood full` PASSED** (program close-out — this is the final track)

---

## Dogfood Gate (MANDATORY)

> Track A's LIVE gate is the **cleanroom** (credential determinism is the dominant cleanroom-regression theme). This is the FINAL track → its `/dogfood full` IS the program close-out gate.

### Execution
```bash
task clean:all && task setup && tilt up && task doctor   # A1: admission-policies must allow k3d-loopzero-registry without the patch
# A2.2 (if ratified): rebuild AGAIN to prove credential determinism across rebuilds
task clean:all && task setup   # second rebuild — passwords MUST match (deterministic)
/dogfood full
```

### Pass Criteria
- [ ] Cleanroom converges WITHOUT bootstrap-patch-admission-policies.sh (A1)
- [ ] (If A2.2) credentials deterministic across TWO rebuilds — zero SOURCE_UPLOAD_FAILED / clone-signature mismatch
- [ ] `/dogfood full`: zero S0/S1; Prova de Vida/Morte pass
- [ ] `script-count` ≤ 30 (program KPI) confirmed on the clean tree
- [ ] `credentials-sot` + `pulumi-required-secrets` GREEN

### On Failure
A credential regression (non-deterministic secret breaking a consumer) = the ESO migration violated D3. Restore the 3 generator scripts from git, mark the ESO migration DEFERRED in the ADR, keep them KEEP-OPERATOR. Never ship a non-deterministic credential path to hit a script count — the cleanroom determinism contract is load-bearing.
