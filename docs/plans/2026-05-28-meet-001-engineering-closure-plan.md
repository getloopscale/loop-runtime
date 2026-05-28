# Plan: MEET-2026-05-28-001 Engineering Closure (M07 Prod Cutover Path A)

> **Version 1.1** — Edge-case-plan amendment (2026-05-28): absorbed 9 MUST FIX edges identified by `/edge-case-plan` review. Affected tasks: T0.1 (drill context allowlist regex), T0.2 (ConfigMap set_at field), T1.5 (gate excludes comments + test files), T2.1 (audit_log trigger uses SERIAL id + correlation_id NULL), T2.2 (PG filter table_schema=public), T2.3 (chaos tests Skip when chaos-mesh absent), T4.1 (removed circuit-breaker metric, added loopzero_cosign_sign_total counter), T4.2 (removed circuit-breaker alert), T4.5 (runbook includes `argocd app history` discovery step). Plus 5 SHOULD TEST adopted as new test cases. Plus 3 DOCUMENT notes. Total 17 edges integrated.
>
> **Version 1.0** — Implements the 15 engineering-side mandatory actions from MEET-2026-05-28-001 (M07 production cutover REJECTED, sponsor accepted). Closes 7 of 11 CRITICAL findings (CR-1, CR-2 partial, CR-6, CR-8, CR-9, CR-10 partial, CR-11) and prepares the engineering surface for operator HARD STOPs (TUF ceremony + Pulumi up sigstore.ts + drill iter ≥56 + customer trial). Does NOT close operator-gated actions (3, 4, 6, 10, 11) which require separate operator-driven workstreams. Expected outcome: M07 production-readiness gap reduces from "24 mandatory actions" to "5 operator HARD STOPs"; engineering surface ready for re-meeting MEET-2026-05-28-002 once operator actions complete.

## Context

**What exists today:**
- M07 score 3.85/4.0 LIVE-validated for LOCAL k3d (per Rule 11 Evidence Bar §1, audit `2026-05-28-keyless-GREEN/07-build-pipeline.md`).
- 15 commits from Ralph iter 4+5 (commits 48915b2c → b3375ed0) implementing keyless signing + Sigstore stack + Rekor URL persistence.
- Plan `2026-05-28-M07-fulcio-cacert-keyless-LIVE-plan.md` v1.7 with 11/11 DoD CLOSED for LOCAL scope.

**What's broken or missing (engineering-side):**
- `--insecure-skip-verify` hardcoded unconditionally in `api/internal/argo/cosign.go:293` and `cosign_attest.go:110` (CVSS 9.1).
- `infra/k3d/build-cluster.yaml:56-64` carries false-parity comment ("matches DOKS production") inaccurate per `grep -rn anonymous-auth infra/pulumi*/` = zero hits.
- Migration 000019 added `image_attestation_rekor_url` column WITHOUT write-once trigger or audit_log row (CVSS 9.0).
- Zero `loopzero_cosign_*` / `loopzero_sign_*` / `loopzero_attest_*` / `loopzero_rekor_*` / `loopzero_fulcio_*` metrics exist.
- Step name `attest-image-{app}` is a string literal in 5+ files with no shared constant (the 3-layer SQL bug class can recur).
- No schema-mirror test diffing `INFORMATION_SCHEMA.columns` against struct + INSERT + SELECT + scanner.
- No `loopzero admin release inspect` diagnostic CLI.
- No `docs/operations/rollback-m07-cutover-runbook.md`.
- `infra/scripts/security/rekor-archive-old-entries.sh` is a STUB with Trillian API TODO.
- `workflow_builder.go:228-235` legacy `spec.Apps` branch missing `InsecureRegistry` field.
- No CI quality gate blocking `anonymous-auth=true` outside `infra/k3d/`.

**Evidence:**
- MEET record `.claude/knowledge-base/meetings/MEET-2026-05-28-001-m07-prod-cutover-readiness.md`
- 7/7 reviewers convergent on 11 CRITICAL findings
- 4 absolute vetoes invoked

## Objective

**Done = "Engineering surface of MEET-2026-05-28-001 closed (15/15 actions GREEN); only operator HARD STOPs remain blocking production cutover."**

Specific measurable goals:
1. `cosign --insecure-skip-verify` cannot execute in `IsProduction()` without explicit env opt-in + new ADR-ratified deadline.
2. Migration 000020 deployed: `image_attestation_rekor_url` write-once enforced + every mutation writes audit_log row.
3. Schema-mirror test in CI catches the next column-add SQL omission BEFORE merge.
4. 7 failure tests added covering column-cascade + enrich-failure + cosign extraction + NetworkPolicy contract + Fulcio-down + Rekor-down + concurrent enrich.
5. 8 new Prometheus metrics + 4 alerts + 1 Grafana dashboard for Sigstore pipeline observability.
6. `loopzero admin release inspect <id>` CLI returns attestation chain diagnostic without source code access.
7. Rollback runbook authored + dry-run rehearsed against LOCAL k3d cleanroom (operator rehearsal on drill = separate item).
8. CI quality gate blocks `anonymous-auth=true` outside `infra/k3d/`.
9. False-parity comment in `build-cluster.yaml` corrected with truthful documentation.
10. Step-name SoT helpers in `pkg/naming` or `api/internal/argo/stepnames.go` consumed by producer + consumer.
11. Legacy `workflow_builder.go:228-235` branch fixed OR deleted.
12. Rekor retention: Trillian admin API integration complete OR explicit storage budget documented in runbook with manual procedure.
13. 2 ADR amendments ratified (ADR-A LOCAL exception, ADR-B cosign v2.4.3 deadline).
14. Cutover ordering codified in `docs/operations/m07-cutover-sequence.md`.
15. All `task quality:gates` EXIT=0 across the 15 actions.

## ADRs

### D1: Gate `--insecure-skip-verify` via dedicated env var (not env-implicit-from-cluster)
- **Decision:** Add `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY` env (default false). Refactor cosign.go + cosign_attest.go scripts to render `--insecure-skip-verify` flag CONDITIONALLY on the env. Add `Config.Validate()` rule rejecting `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true && IsProduction() && TUF_ROOT_PINNED=false`.
- **Rationale:** Hardcoded flag means a single env-var flip (`LOOPZERO_COSIGN_KEYLESS=true`) bypasses SCT verification globally. Explicit insecure-flag with prod guard makes the supply-chain compromise scenario require explicit operator action, not implicit env coupling.
- **Consequences:** LOCAL k3d sets `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true` in helm values for sigstore-extras-enabled deployments; prod values omit OR explicitly false; Config.Validate() fails fast at API startup if violated.

### D2: Migration 000020 with BEFORE UPDATE trigger + audit_log row
- **Decision:** Add migration 000020 that creates: (a) BEFORE UPDATE trigger on `releases.image_attestation_rekor_url` rejecting UPDATE when OLD value NOT NULL; (b) AFTER INSERT trigger writing `audit_log` row with action=`release.attestation_url_set`; (c) AFTER UPDATE trigger ALSO writing audit_log (won't fire for valid updates due to trigger (a) — but defends future column UPDATE patches).
- **Rationale:** Mutable Rekor URL = attacker can swap honest URL with attacker-controlled Rekor entry pointer post-write. Write-once + audited eliminates the attack vector at DB layer.
- **Consequences:** Cannot retroactively patch wrong Rekor URLs without explicit migration (acceptable — forward-only policy already in place). audit_log table receives additional rows (~1 per release activation, low volume).

### D3: Step-name SoT in `pkg/naming`
- **Decision:** Create `pkg/naming/argo_steps.go` exporting `SignStepName(app string) string`, `AttestStepName(app string) string`, `BuildImageStepName(app string) string`, `SBOMStepName(app string) string`. Producer (`workflow_builder.go`, `cosign.go`, `cosign_attest.go`) and consumer (`build_reconcile_complete.go`) call these helpers.
- **Rationale:** String literal `"attest-image-" + app.Name` appears in 5+ files. A single typo (e.g., underscore) silently produces empty Outputs maps and DB columns go NULL — the exact failure mode that drove the 3-layer SQL bug closure. SoT helpers in `pkg/naming` (an existing canonical naming package) prevent this class.
- **Consequences:** New `pkg/naming` exports require Go module re-compile but no API breakage. Helper functions are pure + unit-testable. Round-trip test asserts producer name == consumer expected key.

### D4: Schema-mirror test as Go integration test (not separate tool)
- **Decision:** Add `api/internal/db/schema_mirror_test.go` (build-tagged `//go:build integration`). Test connects to test PG, queries `INFORMATION_SCHEMA.columns` for `releases` (and other M07-touched tables: `builds`, `release_app_attestations`), then asserts every column is referenced in: (a) the struct (via reflection on `db.Release` and others), (b) the INSERT SQL (via parser or string match on column name in `INSERT INTO ... (col1, col2, ...)`), (c) at least one SELECT SQL, (d) `scanReleaseFields` (via reflection on the Scan args).
- **Rationale:** Catches the entire bug class — the next column-add will fail the test if any of the 4 sites diverges. Cheaper than schema-as-code (sqlc requires regen + may not handle all our edge cases). Test runs in CI under `integration` tag.
- **Consequences:** ~150 LOC test infrastructure; runs in CI ~5s; catches column-cascade omission BEFORE merge.

### D5: `loopzero admin release inspect <id>` CLI uses existing admin API + new endpoint
- **Decision:** Add new admin endpoint `GET /api/v1/admin/releases/{id}/inspect` returning JSON with `{release_id, image_signature, image_digest, image_signed_at, image_attestation_rekor_url, build_id, workflow_id, signing_path: "keyed"|"keyless"|"unsigned", attestation_path: "present"|"absent"|"failed"}`. CLI command `loopzero admin release inspect <id> --json` calls this endpoint.
- **Rationale:** On-call SRE at 3 AM cannot grep source. Diagnostic CLI surface eliminates source-access dependency for the canonical "is this release signed + attested correctly?" question.
- **Consequences:** New CLI command (1 new file `cli/src/commands/admin/release.ts`); new admin endpoint (1 new handler `api/internal/routes/admin_release_inspect.go`); both follow existing admin route patterns.

### D6: ADR-A formalize LOCAL exception to ADR-2026-05-27-T2.2-OFFLINE-TUF
- **Decision:** Author `decisions/2026-05-28-T2.2-OFFLINE-TUF-LOCAL-EXCEPTION.md` amending ADR-2026-05-27 to explicitly authorize the LOCAL helm-based Sigstore path (`infra/helm/sigstore-extras/` + `bootstrap-sigstore-local.sh`) with hard boundary: `SIGSTORE_CONTEXT=k3d-loopzero-build` ONLY. Production stays Pulumi sigstore.ts.
- **Rationale:** Architect veto identified the LOCAL chart re-violates "Pulumi sole writer" without an ADR. Formalize the exception with explicit boundary.
- **Consequences:** Bootstrap script adds context-name assertion at top: `[[ "$CONTEXT" == "k3d-loopzero-build" ]] || die "Not k3d-loopzero-build context — refusing to run"`.

### D7: ADR-B cosign v2.4.3 vs v3.x trade-off + 90d forced-upgrade deadline
- **Decision:** Author `decisions/2026-05-28-cosign-v2.4.3-deadline.md` recording: cosign v2.4.3 chosen for LIVE GREEN at Ralph iter 4 (v3.0.6 client-side reject discovered); v3.x revisit deadline T0+90d post-TUF-ceremony. Mechanism: `infra/scripts/security/check-cosign-cutover-deadline.sh` extended to check `LOOPZERO_COSIGN_VERSION_TARGET=v3` ConfigMap + reject merges past deadline.
- **Rationale:** v2 line in maintenance; v3 has TUF-from-signing-config which is the prod target. Without ADR + deadline, v2.4.3 lock-in becomes accidental tech debt.
- **Consequences:** Deadline ratchet mechanism extended; operator notified at T0+60d to schedule v3 upgrade work.

### D8: NetworkPolicy podSelector hardening with SA allowlist
- **Decision:** Replace `from.namespaceSelector kubernetes.io/metadata.name=argo` (bare) with explicit `from.podSelector` matching `app.kubernetes.io/managed-by: argo-workflow-controller` in `infra/helm/sigstore-extras/templates/networkpolicy-*.yaml`. Add Argo workflow ServiceAccount allowlist via `infra/helm/sigstore-extras/values.yaml` `allowedServiceAccounts: [argo-workflow]`.
- **Rationale:** Bare namespaceSelector lets ANY pod in argo namespace (compromised workflow, debug containers, side-channel pods) reach Sigstore. podSelector + SA allowlist restricts to controller-managed workflow pods.
- **Consequences:** sigstore-extras chart values gains `allowedServiceAccounts` list; helm test added asserting rendered NP has correct podSelector.

### D9: Rollback runbook + dry-run on LOCAL k3d cleanroom (not drill)
- **Decision:** Author `docs/operations/rollback-m07-cutover-runbook.md` covering: (a) loopzero-api image rollback via `argocd app rollback`, (b) migration 000019/000020 column-leave-NULL fallback (nullable columns), (c) cosign v2.4.3 → v3.0.6 re-upgrade procedure, (d) keyless → keyed emergency flip via ConfigMap + workflow template re-render. Engineering dry-run on LOCAL k3d cleanroom. Operator rehearsal on drill = separate workstream.
- **Rationale:** SRE veto requires documented + tested rollback. Engineering can author + dry-run LOCAL; operator must drill it before prod claim.
- **Consequences:** 1 new runbook + 1 cleanroom dry-run evidence file. Drill rehearsal deferred to operator workstream.

## Dependency Graph

```
Phase 0 (Foundation + ADRs) ──▶ Phase 1 (Security gates) ──▶ Phase 2 (Data integrity)
       │                              │                              │
       │                              ▼                              ▼
       │                       Phase 3 (NP hardening) ──────▶ Phase 4 (Observability + diag)
       │                              │                              │
       └──────────────────────────────┴──────────────────────────────┘
                                      ▼
                              Final Phase: Dogfood QA
```

- **Phase 0** (ADRs): blocks NOTHING but informs all subsequent phases (ratification can happen in parallel with engineering).
- **Phase 1** (Security): sequential — D1 + D3 blocking. T1.1 (step-name SoT) must land BEFORE T1.2 (gate flag) so the cosign script generators consume helpers.
- **Phase 2** (Data integrity): can run in parallel with Phase 3 + Phase 4 after Phase 1 complete.
- **Phase 3** (NP hardening): can run in parallel with Phase 2 + Phase 4 after Phase 1 complete.
- **Phase 4** (Observability): can run in parallel with Phase 2 + Phase 3 after Phase 1 complete.

---

## Phase 0: Foundation + ADRs

**Objective:** Ratify architectural decisions D6 + D7 + codify cutover ordering. Enables subsequent phases to reference stable ADR IDs.

### T0.1 — ADR-A: LOCAL exception to ADR-2026-05-27-T2.2-OFFLINE-TUF

#### Objective
Formalize the LOCAL helm-based Sigstore path as an explicit, bounded exception to the "Pulumi sole writer" decision from ADR-2026-05-27.

#### Evidence
Principal Architect review (MEET-2026-05-28-001 §C1): "LOCAL bash path resurrects the exact same anti-pattern. Mitigation must be either (a) explicit context guard in bootstrap script OR (b) ADR amendment formalizing the local exception."

#### Files to edit
```
.claude/knowledge-base/decisions/2026-05-28-T2.2-OFFLINE-TUF-LOCAL-EXCEPTION.md — NEW
infra/scripts/local/bootstrap-sigstore-local.sh — add context guard
```

#### Deep file dependency analysis
- **Decisions ADR file (NEW):** Standalone document per ADR convention. References ADR-2026-05-27-T2.2-OFFLINE-TUF as parent decision. Format: same as existing ADRs in `.claude/knowledge-base/decisions/`.
- **bootstrap-sigstore-local.sh:** Currently checks `CONTEXT="${SIGSTORE_CONTEXT:-k3d-loopzero-build}"` at top. Add allowlist regex gate covering canonical LOCAL + drill prod-identical LOCAL contexts.

#### Deep Dives
- **ADR boundary (EC-9 fix):** the exception is bound to a regex allowlist of LOCAL contexts. Default allowlist: `^k3d-loopzero-(build|drill-build)$`. Drill prod-identical (plan 2026-05-16) provisions ephemeral cluster `k3d-loopzero-drill-build` which must reuse the LOCAL bootstrap path. Future LOCAL contexts require ADR amendment + allowlist entry.
- **Hard gate semantics:** `die` exit 1 with explanation `"sigstore-extras + bootstrap-sigstore-local.sh authorized ONLY for contexts matching $SIGSTORE_ALLOWED_CONTEXTS_REGEX (default ^k3d-loopzero-(build|drill-build)$) per ADR-LOCAL-EXCEPTION; prod uses Pulumi sigstore.ts."` Operator override via env `SIGSTORE_ALLOWED_CONTEXTS_REGEX` is logged WARN (audit signal).

#### Tasks
1. Author ADR-A document with decision/rationale/consequences sections.
2. Reference ADR-2026-05-27 as parent.
3. Add regex allowlist guard at top of bootstrap-sigstore-local.sh (line ~30 after pre-flight section):
   ```bash
   ALLOWED_RE="${SIGSTORE_ALLOWED_CONTEXTS_REGEX:-^k3d-loopzero-(build|drill-build)$}"
   [[ "$CONTEXT" =~ $ALLOWED_RE ]] || die "context $CONTEXT not in $ALLOWED_RE; prod uses Pulumi sigstore.ts"
   ```
4. Add unit test in `infra/tests/check-bootstrap-sigstore-context_test.sh` asserting:
   - k3d-loopzero-build PASSES
   - k3d-loopzero-drill-build PASSES
   - k3d-loopzero-mgmt FAILS
   - prod context (e.g., do-sfo3-loopzero-mgmt) FAILS
   - SIGSTORE_ALLOWED_CONTEXTS_REGEX override emits WARN to stderr
5. Commit with `governance(M07): ADR-A LOCAL exception formalized` message.

#### TDD
```
RED:     test_bootstrap_sigstore_rejects_non_local_context() — CONTEXT=k3d-loopzero-mgmt → exit 1
RED:     test_bootstrap_sigstore_accepts_k3d_loopzero_build_context()
RED:     test_bootstrap_sigstore_accepts_k3d_loopzero_drill_build_context()
RED:     test_bootstrap_sigstore_rejects_prod_doks_context()
RED:     test_bootstrap_sigstore_override_regex_emits_warn() — operator override audit signal
GREEN:   Add regex allowlist gate after pre-flight
REFACTOR: None expected
VERIFY:  bash infra/tests/check-bootstrap-sigstore-context_test.sh
```

#### Acceptance Criteria
- [ ] ADR document exists at `.claude/knowledge-base/decisions/2026-05-28-T2.2-OFFLINE-TUF-LOCAL-EXCEPTION.md`
- [ ] ADR cross-links to ADR-2026-05-27-T2.2-OFFLINE-TUF
- [ ] Bootstrap script rejects non-k3d-loopzero-build context with exit 1
- [ ] Hermetic test passes
- [ ] `task quality:gates` EXIT=0

#### DoD
- [ ] ADR + script gate committed
- [ ] Hermetic test in CI
- [ ] No regression on existing bootstrap-sigstore-local.sh idempotency

---

### T0.2 — ADR-B: cosign v2.4.3 vs v3.x trade-off + 90d deadline

#### Objective
Document the cosign v2.4.3 choice as conscious trade-off, not accidental lock-in, with hard upgrade deadline.

#### Evidence
Principal Architect review (MEET-2026-05-28-001 §C2): "cosign v2.4.3 lock-in is accidental, not conscious. No ADR documents this decision."

#### Files to edit
```
.claude/knowledge-base/decisions/2026-05-28-cosign-v2.4.3-deadline.md — NEW
infra/scripts/security/check-cosign-cutover-deadline.sh — extend with v3 upgrade gate
```

#### Deep file dependency analysis
- **ADR file (NEW):** Standalone document. References cosign v3.x release notes + ADR-2026-05-12-cosign-keyed-keyless-90d (existing deadline pattern).
- **check-cosign-cutover-deadline.sh:** Existing operator-time deadline ratchet script. Extend with new check: if `LOOPZERO_COSIGN_VERSION_TARGET=v3` ConfigMap present AND past T0+60d, log WARN; past T0+90d, exit 1.

#### Deep Dives
- **Deadline T0 anchoring (EC-1 fix):** T0 is NOT the script execution time NOR ADR ratification date globally — it MUST come from an explicit `set_at` field in the ConfigMap (`set_at: "YYYY-MM-DD"`). Script REJECTS ConfigMap missing `set_at` with exit 1 + clear error. This prevents two bypasses: (a) operator rewriting ConfigMap to reset the clock, (b) ambiguity if ConfigMap predates the deadline logic.
- **Ratchet mechanism:** ConfigMap `loopzero-system/loopzero-cosign-version-config` with `target_version: v3` AND `set_at: "YYYY-MM-DD"`. Operator updates BOTH fields when scheduling upgrade; deadline check parses set_at + computes T0+60d / T0+90d.

#### Tasks
1. Author ADR-B document.
2. Cross-link to ADR-2026-05-12-cosign-keyed-keyless-90d (precedent).
3. Extend check-cosign-cutover-deadline.sh:
   - Read ConfigMap `loopzero-cosign-version-config`
   - If target_version absent: PASS (no upgrade pending)
   - If target_version=v3 AND `set_at` absent: FAIL with `"target_version requires set_at field; refusing ambiguous deadline"`
   - Parse set_at as ISO date; compute T0+60d / T0+90d
   - If past T0+60d: log WARN
   - If past T0+90d: exit 1
4. Add hermetic test in `infra/tests/check-cosign-v3-deadline_test.sh` covering 5 paths (no target, target+set_at within 60d, 60-90d WARN, past 90d FAIL, target without set_at FAIL).
5. Add ConfigMap template to `infra/helm/admission-policies/templates/cosign-version-config.yaml` with default `target_version: v2.4.3` (no upgrade pending, set_at omitted because target=current version means no countdown).

#### TDD
```
RED:     test_cosign_v3_deadline_within_60d_passes()
RED:     test_cosign_v3_deadline_60_to_90d_warns_but_passes()
RED:     test_cosign_v3_deadline_past_90d_fails()
RED:     test_cosign_v3_deadline_no_target_configmap_passes() (no upgrade pending)
RED:     test_cosign_v3_deadline_target_without_set_at_fails() — EC-1 ambiguity guard
GREEN:   Extend check-cosign-cutover-deadline.sh with v3 upgrade logic + set_at parser
REFACTOR: Refactor common ConfigMap-read logic if both keyless + v3 share parsing
VERIFY:  bash infra/tests/check-cosign-v3-deadline_test.sh
```

#### Acceptance Criteria
- [ ] ADR-B exists at `.claude/knowledge-base/decisions/2026-05-28-cosign-v2.4.3-deadline.md`
- [ ] check-cosign-cutover-deadline.sh extended with v3 deadline logic + set_at REQUIRED when target_version=v3
- [ ] Hermetic test covers 5 paths (no upgrade, within 60d, 60-90d, past 90d, target without set_at)
- [ ] ConfigMap helm template added with default `v2.4.3`

#### DoD
- [ ] ADR + script + test + ConfigMap committed
- [ ] `task quality:gates` EXIT=0
- [ ] Allowlist updated if any new .sh file added

---

### T0.3 — Cutover ordering codified

#### Objective
Document migration-first → API redeploy → workflow templates → Sigstore ordering with explicit operator checklist.

#### Evidence
Deploy Guardian review (MEET-2026-05-28-001 §CR-11): "release_store.go:144 hard-references image_attestation_rekor_url. If API redeploys BEFORE migration completes, every INSERT fails. Cutover MUST be: migration-first, then API redeploy."

#### Files to edit
```
docs/operations/m07-cutover-sequence.md — NEW
infra/scripts/production/m07-cutover-preflight.sh — NEW (validates ordering preconditions)
```

#### Deep file dependency analysis
- **Cutover sequence doc (NEW):** Operator-facing runbook with explicit numbered steps + pre-conditions + abort triggers.
- **m07-cutover-preflight.sh (NEW):** Script that validates:
  - Migration 000019 + 000020 applied to target DB (via `psql`)
  - DOCR image `loopzero-cosign-signer:v2.4.3-r0` exists (via `doctl registry list-images`)
  - DOCR image `loopzero-api:<target-tag>` exists
  - Chart `loopzero-build-cwts` version bumped + published (via `helm show chart oci://...`)
  - TUF ceremony output present in Pulumi stack config (via `pulumi stack output`)

#### Deep Dives
- **Ordering:** (1) Migration 000019 + 000020 → (2) DOCR image publishes → (3) Chart bumps → (4) Pulumi sigstore.ts up → (5) ArgoCD app sync → (6) Validate first build → (7) Flip `clusterImagePolicyKeyless.mode: warn` → `enforce` after 7d dual-signing GREEN.
- **Abort triggers:** any step failure aborts cutover; rollback runbook T4.5 executed.

#### Tasks
1. Author `docs/operations/m07-cutover-sequence.md` with 7 numbered steps + pre-conditions + abort triggers.
2. Author `infra/scripts/production/m07-cutover-preflight.sh` with 5 pre-condition checks.
3. Add hermetic test `infra/tests/check-m07-cutover-preflight_test.sh` (mock all external calls).
4. Update allowlist for new `.sh` file.

#### TDD
```
RED:     test_cutover_preflight_fails_when_migration_not_applied()
RED:     test_cutover_preflight_fails_when_docr_image_missing()
RED:     test_cutover_preflight_fails_when_chart_not_bumped()
RED:     test_cutover_preflight_fails_when_tuf_root_missing()
RED:     test_cutover_preflight_passes_when_all_preconditions_met()
GREEN:   Implement preflight script with 5 checks
REFACTOR: Extract common `die_unless` helper
VERIFY:  bash infra/tests/check-m07-cutover-preflight_test.sh
```

#### Acceptance Criteria
- [ ] Cutover sequence doc exists with 7 numbered steps
- [ ] Preflight script with 5 checks committed
- [ ] Hermetic test covers all 5 fail paths + happy path
- [ ] Allowlist + baseline updated (script-count 132 → 133)

#### DoD
- [ ] All files committed
- [ ] Hermetic test in CI
- [ ] `task quality:gates` EXIT=0

---

## Phase 1: Security gates (CR-1 + CR-2)

**Objective:** Eliminate the 2 highest-severity vetoes: hardcoded `--insecure-skip-verify` (CR-1) and false-parity `anonymous-auth=true` claim (CR-2).

### T1.1 — Step-name SoT helpers in pkg/naming

#### Objective
Single source of truth for Argo step names consumed by producer + consumer, preventing future SQL-omission-class bugs.

#### Evidence
Contract API↔Build review (MEET-2026-05-28-001 §1): "Step naming convention is grep-fragile. `sign-image-{name}` / `attest-image-{name}` are inline `fmt.Sprintf` literals in 5 files with no shared constant or helper. **Fix required:** extract `argo.SignStepName(app)`, `argo.AttestStepName(app)` helpers used by BOTH producer (workflow_builder.go) and consumer (build_reconcile_complete.go)."

#### Files to edit
```
pkg/naming/argo_steps.go — NEW
pkg/naming/argo_steps_test.go — NEW
api/internal/argo/cosign.go — replace fmt.Sprintf("sign-image-%s", ...) with naming.SignStepName(app)
api/internal/argo/cosign_attest.go — replace fmt.Sprintf("attest-image-%s", ...) with naming.AttestStepName(app)
api/internal/argo/workflow_builder.go — replace all step name literals
api/internal/services/build_reconcile_complete.go — replace consumer literals
```

#### Deep file dependency analysis
- **pkg/naming/argo_steps.go (NEW):** Pure Go helpers, no dependencies beyond stdlib. Returns string. Naming convention follows existing `pkg/naming/*.go` style.
- **cosign.go:154, 321:** currently uses `fmt.Sprintf("sign-image-%s", p.AppName)`. Replace with `naming.SignStepName(p.AppName)`.
- **cosign_attest.go:184:** currently uses `fmt.Sprintf("attest-image-%s", p.AppName)`. Replace with `naming.AttestStepName(p.AppName)`.
- **workflow_builder.go:451-509:** multiple sites using string literals. Replace all 6 occurrences.
- **build_reconcile_complete.go:224-226, 273:** consumer side. Currently `"sign-image-" + app.Name`, `"attest-image-" + app.Name`, etc. Replace with naming helpers.

#### Deep Dives
- **Helper signatures:**
  ```go
  func SignStepName(app string) string  // "sign-image-{app}"
  func AttestStepName(app string) string  // "attest-image-{app}"
  func BuildImageStepName(app string) string  // "build-image-{app}"
  func SBOMStepName(app string) string  // "sbom-{app}"
  ```
- **Round-trip invariant:** for any app string `a`, `consumer.parse(producer.SignStepName(a)) == a`. Test asserts.

#### Tasks
1. Create `pkg/naming/argo_steps.go` with 4 exported helpers.
2. Create `pkg/naming/argo_steps_test.go` with unit tests + round-trip property test.
3. Refactor `cosign.go` to use helpers (2 sites).
4. Refactor `cosign_attest.go` to use helpers (1 site).
5. Refactor `workflow_builder.go` to use helpers (6 sites).
6. Refactor `build_reconcile_complete.go` to use helpers (4 sites).
7. Add round-trip test: producer.AttestStepName("hello") consumed by enrichment matches.

#### TDD
```
RED:     TestSignStepName_FormatStable() — asserts exact string format
RED:     TestAttestStepName_FormatStable()
RED:     TestStepName_RoundTrip() — producer name → map key → consumer match
RED:     TestSignStepName_EmptyAppName_PanicsExplicitly() — EC-10: decision is panic with clear message (not silent trailing-dash)
GREEN:   Implement 4 helpers in argo_steps.go (panic on empty app name with descriptive message)
REFACTOR: Replace all 13 call sites
VERIFY:  cd api && go test ./internal/argo/ ./internal/services/ -count=1
         cd pkg && go test ./naming/ -count=1
```

#### Acceptance Criteria
- [ ] 4 helpers exported from pkg/naming
- [ ] All 13 call sites refactored
- [ ] Round-trip test PASS
- [ ] `cyclomatic complexity <= 5` for helpers (per /code-audit)
- [ ] Zero `fmt.Sprintf("sign-image-...")` or `"attest-image-" +` literals remaining (grep gate)

#### DoD
- [ ] All tests GREEN
- [ ] grep `'"\(sign\|attest\|build\)-image-"' api/internal/` returns only test files
- [ ] `task quality:gates` EXIT=0

---

### T1.2 — Gate `--insecure-skip-verify` behind dedicated env

#### Objective
Make `--insecure-skip-verify` conditional on `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY` env (default false) + add `Config.Validate()` guard rejecting it in prod without TUF.

#### Evidence
Platform Security review (MEET-2026-05-28-001 §CR-1): "`--insecure-skip-verify` is HARDCODED unconditionally at cosign.go:293 and cosign_attest.go:110. NO env-gating. Ships verbatim to DOKS prod when CosignKeyless=true."

#### Files to edit
```
api/internal/config/config.go — add CosignInsecureSkipSCT field + Validate() rule
api/internal/argo/cosign.go — render --insecure-skip-verify conditionally
api/internal/argo/cosign_attest.go — same
api/internal/argo/workflow_submitter.go — pass InsecureSkipSCT to InfraParams
api/cmd/setup_workflows.go — wire env var
api/cmd/controller/setup_services.go — wire env var
infra/helm/loopzero-api/values.yaml — add env var (default false)
infra/helm/loopzero-api/values.yaml.gotmpl — local override sets true
```

#### Deep file dependency analysis
- **config.go:319-322:** existing block defines `CosignKeyless`, `CosignAttestEnabled`. Add `CosignInsecureSkipSCT bool` from env `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY` default false.
- **config.go:467-490 Validate():** add rule:
  ```go
  if c.CosignInsecureSkipSCT && c.IsProduction() && !c.CosignTUFRootPinned {
      return fmt.Errorf("LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true requires TUF root pinning (LOOPZERO_COSIGN_TUF_ROOT_PINNED=true) in production")
  }
  ```
- **cosign.go:289-297 (keyless sign block):** render `--insecure-skip-verify` conditionally:
  ```go
  insecureSCTFlag := ""
  if p.InsecureSkipSCT {
      insecureSCTFlag = " --insecure-skip-verify"
  }
  // ... fmt.Sprintf("cosign sign --yes%s%s ...", insecureFlag, insecureSCTFlag, ...)
  ```
- **cosign_attest.go:106-110:** same pattern for attest.
- **workflow_submitter.go:** add `InsecureSkipSCT bool` to InfraParams; propagate to `CosignSignParams` + `CosignAttestParams`.
- **setup_workflows.go + setup_services.go:** wire `cfg.CosignInsecureSkipSCT` into `InfraParams.InsecureSkipSCT`.
- **helm loopzero-api values.yaml:** default `false`; LOCAL k3d values override to `true`.

#### Deep Dives
- **`CosignTUFRootPinned` field:** new bool from env `LOOPZERO_COSIGN_TUF_ROOT_PINNED` (default false). Set to true ONLY after operator TUF ceremony complete + Pulumi sigstore.ts deploys with `tufPreMintedKeys`.
- **Backward compat:** existing LOCAL k3d deployments need helm values override to set both `CosignInsecureSkipSCT=true` AND `CosignTUFRootPinned=false`. The validate rule passes because `IsProduction()=false` in LOCAL.

#### Tasks
1. Add `CosignInsecureSkipSCT` + `CosignTUFRootPinned` to `Config` struct.
2. Wire env vars in `loadConfig()`.
3. Add Validate() rule rejecting `insecureSkipSCT=true && IsProduction() && !TUFRootPinned`.
4. Refactor cosign.go to render flag conditionally.
5. Refactor cosign_attest.go same.
6. Update workflow_submitter.go InfraParams.
7. Update setup_workflows.go + setup_services.go wires.
8. Update helm loopzero-api values.yaml.
9. Add unit tests for Config.Validate() rejection + LIVE conditional rendering.

#### TDD
```
RED:     TestConfig_Validate_RejectsInsecureSkipSCT_InProd_WithoutTUF()
RED:     TestConfig_Validate_AcceptsInsecureSkipSCT_InLocal()
RED:     TestConfig_Validate_AcceptsInsecureSkipSCT_InProd_WithTUF()
RED:     TestCosignSignScript_RendersInsecureFlag_WhenEnabled()
RED:     TestCosignSignScript_OmitsInsecureFlag_WhenDisabled()
RED:     TestCosignAttestScript_RendersInsecureFlag_WhenEnabled()
RED:     TestCosignAttestScript_OmitsInsecureFlag_WhenDisabled()
RED:     TestCosignSignScript_KeyedMode_OmitsInsecureFlag_RegardlessOfEnv() — EC-11: keyless=false → flag always omitted (flag is keyless-only concern)
GREEN:   Implement gating + conditional rendering
REFACTOR: Extract `insecureSCTFlagFor(params)` helper if duplication
VERIFY:  cd api && go test ./internal/config/ ./internal/argo/ -count=1
```

#### Acceptance Criteria
- [ ] Config.Validate() rejects unsafe combo
- [ ] cosign scripts render conditionally
- [ ] LOCAL k3d helm values set flag=true
- [ ] Prod helm values leave default=false
- [ ] grep `"insecure-skip-verify"` returns only conditional code paths + comments + tests
- [ ] Round-trip test: API startup with `LOOPZERO_COSIGN_INSECURE_SKIP_SCT_VERIFY=true` + `IS_PRODUCTION=true` + `LOOPZERO_COSIGN_TUF_ROOT_PINNED=false` → API exits 1 with clear error

#### DoD
- [ ] All tests GREEN
- [ ] LIVE smoke: LOCAL build still produces signed image (flag rendered)
- [ ] `task quality:gates` EXIT=0
- [ ] cyclomatic complexity <= 8

---

### T1.3 — Fix workflow_builder.go:228-235 legacy `spec.Apps` branch

#### Objective
Either add missing `InsecureRegistry: b.infraParams.RegistryInsecure` to legacy attest branch, OR delete the dead code path.

#### Evidence
Contract API↔Build review (MEET-2026-05-28-001 §2): "workflow_builder.go:228-235 (legacy spec.Apps path) does NOT set `InsecureRegistry: b.infraParams.RegistryInsecure`, while the parallel sign-template call (line 213-220) does. Currently dead code in prod (Plan always set per build_orchestrator_submit.go:170), but if a future submission path bypasses Plan, attest would default to no-insecure-registry flags."

#### Files to edit
```
api/internal/argo/workflow_builder.go — DELETE lines 228-235 (preferred) OR add InsecureRegistry field
```

#### Deep file dependency analysis
- **workflow_builder.go:228-235:** legacy branch active when `b.spec.Plan == nil`. Per `build_orchestrator_submit.go:170`, Plan is ALWAYS set. The legacy branch is dead code.
- **Decision:** DELETE preferred (per CLAUDE.md "Legacy code is an error. Delete it."). If deletion blocks tests, fall back to adding the missing field.

#### Deep Dives
- **Grep verification:** `grep -rn "spec.Apps" api/internal/` should show only legacy branch and possibly tests. Verify no production code reaches `Plan == nil` path.

#### Tasks
1. Verify `spec.Plan == nil` is unreachable: grep all `submitBuild` / `Plan = ` patterns.
2. Delete lines 228-235 + accompanying if-else branch.
3. Add test asserting `Plan == nil` is unreachable from public API.
4. If deletion breaks tests: fall back to adding `InsecureRegistry: b.infraParams.RegistryInsecure` to legacy branch + document why.

#### TDD
```
RED:     TestWorkflowBuilder_LegacyAppsBranch_IsUnreachable() — fails before deletion (branch exists)
GREEN:   Delete legacy branch
REFACTOR: Simplify control flow (remove now-unreachable else)
VERIFY:  cd api && go test ./internal/argo/ -count=1
```

#### Acceptance Criteria
- [ ] Legacy branch deleted OR (fallback) InsecureRegistry field added
- [ ] grep `spec.Apps` shows only Plan branch + tests
- [ ] All existing tests still GREEN

#### DoD
- [ ] Branch removed/fixed
- [ ] `task quality:gates` EXIT=0
- [ ] Reduced LOC count in workflow_builder.go

---

### T1.4 — Remove false-parity comment in build-cluster.yaml + decide prod path

#### Objective
Replace inaccurate "matches DOKS production" comment with truthful documentation of the LOCAL-only nature of `anonymous-auth=true` + explicit decision on prod path.

#### Evidence
Security Blast Radius review (MEET-2026-05-28-001 §CR-2): "The code comment asserts the flag 'guarantees behavior matches DOKS production (CTO Golden Rule 0).' Verified false: `grep -rn anonymous-auth infra/pulumi*/` returns ZERO hits."

#### Files to edit
```
infra/k3d/build-cluster.yaml — replace lines 54-67 with truthful comment
.claude/knowledge-base/decisions/2026-05-28-anonymous-auth-prod-path.md — NEW (ADR documenting prod path)
```

#### Deep file dependency analysis
- **build-cluster.yaml:54-67:** current comment + extraArg. Replace with truthful explanation.
- **ADR (NEW):** documents the decision — chosen path is "prod uses authenticated discovery via Fulcio SA with ClusterRoleBinding system:service-account-issuer-discovery; LOCAL k3d needs anonymous-auth=true workaround due to ProjectedVolume/SA timing in k3d fresh clusters."

#### Deep Dives
- **Decision:** Option A (prod = authenticated discovery via Fulcio SA, anonymous-auth STAYS OFF on DOKS). LOCAL k3d retains the flag as a documented workaround due to k3d-specific timing.
- **Truthful comment text:**
  ```yaml
  # LOCAL k3d ONLY: enables anonymous OIDC discovery so Fulcio can validate
  # K8s SA tokens during keyless signing. Production DOKS uses authenticated
  # discovery via Fulcio SA + ClusterRoleBinding (see ADR-2026-05-28-anonymous-auth-prod-path.md).
  # NOT applicable to prod. Quality gate `check-anonymous-auth-allowlist`
  # rejects this flag outside `infra/k3d/`.
  ```

#### Tasks
1. Author ADR documenting prod path (authenticated discovery via Fulcio SA).
2. Replace lines 54-67 in build-cluster.yaml with truthful comment.
3. Cross-link to ADR.
4. Update `infra/k3d/mgmt-cluster.yaml` + `workload-cluster.yaml` if they reference DOKS parity in any way (sanity check).

#### TDD
```
RED:     test_build_cluster_yaml_comment_does_not_claim_doks_parity() — greps for "matches DOKS" / "Golden Rule 0" / "guarantees behavior" in build-cluster.yaml
GREEN:   Replace comment block
REFACTOR: None
VERIFY:  bash infra/tests/check-build-cluster-comment_test.sh
```

#### Acceptance Criteria
- [ ] ADR exists documenting prod path
- [ ] build-cluster.yaml comment is truthful
- [ ] grep returns zero false-parity claims
- [ ] Hermetic test PASS

#### DoD
- [ ] ADR + comment fix committed
- [ ] grep gate enforces truthfulness
- [ ] `task quality:gates` EXIT=0

---

### T1.5 — Quality gate: `anonymous-auth=true` outside k3d allowlist

#### Objective
CI quality gate that fails if `anonymous-auth=true` appears anywhere outside `infra/k3d/`.

#### Evidence
Security Blast Radius review (MEET-2026-05-28-001 §C2 conditional release): "Add `pkg/quality/<gate>` that greps Pulumi/DOKS provisioning for `anonymous-auth=true` → CI fails."

#### Files to edit
```
pkg/quality/anonymousauth/check.go — NEW
pkg/quality/anonymousauth/check_test.go — NEW
cmd/loopzero-ops/main.go — register new gate
taskfiles/quality.yml — wire into task quality:gates
```

#### Deep file dependency analysis
- **anonymousauth package (NEW):** pure-logic Go gate following existing `pkg/quality/<gate>` patterns (cliexitcodes, advisorylock, etc.).
- **loopzero-ops main.go:** add subcommand `anonymous-auth-allowlist` calling the gate.
- **quality.yml:** add `/tmp/loopzero-ops quality anonymous-auth-allowlist --check` to gates.

#### Deep Dives
- **Gate logic (EC-2 fix):** walk `infra/`, `charts/`, `api/`, `cli/`, `operators/`, `pkg/`, `cmd/`, grep for `anonymous-auth=true` OR `anonymous-auth: true` (yaml form). **Allowlist (path-based):** `infra/k3d/build-cluster.yaml`. **Allowlist (pattern-based):** files matching `*_test.{sh,go,ts,yaml}` AND `*_test_data/**` (test fixtures may legitimately reference the string). **Line-level filter:** lines starting with `#`, `//`, `/*`, or `*` (markdown/yaml/Go comments) are stripped before grep — comments documenting the gate or the flag itself are not violations. Implementation: read file line-by-line, skip lines matching `^\s*(#|//|/\*|\*)` before pattern match.

#### Tasks
1. Create `pkg/quality/anonymousauth/check.go` with Walk + grep + allowlist logic (path-based + pattern-based + line-level comment skip).
2. Create unit tests covering: (a) zero matches → PASS, (b) match in k3d → PASS, (c) match outside k3d → FAIL with file:line, (d) match in `*_test.go` → PASS, (e) match in comment line → PASS, (f) match in YAML comment → PASS.
3. Register in cmd/loopzero-ops.
4. Add to task quality:gates.

#### TDD
```
RED:     TestAnonymousAuthGate_NoMatches_Passes()
RED:     TestAnonymousAuthGate_MatchInK3d_Passes()
RED:     TestAnonymousAuthGate_MatchInPulumi_Fails()
RED:     TestAnonymousAuthGate_MatchInHelm_Fails()
RED:     TestAnonymousAuthGate_YamlBooleanForm_Detected()
RED:     TestAnonymousAuthGate_TestFile_Allowlisted() — EC-2: *_test.go with literal passes
RED:     TestAnonymousAuthGate_CommentLine_Skipped() — EC-2: `# anonymous-auth=true (doc)` passes
RED:     TestAnonymousAuthGate_YamlComment_Skipped() — EC-2: yaml `# anon-auth ...` passes
GREEN:   Implement Walk + grep + allowlist + line-level filter
REFACTOR: Reuse common allowlist parser if exists in pkg/quality/
VERIFY:  cd pkg && go test ./quality/anonymousauth/ -count=1
         /tmp/loopzero-ops quality anonymous-auth-allowlist --check
```

#### Acceptance Criteria
- [ ] Gate added to task quality:gates
- [ ] All test paths GREEN
- [ ] `task quality:gates` EXIT=0 with new gate active
- [ ] cyclomatic complexity <= 10

#### DoD
- [ ] Gate committed + wired
- [ ] Tests GREEN
- [ ] No existing files trigger fail (verified)

---

## Phase 2: Data integrity (CR-6 + CR-9)

**Objective:** Eliminate the SQL omission bug class via schema-mirror test + add write-once enforcement on attestation column.

### T2.1 — Migration 000020: write-once trigger + audit_log

#### Objective
Add BEFORE UPDATE trigger preventing mutation of `image_attestation_rekor_url` once NOT NULL + audit_log row on every column write.

#### Evidence
Security review (MEET-2026-05-28-001 §CR-6): "An attacker with PG mutation access can overwrite a release row's Rekor URL to point at an attacker-controlled mirror. No `audit_log` row written on column write. No `pg_trigger_audit`."

#### Files to edit
```
api/internal/db/migrations/000020_attestation_rekor_url_write_once.up.sql — NEW
api/internal/db/release_store_test.go — test write-once enforcement
api/internal/db/audit_log_test.go — test audit_log row written
```

#### Deep file dependency analysis
- **Migration 000020 (NEW):** SQL DDL creating:
  1. `BEFORE UPDATE ON releases` trigger function `reject_attestation_url_mutation()` raising exception when OLD.image_attestation_rekor_url IS NOT NULL AND NEW.image_attestation_rekor_url IS DISTINCT FROM OLD.image_attestation_rekor_url.
  2. `AFTER INSERT ON releases` trigger function writing audit_log row with action=`release.attestation_url_set` when NEW.image_attestation_rekor_url IS NOT NULL.
- **release_store_test.go:** add tests for trigger behavior.
- **audit_log_test.go:** add test for audit row presence.

#### Deep Dives
- **Trigger function SQL:**
  ```sql
  CREATE OR REPLACE FUNCTION reject_attestation_url_mutation() RETURNS TRIGGER AS $$
  BEGIN
    IF OLD.image_attestation_rekor_url IS NOT NULL
       AND NEW.image_attestation_rekor_url IS DISTINCT FROM OLD.image_attestation_rekor_url THEN
      RAISE EXCEPTION 'image_attestation_rekor_url is write-once (release_id=%)', OLD.id;
    END IF;
    RETURN NEW;
  END;
  $$ LANGUAGE plpgsql;

  CREATE TRIGGER releases_attestation_url_immutable
    BEFORE UPDATE ON releases
    FOR EACH ROW
    EXECUTE FUNCTION reject_attestation_url_mutation();
  ```
- **Audit_log trigger (EC-3 fix — schema-correct):**
  ```sql
  -- audit_log schema (migration 000001): id SERIAL PK, tenant_id, action, actor,
  -- resource_type, resource_id, correlation_id (nullable), metadata (jsonb),
  -- created_at (default now()). DO NOT pass id (SERIAL auto) or created_at (default).
  CREATE OR REPLACE FUNCTION audit_attestation_url_set() RETURNS TRIGGER AS $$
  BEGIN
    IF NEW.image_attestation_rekor_url IS NOT NULL THEN
      INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
      VALUES (NEW.tenant_id,
              'release.attestation_url_set',
              'system:release-attestation',
              'release',
              NEW.id,
              NEW.correlation_id,  -- propagate from release if set, else NULL
              jsonb_build_object('rekor_url', NEW.image_attestation_rekor_url));
    END IF;
    RETURN NEW;
  END;
  $$ LANGUAGE plpgsql;

  CREATE TRIGGER releases_attestation_url_audit
    AFTER INSERT ON releases
    FOR EACH ROW
    EXECUTE FUNCTION audit_attestation_url_set();
  ```
- **Schema verification:** `releases.correlation_id` exists (existing column). audit_log column order matches migration 000001:244-254 verified.
- **Idempotency:** trigger functions use CREATE OR REPLACE; can re-run safely.
- **DOCUMENT (EC-15):** `gen_random_uuid()` not used here (we removed it after EC-3 fix); audit_log id is SERIAL. pgcrypto extension dependency unchanged (already used by migrations 000001/000009).
- **DOCUMENT (EC-16):** migration 000020 has NO down.sql per CLAUDE.md forward-only policy. Emergency rollback procedure (drop triggers) documented in runbook T4.5 + requires sponsor sign-off.

#### Tasks
1. Author migration 000020 SQL.
2. Add `release_store_test.go` test cases:
   - Insert with URL → audit_log row exists.
   - UPDATE same URL → no exception, no new audit_log.
   - UPDATE different URL → exception.
3. Update existing release_store.go IF any UPDATE path touches image_attestation_rekor_url (verify none).
4. Validate against PG via `task db:migrate` LOCAL.

#### TDD
```
RED:     TestReleaseStore_InsertWithRekorURL_WritesAuditLog()
RED:     TestReleaseStore_UpdateSameRekorURL_NoException()
RED:     TestReleaseStore_UpdateDifferentRekorURL_RaisesException()
RED:     TestReleaseStore_InsertNullRekorURL_NoAuditLog()
GREEN:   Apply migration 000020
REFACTOR: None
VERIFY:  cd api && go test ./internal/db/ -count=1 -tags=integration
         task db:migrate
```

#### Acceptance Criteria
- [ ] Migration 000020 applies cleanly (LOCAL + drill)
- [ ] All 4 tests GREEN
- [ ] audit_log row written on INSERT with non-null URL
- [ ] UPDATE attempt with different value raises SQL exception
- [ ] No regression on existing release_store_test.go suite

#### DoD
- [ ] Migration committed
- [ ] Tests GREEN
- [ ] `task db:migrate` GREEN
- [ ] `task quality:gates` EXIT=0

---

### T2.2 — Schema-mirror test in CI

#### Objective
Test that diffs `INFORMATION_SCHEMA.columns` against struct tags + INSERT SQL + SELECT SQL + scanReleaseFields — catches the SQL omission bug class before merge.

#### Evidence
Architect + Validation Planner reviews (MEET-2026-05-28-001 §CR-9): "The architectural fix is schema-as-code OR schema mirror tests. Add an architectural test that diffs INFORMATION_SCHEMA.columns against `releases` struct tags + INSERT/SELECT/scanReleaseFields field list. If a column exists in any of {migration, struct, INSERT, SELECT, scanner} but not all five, fail CI."

#### Files to edit
```
api/internal/db/schema_mirror_test.go — NEW
api/internal/db/schema_mirror_helpers.go — NEW (struct field introspection, SQL parsing)
```

#### Deep file dependency analysis
- **schema_mirror_test.go (NEW):** integration test (`//go:build integration`). Connects to test PG, queries INFORMATION_SCHEMA, asserts mirror invariants for: `releases`, `builds`, `release_app_attestations`.
- **schema_mirror_helpers.go (NEW):** helpers for:
  - `extractStructDbTags(reflect.Type) []string` — uses reflection on Release struct
  - `extractInsertColumns(sql string) []string` — regex parses `INSERT INTO ... (col1, col2, ...)`
  - `extractSelectColumns(sql string) []string` — regex parses `SELECT col1, col2 FROM`
  - `extractScanFields(funcDecl ast.Decl) []string` — Go AST walk on scanReleaseFields

#### Deep Dives
- **Test pseudocode (EC-4 fix — explicit table_schema='public' filter):**
  ```go
  func queryDbColumns(t *testing.T, tableName string) []string {
      // EC-4: PG INFORMATION_SCHEMA returns columns from ALL schemas
      // (information_schema, pg_catalog, public, ...). Without filter,
      // test inclui colunas system tables → false positives. Filter required.
      rows, err := testDB.Query(`
          SELECT column_name FROM information_schema.columns
          WHERE table_schema = 'public' AND table_name = $1
          ORDER BY ordinal_position`, tableName)
      ...
  }

  func TestSchemaMirror_Releases(t *testing.T) {
      dbCols := queryDbColumns(t, "releases")  // public.releases only
      structCols := extractStructDbTags(reflect.TypeOf(Release{}))
      insertCols := extractInsertColumns(insertReleaseSQL)
      selectColsList := extractSelectColumnsFromAllSelects()  // 13 SELECTs
      scanFields := extractScanFields(scanReleaseFieldsFunc)

      // Every dbCol must appear in struct, insert, ≥1 select, scan
      for _, col := range dbCols {
          assertIn(t, col, structCols, "missing from struct")
          assertIn(t, col, insertCols, "missing from INSERT")
          assertInAtLeastOneSelect(t, col, selectColsList)
          assertIn(t, col, scanFields, "missing from scanReleaseFields")
      }
  }
  ```
- **AST walk for scanFields:** Go `go/ast` + `go/parser` walks release_store.go, finds `scanReleaseFields` function, extracts arg names from `row.Scan(&r.X, &r.Y, ...)`.
- **Meta-test for future M07 tables (EC-12):** add `var m07Tables = []string{"releases", "builds", "release_app_attestations"}` as package-level var + `TestSchemaMirror_AllM07TablesListed` asserts every table in `m07Tables` has its dedicated `TestSchemaMirror_<table>` test (via reflection on package test functions OR hardcoded list match). Catches "next column-add in new M07 table".

#### Tasks
1. Author schema_mirror_helpers.go with 4 extractors + `queryDbColumns` with explicit `table_schema='public'` filter.
2. Author schema_mirror_test.go with `TestSchemaMirror_Releases` + similar for builds + release_app_attestations.
3. Add `//go:build integration` tag.
4. Wire into `task test:integration` (or equivalent).
5. Validate test catches the 3-layer bug class by adding a synthetic column to `releases` migration only (no struct/INSERT/SELECT) → test must FAIL.
6. Add meta-test `TestSchemaMirror_AllM07TablesListed` per EC-12.

#### TDD
```
RED:     TestSchemaMirror_Releases_AllColumnsMirrored()
RED:     TestSchemaMirror_Releases_DetectsMissingFromInsert() — synthetic column without INSERT → FAIL
RED:     TestSchemaMirror_Releases_DetectsMissingFromStruct()
RED:     TestSchemaMirror_Releases_DetectsMissingFromScan()
RED:     TestSchemaMirror_Builds_AllColumnsMirrored()
RED:     TestSchemaMirror_ReleaseAppAttestations_AllColumnsMirrored()
RED:     TestSchemaMirror_QueryFiltersToPublicSchema() — EC-4: assert pg_catalog tables excluded
RED:     TestSchemaMirror_AllM07TablesListed() — EC-12: meta-test asserts m07Tables var matches test funcs
GREEN:   Implement helpers + tests
REFACTOR: Extract shared assertions into helper
VERIFY:  cd api && go test ./internal/db/ -tags=integration -count=1
```

#### Acceptance Criteria
- [ ] Test runs in CI under integration tag
- [ ] Catches missing-from-INSERT / missing-from-struct / missing-from-scan
- [ ] All 3 M07 tables (releases, builds, release_app_attestations) covered
- [ ] Test time < 10s
- [ ] cyclomatic complexity <= 12 for helpers

#### DoD
- [ ] Helpers + tests committed
- [ ] CI runs test
- [ ] Synthetic regression caught
- [ ] `task quality:gates` EXIT=0

---

### T2.3 — 7 failure tests

#### Objective
Add 7 failure-path tests covering scenarios identified by Validation Planner: column-cascade, enrich-failure paths, cosign extraction, NetworkPolicy contract, Fulcio-down, Rekor-down, concurrent enrich.

#### Evidence
Change Validation Planner (MEET-2026-05-28-001 §"Failure tests required"): 7 specific test files listed with cases.

#### Files to edit
```
api/internal/db/release_store_column_cascade_test.go — NEW (test 1: column-cascade integration)
api/internal/services/build_reconcile_complete_failure_test.go — NEW (test 2: enrich failure paths)
api/internal/argo/cosign_attest_extract_test.go — NEW (test 3: cosign Rekor URL extraction edge cases)
infra/tests/helm/sigstore-extras-policy_test.sh — NEW (test 4: NetworkPolicy contract)
infra/tests/chaos/fulcio-down-during-sign_test.go — NEW (test 5: Fulcio down chaos)
infra/tests/chaos/rekor-down-during-attest_test.go — NEW (test 6: Rekor down chaos)
api/internal/services/build_reconcile_complete_concurrent_test.go — NEW (test 7: concurrent enrich race)
```

#### Deep file dependency analysis
Each test file is standalone and adds coverage to a specific failure mode. See Validation Planner output for exact test case descriptions.

#### Deep Dives
- **Test 1 (column-cascade):** integration test exercising every SELECT path on a row with non-null URL + null URL.
- **Test 2 (enrich-failure):** unit tests covering: AttestStepFailed, MalformedURL, MissingOutputs, DuplicateAttestSteps.
- **Test 3 (cosign extract):** shell-level test using bash subshell with fixture stdouts.
- **Test 4 (NP contract):** helm template + yq assertion on rendered NP target ports.
- **Test 5 + 6 (chaos) — EC-5 fix:** Ginkgo using chaos-mesh to scale Sigstore deployment to 0 mid-build. **MUST `Skip()` when chaos-mesh CRDs absent** to avoid CI false-fails in environments without chaos-mesh:
  ```go
  BeforeEach(func() {
      _, err := chaosClient.Discovery().ServerResourcesForGroupVersion("chaos-mesh.org/v1alpha1")
      if err != nil {
          Skip("chaos-mesh CRDs absent — install chaos-mesh per docs/operations/chaos-test-setup.md to enable")
      }
  })
  ```
  Same pattern as `infra/tests/chaos/chaos_test.go` existing skip-guard.
- **Test 7 (concurrent):** Go test with 2 goroutines calling enrichAppReleasesWithAttestation simultaneously. Run with `-race` flag (per CLAUDE.md tdd-workflow.md). DOCUMENT: race detector may flake on shared CI runners; if flake rate >1%, mark as `t.Parallel()=false` + add retry annotation.

#### Tasks
1-7. Implement each test file per spec.

#### TDD
Each test file has its own RED→GREEN→REFACTOR. See individual specs in Validation Planner output.

#### Acceptance Criteria
- [ ] All 7 test files GREEN in CI
- [ ] Test 1 covers all 13 SELECT sites
- [ ] Test 4 NP target ports match (5555/5554/3000/6962)
- [ ] Tests 5-6 use chaos-mesh + verify clean failure semantics
- [ ] Test 7 asserts no deadlock + single value persisted

#### DoD
- [ ] All 7 tests committed
- [ ] CI runs all 7
- [ ] `task quality:gates` EXIT=0

---

## Phase 3: NetworkPolicy hardening (CR-7 ingress)

**Objective:** Eliminate cross-tenant signing identity exposure by replacing bare namespaceSelector with podSelector + SA allowlist.

### T3.1 — NetworkPolicy podSelector + SA allowlist

#### Objective
Replace `from.namespaceSelector kubernetes.io/metadata.name=argo` with podSelector matching argo-workflow-controller managed pods AND SA name allowlist.

#### Evidence
Security review (MEET-2026-05-28-001 §FINDING-SEC-M07-005): "Required: `podSelector` matching only `argo-workflow-controller-managed` pods AND with the correct SA, not namespaceSelector alone."

#### Files to edit
```
infra/helm/sigstore-extras/templates/networkpolicy-fulcio.yaml — refactor selector
infra/helm/sigstore-extras/templates/networkpolicy-rekor.yaml — same
infra/helm/sigstore-extras/templates/networkpolicy-ctlog.yaml — same
infra/helm/sigstore-extras/values.yaml — add allowedServiceAccounts list
infra/tests/helm/sigstore-extras-podselector_test.sh — NEW (e2e contract test)
```

#### Deep file dependency analysis
- **networkpolicy-*.yaml:** refactor `from` to combine namespaceSelector + podSelector:
  ```yaml
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: argo
          podSelector:
            matchExpressions:
              - key: workflows.argoproj.io/workflow
                operator: Exists
      ports: [...]
  ```
- **values.yaml:** add `allowedServiceAccounts: ["argo-workflow"]` (currently default Argo SA name).
- **Test:** helm template + yq assert podSelector present.

#### Deep Dives
- **Why podSelector + namespaceSelector (AND semantics):** in NetworkPolicy `from` array element, namespaceSelector AND podSelector inside the SAME element = AND logic (both must match). Different array elements = OR. We want AND.

#### Tasks
1. Refactor 3 NetworkPolicy templates with combined selector.
2. Update values.yaml with allowedServiceAccounts.
3. Add helm test asserting rendered NP has podSelector + namespaceSelector combined.
4. LIVE smoke: deploy chart to k3d, verify (a) workflow pod can reach Fulcio, (b) non-workflow pod in argo namespace CANNOT reach Fulcio.

#### TDD
```
RED:     test_networkpolicy_fulcio_renders_combined_selector() — yq query
RED:     test_networkpolicy_rekor_renders_combined_selector()
RED:     test_networkpolicy_ctlog_renders_combined_selector()
RED:     test_e2e_workflow_pod_reaches_fulcio() — LIVE smoke
RED:     test_e2e_non_workflow_pod_cannot_reach_fulcio() — LIVE smoke
RED:     test_e2e_allowed_service_accounts_match_argo_default() — EC-13: kubectl get sa -n argo + assert ∈ values.allowedServiceAccounts (catches drift on Argo SA rename)
GREEN:   Refactor templates
REFACTOR: Extract common selector pattern into _helpers.tpl if duplication
VERIFY:  bash infra/tests/helm/sigstore-extras-podselector_test.sh
         kubectl --context k3d-loopzero-build run workflow-test ... (positive)
         kubectl --context k3d-loopzero-build run debug-test ... (negative)
```

#### Acceptance Criteria
- [ ] 3 NP templates refactored
- [ ] Helm template tests PASS
- [ ] LIVE positive smoke: workflow pod connects
- [ ] LIVE negative smoke: non-workflow pod gets connection refused
- [ ] Backward compat: existing Ralph iter 4 builds still sign successfully

#### DoD
- [ ] Refactor committed
- [ ] Tests GREEN
- [ ] LIVE smoke evidence captured
- [ ] `task quality:gates` EXIT=0

---

## Phase 4: Observability + diagnostics (CR-8 + CR-10)

**Objective:** Eliminate "3 AM page with no signal" failure mode via metrics + alerts + dashboard + diagnostic CLI + rollback runbook.

### T4.1 — Metrics: 8 new Sigstore counters/histograms

#### Objective
Emit 8 metrics covering sign + attest + Rekor URL persistence + Fulcio cert latency. **EC-6 fix:** removed `loopzero_sigstore_circuit_breaker_state` (no circuit breaker exists yet — Production Design Review flagged that as separate workstream). **EC-7 fix:** added `loopzero_cosign_sign_total{result}` and `loopzero_cosign_attest_total{result}` counters required as denominators in T4.2 error-rate alerts.

Final 8 metrics:
1. `loopzero_cosign_sign_duration_seconds` (histogram)
2. `loopzero_cosign_attest_duration_seconds` (histogram)
3. `loopzero_cosign_sign_total{result="success|failure"}` (counter — denominator for alerts)
4. `loopzero_cosign_attest_total{result="success|failure"}` (counter — denominator for alerts)
5. `loopzero_cosign_sign_errors_total{component="fulcio|rekor|ctlog"}` (counter)
6. `loopzero_cosign_attest_errors_total{component="fulcio|rekor|ctlog"}` (counter)
7. `loopzero_release_attestation_rekor_url_null_total{result="null|populated"}` (counter)
8. `loopzero_fulcio_cert_issuance_duration_seconds` (histogram)

#### Evidence
SRE + Production Design reviews: "Zero `loopzero_cosign_*` / `loopzero_sign_*` / `loopzero_attest_*` / `loopzero_rekor_*` / `loopzero_fulcio_*` metrics in the codebase. You cannot detect Sigstore brownouts in prod."

#### Files to edit
```
api/internal/metrics/sigstore_metrics.go — NEW (8 metric definitions)
api/internal/services/build_reconcile_complete.go — instrument enrichment + URL persist counters
api/internal/argo/cosign.go — inject duration timer wrapper (via env or sidecar)
api/internal/argo/cosign_attest.go — same
```

#### Deep file dependency analysis
- **sigstore_metrics.go (NEW):** Prometheus metric definitions following existing pattern in `api/internal/metrics/`.
- **build_reconcile_complete.go:** increment `loopzero_release_attestation_rekor_url_null_total{result="null|populated"}` AND `loopzero_cosign_sign_total{result="success|failure"}` AND `loopzero_cosign_attest_total{result="success|failure"}` based on enrichment outcome.
- **cosign.go + cosign_attest.go scripts:** wrap cosign calls with `time` command + emit metric via API callback (not bash-side Prometheus).

#### Deep Dives
- **Bash-to-metric path:** cosign scripts write duration to output param `sign_duration_ms` (new). enrichAppReleasesWithAttestation reads it. API emits histogram observation.
- **Counter labels:** `result="null|populated"` for null counter. `result="success|failure"` for sign/attest totals. `component="fulcio|rekor|ctlog"` for errors counter.
- **EC-NEW (T4.1 edge): empty `sign_duration_ms` output param.** If cosign hangs/killed (timeout), Argo output param is empty. enrichment MUST gate: `if dur, err := strconv.Atoi(out); err == nil && dur >= 0 { metric.Observe(...) }`. Empty → skip observation, increment failure counter instead.
- **Histogram buckets (DOCUMENT — EC-16 generalized):** Prometheus default histogram buckets (.005s to 10s) inappropriate for cosign sign (typical 1-30s). Override via `prometheus.LinearBuckets(1, 5, 12)` (1s, 6s, 11s, ..., 56s). Operator can tune via helm values later.

#### Tasks
1. Define 8 metrics in sigstore_metrics.go (no circuit_breaker_state; with sign_total + attest_total).
2. Instrument enrichment with null counter + sign_total/attest_total counters.
3. Add output param `sign_duration_ms` to cosign.go script.
4. Add output param `attest_duration_ms` to cosign_attest.go script.
5. Wire into enrichAppReleasesWithAttestation to emit histogram observations WITH empty-output guard.
6. Unit tests for metric emission.

#### TDD
```
RED:     TestSigstoreMetrics_NullCounter_IncrementsOnEmpty()
RED:     TestSigstoreMetrics_PopulatedCounter_IncrementsOnNonEmpty()
RED:     TestSigstoreMetrics_SignDurationHistogram_Observed()
RED:     TestSigstoreMetrics_AttestDurationHistogram_Observed()
RED:     TestSigstoreMetrics_SignTotal_IncrementsSuccess() — EC-7: denominator
RED:     TestSigstoreMetrics_SignTotal_IncrementsFailure() — EC-7: denominator
RED:     TestSigstoreMetrics_AttestTotal_IncrementsBoth()
RED:     TestSigstoreMetrics_EmptyDurationOutput_SkipsObservation_IncrementsFailure() — EC-NEW
GREEN:   Implement 8 metrics + instrumentation + empty-output guard
REFACTOR: None
VERIFY:  cd api && go test ./internal/metrics/ ./internal/services/ -count=1
         curl localhost:9090/metrics | grep loopzero_cosign
```

#### Acceptance Criteria
- [ ] 8 metrics registered (verify via `/metrics` endpoint) — NO circuit_breaker_state
- [ ] sign_total + attest_total counters present (EC-7 denominators)
- [ ] Empty duration output → skip histogram + increment failure counter (EC-NEW)
- [ ] Null counter increments on each empty URL persist
- [ ] Duration histograms observe values from cosign scripts (when non-empty)
- [ ] LIVE smoke: trigger build → metrics show non-zero values

#### DoD
- [ ] Metrics + tests committed
- [ ] LIVE evidence: metrics endpoint shows values post-build
- [ ] `task quality:gates` EXIT=0

---

### T4.2 — Prometheus alerts: 3 new alerts (EC-6 fix — removed circuit breaker)

#### Objective
Add alerts for: cosign sign P99 latency, cosign sign error rate, Rekor URL populate rate. **EC-6 fix:** removed `LoopZeroSigstoreCircuitBreakerOpen` alert because no circuit breaker exists (separate workstream). Re-add when circuit breaker lands.

#### Evidence
SRE review (MEET-2026-05-28-001): "no SLO for sign-step end-to-end, no SLO for attest-step, no error budget for `image_attestation_rekor_url` null rate."

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-sigstore.yaml — NEW
charts/loopzero-monitoring/templates/prometheusrule-sigstore.yaml — NEW (OCI parallel)
docs/runbooks/alerts/LoopZeroCosignSignSlow.md — NEW
docs/runbooks/alerts/LoopZeroCosignSignErrorRate.md — NEW
docs/runbooks/alerts/LoopZeroReleaseAttestationRekorURLNullRate.md — NEW
```

#### Deep file dependency analysis
- **prometheusrule-sigstore.yaml (NEW):** PrometheusRule CRD with 3 alerts.
- **OCI parallel:** mirror to charts/loopzero-monitoring/.
- **3 runbooks (NEW):** standard runbook format per existing alerts.

#### Deep Dives
- **Alert SLOs (proposal — can adjust post-review). EC-7 fix: denominators reference `loopzero_cosign_*_total` defined in T4.1:**
  - `LoopZeroCosignSignSlow`: `histogram_quantile(0.99, rate(loopzero_cosign_sign_duration_seconds_bucket[10m])) > 60` for 10m → severity critical
  - `LoopZeroCosignSignErrorRate`: `rate(loopzero_cosign_sign_errors_total[5m]) / rate(loopzero_cosign_sign_total[5m]) > 0.01` for 10m → critical (denominator defined in T4.1)
  - `LoopZeroReleaseAttestationRekorURLNullRate`: `rate(loopzero_release_attestation_rekor_url_null_total{result="null"}[5m]) / (rate(loopzero_release_attestation_rekor_url_null_total{result="null"}[5m]) + rate(loopzero_release_attestation_rekor_url_null_total{result="populated"}[5m])) > 0.01` for 10m → critical

#### Tasks
1. Author PrometheusRule with 3 alerts.
2. Mirror to OCI chart.
3. Author 3 runbooks.
4. Validate alert syntax with `promtool check rules`.

#### TDD
```
RED:     test_prometheusrule_yaml_valid() — promtool check
RED:     test_alerts_have_runbook_url_annotation()
RED:     test_runbook_files_exist_at_referenced_path()
GREEN:   Author rules + runbooks
REFACTOR: None
VERIFY:  promtool check rules infra/helm/monitoring/manifests/prometheusrule-sigstore.yaml
```

#### Acceptance Criteria
- [ ] 3 alerts defined (no circuit_breaker_open alert — EC-6)
- [ ] promtool check passes (denominators resolvable per EC-7 T4.1 counter definitions)
- [ ] Runbook URLs in annotations
- [ ] 3 runbooks exist at referenced paths
- [ ] OCI chart parallel updated

#### DoD
- [ ] Files committed
- [ ] promtool validation in CI
- [ ] `task quality:gates` EXIT=0

---

### T4.3 — Grafana dashboard: Sigstore pipeline

#### Objective
Single dashboard showing sign duration P50/P95/P99, attest duration P50/P95/P99, null rate, error rate by component, circuit breaker state, Rekor entries created/s.

#### Evidence
SRE review: "No Grafana dashboard. You cannot detect Sigstore brownouts in prod."

#### Files to edit
```
infra/helm/monitoring/dashboards/sigstore-pipeline.json — NEW
```

#### Deep file dependency analysis
- **dashboard JSON (NEW):** standard Grafana dashboard following existing pattern in `infra/helm/monitoring/dashboards/`.

#### Deep Dives
- **Dashboard panels (proposal):**
  1. Sign duration heatmap
  2. Attest duration heatmap
  3. Null rate timeseries
  4. Error rate by component stacked area
  5. Circuit breaker state (single stat)
  6. Rekor entries created/s

#### Tasks
1. Author dashboard JSON.
2. Validate JSON via Grafana API.
3. Add screenshot to docs/operations/sigstore-storage-model.md.

#### TDD
```
RED:     test_dashboard_json_valid()
RED:     test_dashboard_references_existing_metric_names() — grep loopzero_cosign in dashboard JSON against sigstore_metrics.go
GREEN:   Author dashboard
REFACTOR: None
VERIFY:  jq . infra/helm/monitoring/dashboards/sigstore-pipeline.json
```

#### Acceptance Criteria
- [ ] Dashboard JSON valid
- [ ] References metrics that exist in code
- [ ] 6 panels minimum

#### DoD
- [ ] Dashboard committed
- [ ] Loaded in Grafana LIVE
- [ ] Screenshot in docs

---

### T4.4 — `loopzero admin release inspect <id>` CLI + API endpoint

#### Objective
Diagnostic CLI surface for on-call without source access.

#### Evidence
SRE review (MEET-2026-05-28-001): "There is no loopzero-ops CLI command like `loopzero admin release inspect <release-id> --debug-attestation` showing which write path was taken."

#### Files to edit
```
api/internal/routes/admin_release_inspect.go — NEW (GET /api/v1/admin/releases/{id}/inspect)
api/internal/services/release_inspector.go — NEW (orchestration)
cli/src/commands/admin/release.ts — NEW (CLI command)
cli/tests/unit/admin-release.test.ts — NEW
```

#### Deep file dependency analysis
- **admin_release_inspect.go (NEW):** route handler following existing admin pattern. Auth via X-Service-Auth (admin token).
- **release_inspector.go (NEW):** queries release_store + audit_log + builds workflow snapshot.
- **CLI command (NEW):** Commander.js subcommand `admin release inspect <id>`.

#### Deep Dives
- **Response schema:**
  ```json
  {
    "release_id": "rel-...",
    "build_id": "build-...",
    "workflow_id": "loopzero-build-...",
    "image_digest": "sha256:...",
    "image_signature": "registry/...@sha256:...",
    "image_signed_at": "2026-05-28T...",
    "image_attestation_rekor_url": "http://rekor.../api/v1/log/entries?logIndex=N",
    "signing_path": "keyless",
    "attestation_path": "present",
    "audit_log_entries": [...]
  }
  ```
- **CLI exit codes:** 0 success, 4 auth (no admin token), 1 release not found.

#### Tasks
1. Implement GET /api/v1/admin/releases/{id}/inspect handler.
2. Implement ReleaseInspector service.
3. Add CLI command + tests.
4. Add Rule 3 check: no internal terms in CLI output.

#### TDD
```
RED:     TestAdminReleaseInspect_Returns_AttestationChain()
RED:     TestAdminReleaseInspect_404_ForUnknownRelease()
RED:     TestAdminReleaseInspect_403_WithoutAdminAuth()
RED:     TestAdminReleaseInspect_GracefulDegradeWhenWorkflowGCd() — EC-14: workflow CR deleted by reaper (>90d) → return 200 with workflow_id=null + signing_path="unknown", NOT 500
RED:     TestCli_AdminReleaseInspect_JsonShape() — Jest
RED:     TestCli_AdminReleaseInspect_NoInternalTerms() — Jest grep
GREEN:   Implement handler + service + CLI
REFACTOR: None
VERIFY:  cd api && go test ./internal/routes/ ./internal/services/ -count=1
         cd cli && npm test
```

#### Acceptance Criteria
- [ ] Endpoint returns full attestation chain
- [ ] CLI exit codes correct
- [ ] No internal terms in CLI output
- [ ] cyclomatic complexity <= 10
- [ ] Coverage >= 90% on new files

#### DoD
- [ ] All files committed
- [ ] Tests GREEN
- [ ] LIVE smoke: `loopzero admin release inspect rel-506b861c71594269 --json` returns populated chain
- [ ] `task quality:gates` EXIT=0

---

### T4.5 — Rollback runbook + LOCAL dry-run

#### Objective
Author canonical rollback runbook covering all M07 cutover failure modes + dry-run on LOCAL k3d cleanroom. Operator drill rehearsal = separate workstream.

#### Evidence
SRE + Validation Planner + Deploy Guardian reviews: "Documented + tested rollback for M07 cutover is missing."

#### Files to edit
```
docs/operations/rollback-m07-cutover-runbook.md — NEW
infra/scripts/production/m07-cutover-rollback.sh — NEW (orchestration)
infra/tests/check-m07-rollback-dry-run_test.sh — NEW (hermetic)
.claude/knowledge-base/audits/2026-05-28-m07-rollback-local-dryrun-evidence.md — NEW
```

#### Deep file dependency analysis
- **runbook (NEW):** operator-facing markdown with numbered steps + commands + abort triggers.
- **rollback orchestration script (NEW):** wraps argocd app rollback + helm rollback + (last-resort) migration rollback.
- **hermetic test (NEW):** validates script syntax + happy path on mocks.
- **dry-run evidence (NEW):** captures LOCAL cleanroom dry-run output.

#### Deep Dives
- **Rollback flow — EC-8 fix: explicit `argocd app history` discovery step BEFORE every rollback:**
  ```
  Step 0a (PRE-CONDITION — EC-17): Drain in-flight workflows first
    $ kubectl --context k3d-loopzero-build get workflows -n argo --no-headers \
        | awk '$2 == "Running" {print $1}' \
        | xargs -r -n1 argo --kubeconfig=$KUBECONFIG terminate -n argo
    (rationale: helm uninstall mid-build leaks Rekor entries — log is append-only)

  Step 0b (DISCOVERY — EC-8): Find target rollback revision
    $ argocd app history loopzero-api
    (output: list of past revisions with timestamps + commit SHA)
    Pick LAST GREEN revision (i.e., before the cutover commit). Note REV.

  Step 1: loopzero-api image rollback
    $ argocd app rollback loopzero-api $REV
    (wait Healthy/Synced)

  Step 2: Workflow template rollback (same discovery)
    $ argocd app history loopzero-build-cwts
    $ argocd app rollback loopzero-build-cwts $REV

  Step 3: Sigstore stack removal + public fallback
    $ helm uninstall sigstore-extras -n fulcio-system
    $ # Update Pulumi stack config: CosignFulcioURL=https://fulcio.sigstore.dev
    $ # Update Pulumi stack config: CosignRekorURL=https://rekor.sigstore.dev
    $ pulumi up

  Step 4 (LAST RESORT — sponsor sign-off required): Migration rollback
    $ # ONLY if data corruption observed; nullable columns mean default = no rollback needed
    $ psql -c 'DROP TRIGGER releases_attestation_url_immutable ON releases;'
    $ psql -c 'DROP TRIGGER releases_attestation_url_audit ON releases;'
    $ psql -c 'DROP FUNCTION reject_attestation_url_mutation();'
    $ psql -c 'DROP FUNCTION audit_attestation_url_set();'
    (forward-only policy violation — sponsor approval mandatory)
  ```
- **Time budget:** Steps 0a+0b+1+2 = ≤15 min (target SRE veto release condition). Steps 3+4 = longer, separate workstream.

#### Tasks
1. Author runbook with 4 scenarios + abort triggers + time budgets.
2. Author orchestration script.
3. Author hermetic test.
4. Dry-run on LOCAL k3d cleanroom — capture output.
5. Add allowlist entry for new .sh file.

#### TDD
```
RED:     test_rollback_script_syntax_valid()
RED:     test_rollback_script_dry_run_no_mutations()
RED:     test_rollback_script_argocd_rollback_called_in_order()
RED:     test_rollback_script_emits_audit_marker()
GREEN:   Author script + hermetic test
REFACTOR: None
VERIFY:  bash infra/tests/check-m07-rollback-dry-run_test.sh
         bash infra/scripts/production/m07-cutover-rollback.sh --dry-run --target-rev=N-1
```

#### Acceptance Criteria
- [ ] Runbook covers 4 scenarios
- [ ] Script + hermetic test pass
- [ ] LOCAL dry-run captured with timestamp
- [ ] Allowlist updated (script-count 133 → 134)

#### DoD
- [ ] Files committed
- [ ] LOCAL dry-run evidence in audits/
- [ ] `task quality:gates` EXIT=0

---

### T4.6 — Rekor retention: complete Trillian admin API integration

#### Objective
Complete `infra/scripts/security/rekor-archive-old-entries.sh` Trillian admin API integration OR document explicit storage budget + manual procedure in runbook.

#### Evidence
Production Design review (MEET-2026-05-28-001): "Rekor retention script (`rekor-archive-old-entries.sh`) is a STUB with operator-completion TODO for Trillian admin API."

#### Files to edit
```
infra/scripts/security/rekor-archive-old-entries.sh — extend with Trillian API calls OR add explicit manual-procedure flag
docs/operations/rekor-retention.md — NEW (manual procedure if API incomplete)
infra/tests/check-rekor-archive_test.sh — NEW (hermetic)
```

#### Deep file dependency analysis
- **rekor-archive-old-entries.sh:** currently stub. Decision: Trillian admin API integration is operator HARD STOP (Trillian deployment + admin token). For Plan A engineering scope, document the manual procedure + add `--manual-procedure-only` flag.
- **runbook (NEW):** explicit storage budget (e.g., 140GB PV, 100k entries/day = ~6 months until manual archival needed) + manual archival commands.

#### Deep Dives
- **Decision for Plan A:** add `--manual-procedure-only` flag that exits 0 with WARN. Operator workstream completes Trillian integration separately.

#### Tasks
1. Add `--manual-procedure-only` flag + log WARN.
2. Author rekor-retention.md runbook.
3. Add hermetic test for flag behavior.

#### TDD
```
RED:     test_rekor_archive_manual_procedure_flag_exits_0_with_warn()
RED:     test_rekor_archive_no_flag_exits_1_with_unimplemented_message()
GREEN:   Add flag handling
REFACTOR: None
VERIFY:  bash infra/scripts/security/rekor-archive-old-entries.sh --manual-procedure-only
```

#### Acceptance Criteria
- [ ] Flag handled
- [ ] Runbook documents storage budget + manual procedure
- [ ] Hermetic test PASS

#### DoD
- [ ] Script + runbook committed
- [ ] `task quality:gates` EXIT=0

---

## Coverage Matrix

| # | Gap / Requirement (MEET-2026-05-28-001 action) | Task(s) | Resolution |
|---|---|---|---|
| 1 | Add Config.Validate() guard: reject CosignKeyless+InsecureSCT in prod | T1.2 | New CosignInsecureSkipSCT + TUFRootPinned env fields + Validate() rule |
| 2 | Remove unconditional --insecure-skip-verify; gate via env | T1.2 | Conditional rendering in cosign.go + cosign_attest.go |
| 5 | Migration 000020 write-once trigger + audit_log | T2.1 | BEFORE UPDATE trigger + AFTER INSERT audit row |
| 7 | NetworkPolicy hardening podSelector + SA allowlist | T3.1 | Refactor 3 NP templates with combined selector |
| 8 | Remove false-parity comment + decide prod path | T1.4 | Truthful comment + ADR documenting authenticated discovery path |
| 9 | Quality gate: anonymous-auth=true outside k3d allowlist | T1.5 | pkg/quality/anonymousauth gate |
| 12 | Schema mirror test | T2.2 | api/internal/db/schema_mirror_test.go integration test |
| 13 | 7 failure tests | T2.3 | 7 new test files |
| 14 | Rollback runbook + dry-run (engineering side) | T4.5 | Runbook + orchestration script + LOCAL dry-run evidence |
| 15 | Step-name helpers SoT | T1.1 | pkg/naming/argo_steps.go + 13 call sites refactored |
| 16 | Fix workflow_builder.go:228-235 legacy branch | T1.3 | Delete dead path (preferred) |
| 17 | Metrics + alerts | T4.1, T4.2 | 8 metrics + 4 alerts |
| 18 | Grafana dashboard | T4.3 | sigstore-pipeline.json |
| 19 | Rekor retention completion | T4.6 | Manual procedure flag + runbook (Trillian API = operator workstream) |
| 20 | loopzero admin release inspect CLI | T4.4 | New endpoint + CLI command |
| 21 | ADR-A LOCAL exception | T0.1 | New ADR + bootstrap script context guard |
| 22 | ADR-B cosign v2.4.3 deadline | T0.2 | New ADR + deadline ratchet extension |
| 23 | Cutover ordering codified | T0.3 | docs/operations/m07-cutover-sequence.md + preflight script |

**Coverage: 18/18 engineering actions covered (100%)**

Note: actions 3 (TUF ceremony), 4 (Pulumi up sigstore.ts), 6 (per-tenant SA isolation), 10 (drill iter ≥56), 11 (customer trial), 24 (customer disclosure) are OPERATOR HARD STOPs or T3 (new meeting) excluded from Plan A scope.

### Edge Case Coverage (v1.1 amendments)

| EC | Task | Resolution |
|----|------|------------|
| EC-1 | T0.2 | ConfigMap `set_at` field REQUIRED when `target_version` set; script FAILs if absent |
| EC-2 | T1.5 | Allowlist `*_test.{sh,go,ts,yaml}` + skip lines starting with `#`/`//`/`/*`/`*` |
| EC-3 | T2.1 | Trigger SQL uses correct audit_log schema: no `id`/`gen_random_uuid()`, includes `correlation_id` |
| EC-4 | T2.2 | PG query filters `table_schema='public'` to exclude system tables |
| EC-5 | T2.3 | Chaos tests `Skip()` when chaos-mesh CRDs absent (BeforeEach guard) |
| EC-6 | T4.1, T4.2 | Removed `loopzero_sigstore_circuit_breaker_state` metric + alert (no breaker exists; separate workstream) |
| EC-7 | T4.1 | Added `loopzero_cosign_sign_total` + `loopzero_cosign_attest_total` counters (denominators for T4.2 alerts) |
| EC-8 | T4.5 | Runbook includes `argocd app history` discovery step BEFORE every rollback command |
| EC-9 | T0.1 | Bootstrap context guard via regex allowlist: `^k3d-loopzero-(build|drill-build)$` (supports drill) |
| EC-10 | T1.1 | Empty `app.Name` → `panic()` with descriptive message (explicit decision) |
| EC-11 | T1.2 | Insecure flag omitted when Keyless=false (keyed mode), regardless of env |
| EC-12 | T2.2 | Meta-test `TestSchemaMirror_AllM07TablesListed` asserts all M07 tables covered |
| EC-13 | T3.1 | e2e test asserts SA names in deployed Argo ∈ values.allowedServiceAccounts |
| EC-14 | T4.4 | Graceful degrade when workflow GC'd (>90d) — return 200 with null fields, NOT 500 |
| EC-15 | T2.1 | DOCUMENT: pgcrypto already enabled (migrations 000001/000009) — no new dependency |
| EC-16 | T2.1, T4.1 | DOCUMENT: migration forward-only (no down.sql); histogram buckets operator-tunable |
| EC-17 | T4.5 | Rollback pre-condition Step 0a: drain in-flight workflows BEFORE helm uninstall |
| EC-NEW | T4.1 | Empty `sign_duration_ms` output param → skip histogram + increment failure counter |

## Global Definition of Done

- [ ] All 5 phases completed
- [ ] All tests passing (Go + TypeScript + bash hermetic)
- [ ] Zero golangci-lint warnings
- [ ] cd api && go test ./... GREEN
- [ ] cd cli && npm test GREEN
- [ ] task quality:gates EXIT=0
- [ ] task validate:manifests GREEN
- [ ] Backward compatibility preserved (existing LOCAL k3d still builds successfully)
- [ ] code-audit checks passing across all modified files
- [ ] Plan-specific criteria:
  - [ ] `--insecure-skip-verify` cannot execute in IsProduction() without TUF
  - [ ] image_attestation_rekor_url write-once enforced at DB layer
  - [ ] Schema-mirror test catches synthetic regression
  - [ ] 7 failure tests cover documented scenarios
  - [ ] 8 metrics + 4 alerts deployed
  - [ ] Grafana dashboard loaded LIVE
  - [ ] loopzero admin release inspect returns populated chain
  - [ ] Rollback runbook + LOCAL dry-run evidence captured
  - [ ] CI gate blocks anonymous-auth=true outside k3d
- [ ] **Dogfood QA PASS** — `/dogfood full` health score >= 70, zero CRITICAL issues introduced by this plan's changes
- [ ] **Runtime-metric proof** — for T4.1 metrics: LIVE smoke build observes non-zero values on metrics endpoint within 5 min

## Final Phase: Dogfood QA (MANDATORY)

> This phase runs AFTER all implementation phases are complete. The plan is NOT done until dogfood passes.

**Objective:** Validate that the implemented changes work as a real user would experience them, not just as unit tests assert.

### Execution

Run `/dogfood full`. Always full. No shortcuts.

### Acceptance Criteria

- [ ] Health score >= 70/100
- [ ] Zero CRITICAL issues introduced by this plan's changes
- [ ] Zero HIGH issues in commands/features modified by this plan (admin release inspect, build pipeline, Sigstore stack)
- [ ] Any pre-existing issues documented (clone-runner concurrency, chaos C01) not blocking
- [ ] LIVE smoke: build deploy on LOCAL k3d → sign + attest + DB columns populated + audit_log row written + metrics emitted + loopzero admin release inspect returns expected JSON

### If Dogfood Fails

1. Identify which issues are caused by this plan's changes vs pre-existing.
2. Fix all plan-caused CRITICAL and HIGH issues before declaring the plan complete.
3. Re-run `/dogfood full` to confirm fixes.
4. Pre-existing issues are logged but do NOT block plan completion.
