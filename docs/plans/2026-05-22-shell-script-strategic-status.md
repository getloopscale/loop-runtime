# Implementation Status — 2026-05-22 Shell Script Strategic Plan

> **Live progress tracker for Ralph loop iterations.** Updated at end of each
> iteration. Next iter reads this to pick up where previous left off.

**Plan version:** v1.3 (`docs/plans/2026-05-22-shell-script-strategic-plan.md`)
**Branch:** `refactor/shell-scripts-strategic`
**Promise:** "TODAS AS TASKS, CRITERIOS DE ACEITES, DODs CONCLUIDOS E VALIDADOS CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS"
**Promise eligibility:** ❌ FALSE — Phases 1-7 + 3 LIVE gates pending

---

## Iteration log

### Iter 12 (2026-05-22) — Phase 5 T5.3 (test-deletion pure-Go gate)

**Commit:** (current)

**Done:**
- `pkg/quality/testdeletion/` (NEW) — pure-Go gate replacing
  `infra/scripts/quality/check-test-deletion.sh`. Shells `git diff` (ALLOWED
  CLI per D5) + DiffOverride seam for hermetic tests. 9 tests GREEN.
- `cmd/loopzero-ops/quality_subcmd.go` — wires `test-deletion` subcommand (reads
  BASE_REF/HEAD_REF/PR_BODY/PR_LABELS env).
- `.github/workflows/quality-gates.yml` — test-deletion job runs Go gate.
- `infra/scripts/quality/check-test-deletion.sh` — DELETED.
- allowlist 253 (was 254).

**T5.1 deferred:** Pulumi Automation API migration (pulumi-archive.sh,
pulumi-orphan-rm.sh) requires heavy `github.com/pulumi/pulumi/sdk/v3` dep +
LIVE Pulumi stack validation (operator HARD STOP per plan T5.1 acceptance).

**Script count: 254 → 253** (4 deletions cumulative).

---

### Iter 11 (2026-05-22) — Phase 1 T1.1 dead-set validation (0 deletable; prevented destructive delete)

**Commit:** (current)

**Done:**
- `docs/scripts-audit/2026-05-22-dead-set.md` (NEW) — T1.1 validation output.
- `docs/scripts-audit/REVIEWED.md` — appended iter-11 finding.

**Finding: validated dead-set = 0 scripts.** The 73 raw zero-caller scripts
collapse to 0 safely-deletable:
- **13 `cell2-gate-c*.sh`** dynamically sourced via `$LIB_DIR/cell2-gate-${g}.sh`
  (T1.1.a EC-03 catch — would have been a destructive false-positive delete).
- **2 hermetic tests** (`dry-run-lib_test.sh`, `values_gate_test.sh`) test LIVE
  code → WIRE-CANDIDATE not delete (deleting destroys coverage).
- **1** (`sanitize.sh`) pairs with render-drill.sh snapshot harness → REVIEW-NEEDED.

**Honest outcome:** NO deletion this iter. The validation PREVENTED a 15-script
destructive delete (13 live dynamically-sourced + 2 valid tests). Per
"NUNCA MINTA" + anti-pattern "Deletion sem validation" — did NOT fabricate
deletions to inflate the count. Big reductions deferred to Phase 4 (Ginkgo) +
Phase 5 (pure-logic) + operator-reviewed dead-set.

**Phase 1: T1.1 done (engineering validation); T1.2 = NO-OP (empty dead-set).**

---

### Iter 10 (2026-05-22) — Phase 5 T5.3 (divergence-matrix pure-Go gate)

**Commit:** (current)

**Done:**
- `pkg/quality/divergencematrix/` (NEW) — pure-Go gate replacing
  `scripts/check-divergence-matrix.sh`. Walks api/operators/cli/pkg/infra/scripts
  for `LOCAL ONLY (LP-NNN)` markers, cross-references matrix entries. Excludes
  test files (fixtures contain example markers). 8 tests GREEN.
- `cmd/loopzero-ops/quality_subcmd.go` — wires `divergence-matrix` subcommand.
- `Taskfile.yml audit:divergence-matrix` — calls Go gate via `go run`.
- `.github/workflows/infra-gates.yml` — divergence-matrix job runs Go gate +
  path filters repointed to `pkg/quality/divergencematrix/**`.
- `scripts/check-divergence-matrix.sh` — DELETED.
- `.claude/rules/shell-scripts-allowlist.md` — entry removed (254, was 255).

**Design refinement:** gate excludes `_test.go`/`_test.sh` from marker scan
(test fixtures legitimately contain example LP-NNN that aren't production
LOCAL-ONLY paths). Live check PASS (0 violations against real repo).

**Script count: 255 → 254.**

---

### Iter 9 (2026-05-22) — Phase 5 T5.3 partial (taskfile-interface gate + LATENT BUG fixed)

**Commit:** (current)

**Done:**
- `pkg/quality/taskfileinterface/` (NEW) — pure-Go gate replacing
  `scripts/check-taskfile-interface.sh`. Parses Taskfile.yml + flattened
  includes DIRECTLY via gopkg.in/yaml.v3 (NO `task` binary dependency — the
  bash version shelled `task --list-all`). 8 tests GREEN.
- `cmd/loopzero-ops/quality_subcmd.go` — wires `taskfile-interface` subcommand.
- `scripts/check-taskfile-interface.sh` — DELETED.
- `.claude/rules/shell-scripts-allowlist.md` — entry removed (255, was 256).

**LATENT BUG SURFACED + FIXED (per error-documentation rule):**
The Go parser caught a duplicate `test:coverage` key in taskfiles/test.yml
(line 131 HTML variant SHADOWED by line 1061 merged-lcov variant). Task v3
silently used the last → line-131 task was dead code. The bash gate
(`task --list-all` output parsing) never noticed. Fixed: renamed line-131 to
`test:coverage:html`. Gate now surfaces parse errors loud (not silent-skip).
Finding doc: `concepts/operations/finding-2026-05-22-taskfile-duplicate-key.md`.

**Script count: 256 → 255.**
**Phase 5: 2/3** (T5.2 + T5.3-first-migration done; T5.1 Pulumi + more T5.3
pure-logic migrations pending).

---

### Iter 8 (2026-05-22) — Phase 5 T5.2 COMPLETE (generators → //go:generate)

**Commit:** (current)

**Done:**
- `pkg/statemachine/cmd/gen-dots/main.go` (NEW) — idiomatic Go generator
  replacing `infra/scripts/gen-state-machine-dots.sh`. `--write`/`--check`/`--out`
  flags. Deterministic output (Visualize sorts states+transitions).
- `pkg/statemachine/visualize.go` — added `//go:generate go run ./cmd/gen-dots
  --write --out ../../docs/state-machines` directive.
- `pkg/statemachine/cmd/gen-dots/gen_dots_test.go` — 6 tests GREEN:
  ProducesAllDotFiles, DeterministicOutput, CheckDrift (missing/modified/current),
  CommittedDotsAreCurrent (validates REAL committed DOTs match Visualize).
- `taskfiles/test.yml` — pkg:state-machine-dots:check/write now invoke
  `go run ./statemachine/cmd/gen-dots` (was `bash gen-state-machine-dots.sh`).
- `infra/scripts/gen-state-machine-dots.sh` — DELETED (git rm).
- `.claude/rules/shell-scripts-allowlist.md` — entry removed (256, was 257).

**Validation:** `go generate ./statemachine/...` produces byte-identical DOTs
to committed (zero git diff). no-shell-wrap gate PASS (gen-dots uses Visualize()
not shellrun).

**Script count: 257 → 256** (first deletion of the reduction journey).

---

### Iter 7 (2026-05-22) — Phase 3 COMPLETE (T3.1+T3.2 — canonicalize Tilt)

**Commit:** (current)

**Done:**
- `Taskfile.yml` (T3.2): setup target prepends `[NOTICE]` echoes pointing
  to `docs/operations/dev-loop.md` + clarifies `tilt up` is the canonical
  dev loop. Setup desc rewritten to describe one-shot vs day-to-day distinction.
- `docs/operations/dev-loop.md` (T3.1 NEW): canonical operator guide covering
  cold start sequence, dev loop semantics, common operations (mid-day changes,
  helm chart change, CRD/migration changes), when NOT to use task setup,
  troubleshooting table cross-referencing T2.3/T2.2/T2.3.b fixes.
- `README.md` (T3.1): Setup section now explicitly distinguishes ONE-SHOT vs
  DEV LOOP; links to docs/operations/dev-loop.md.
- `CLAUDE.md` (T3.1): development commands section reflects same distinction;
  task dev:up marked as legacy alias.
- `pkg/quality/phase3gate/` — 4 hermetic Go tests GREEN validating Phase 3
  doc/Taskfile compliance.

**Phase 3 COMPLETE** (no T2.4-style HARD STOP — Phase 3 is doc/notice only,
not requiring LIVE validation).

---

### Iter 6 (2026-05-22) — Phase 2 ENGINEERING COMPLETE (T2.1+T2.2+T2.3, critical path)

**Commit:** (current)

**Done (engineering side — T2.4 LIVE remains HARD STOP):**
- `docs/scripts-audit/2026-05-22-helmfile-mapping.md` (T2.1) — full bare-helm
  → helmfile release mapping; identified gaps (network-policies chart not in
  helmfile, kratos/hydra namespace migration loopzero-system → loopzero-auth);
  documents EC-01 + EC-02 compliance plan
- `infra/helm/ory-kratos/values.yaml.gotmpl` (T2.2 EC-02) — DSN line uses
  single-quoted YAML, unescaped `"` inside gotmpl literal (was `\"` escape
  that crashed helmfile render)
- `infra/helm/ory-hydra/values.yaml.gotmpl` (T2.2 EC-02) — same fix
- `taskfiles/infra.yml` (T2.3) — install:mgmt/build/workload + cloud:setup
  REWRITTEN to use `helmfile sync` master pattern (EC-01) + `kubectl
  -o go-template`/`openssl base64 -d -A` (EC-02). Zero `helm upgrade --install`
  + zero `base64 -d` in live code (verified via hermetic test)
- `pkg/quality/phase2gate/` — 8 hermetic Go tests GREEN validating:
  * Zero bare `helm upgrade --install` in Taskfile (non-comment)
  * Zero bare `base64 -d` in Taskfile (non-comment, excludes openssl/`-A`)
  * Helmfile sync present for mgmt+build+runtime
  * Portable base64 form present
  * kratos+hydra YAML escape fixed
  * Mapping doc T2.1 exists with EC-01/EC-02 references
  * install:mgmt block uses MASTER sync (no --selector in code, only docs)

**Cleanroom unblock rationale:**
Bug A (gitea PASSWD empty) — was caused by helm install with chart's default
values; now resolved because helmfile chart values reference
`loopzero-db-credentials` Secret via generate-secrets.sh pre-sync.
Bug B (kratos/hydra YAML escape) — fixed in T2.2 directly.

**T2.4 HARD STOP:** Operator must run `task clean:all && task setup && task doctor`
on fresh k3d to confirm `task setup` exit 0 + PASS=N FAIL=0. Plan requires
LIVE validation that Ralph CANNOT perform.

---

### Iter 5 (2026-05-22) — Phase 6 T6.3 COMPLETE (no-shell-wrap gate ATIVO)

**Commit:** (current)

**Done:**
- `pkg/quality/noshellwrap/check.go` (~190 LOC) — AST walker via `go/parser` +
  `go/ast` stdlib (no external deps). Detects `<x>.Run("<forbidden-cli>", ...)`
  calls where first arg is string literal in ForbiddenCLI set.
- `pkg/quality/noshellwrap/check_test.go` — 14 testify tests GREEN, coverage 90.5%
  (atinge target ≥90%). Cobre: forbidden detection, allowed CLI pass, nolint
  suppression (same/preceding line), test file exemption, vendor/ exclusion,
  multiple violations, dynamic CLI documented limitation, unparseable file
  graceful skip, non-existent root error
- `cmd/loopzero-ops/quality_subcmd.go` — wires `no-shell-wrap` subcommand
- `.github/workflows/no-shell-wrap-gate.yml` — PR-blocking gate scanning
  pkg/ + cmd/ separately

**Forbidden CLIs (per plan D5 deep-dive):**
- pulumi → use pulumi-go-sdk Automation API
- helm → use helm.sh/helm/v3 SDK
- kubectl → use k8s.io/client-go
- aws → use aws-sdk-go-v2
- gh → use github.com/google/go-github

**Smoke test:** Gate ran against `pkg/` (current branch state) → PASS with zero
violations (expected; branch is pre-migration). Once Phase 5 (SDK-native Go) lands,
gate validates that migrations didn't shell-wrap forbidden CLIs.

**Phase 6 status: 3/4 done.** T6.1 + T6.2 + T6.3 ATIVOS. T6.4 (final inventory
validation) pending — requires Phases 1-5 complete to validate inventory ≤ 30.

---

### Iter 4 (2026-05-22) — Phase 7 T7.2 COMPLETE (OpenCost adopted — Phase 7 DONE)

**Commit:** (current)

**Done:**
- `infra/helm/opencost/Chart.yaml` — wrapper chart pointing to upstream
  `opencost-helm-chart` v1.45.0
- `infra/helm/opencost/values.yaml` — defaults: external Prometheus integration
  (LoopZero's existing one), DigitalOcean customPricing, non-root + readOnlyRootFilesystem
- `infra/helm/opencost/values.schema.json` — schema gate per T3.1 pattern
- `infra/helmfile/mgmt.helmfile.yaml.gotmpl` — added opencost-helm-chart repo +
  release with `needs: [monitoring/prometheus]`
- `infra/helmfile/values/{dev,drill,prod,standalone,standalone-multi}/opencost.yaml.gotmpl`
  — 5 env-stub override files (consistent with prometheus/grafana pattern)
- `infra/helm/monitoring/manifests/servicemonitor-opencost.yaml` — Prometheus
  scrape config for :9003/metrics, interval 60s
- `pkg/quality/opencostgate/` — 9 hermetic Go tests GREEN validating YAML
  structure (per Anti-Pattern #8: NO production Go code)
- `docs/operations/cost-attribution.md` — operator runbook with DO pricing
  values, Prometheus queries, validation sequence
- CLAUDE.md updated: OpenCost moved from "P0 gap" / "v1.x deferred" → "ADOPTED 2026-05-22"

**Phase 7 SUMMARY (T7.1 + T7.2 both done):**
- Pluto (iter 3) — deprecated K8s API detector, CI gate active
- OpenCost (iter 4) — cost attribution per tenant, Helm chart in mgmt
- Both Anti-Pattern #8 materialized: external tool ADOPTED, zero Go gate reimplementation
- Both LIVE validation pending operator action (cleanroom or drill cluster)

---

### Iter 3 (2026-05-22) — Phase 7 T7.1 COMPLETE (Pluto adopted)

**Commit:** (current)

**Done:**
- `.github/workflows/pluto-deprecated-apis.yml` — Fairwinds Pluto Action SHA-pinned;
  scans all Helm chart renders (`infra/helm/` + `charts/`) on PR + weekly cron + workflow_dispatch
- `infra/tests/pluto/fixtures/deprecated.yaml` — fixture with `policy/v1beta1 PodDisruptionBudget`
  (removed in v1.25); manual invoke validates gate sensitivity
- `infra/tests/pluto/fixtures/current.yaml` — fixture with current stable APIs
- `pkg/quality/plutogate/` — hermetic Go tests (7/7 GREEN) validating workflow YAML
  structure (SHA-pin, target-versions set, only-show-removed: false)
- `docs/operations/k8s-upgrade-runbook.md` — operator runbook for DOKS minor upgrade
  with Pluto check sequence + rollback strategy
- CLAUDE.md updated: Pluto moved from "P0 gap" → "ADOPTED 2026-05-22 via plan T7.1"

**Anti-Pattern #8 materialized:** NO Go gate reimplementation. External Pluto Action
adopted as-is, validated only via YAML-structure hermetic tests.

**Why this matters:** Destrava próximo DOKS minor upgrade (v1.32 → v1.33). Sem
Pluto, manifest com API removida no target version causaria failure post-upgrade
silenciosamente. Workflow scans `infra/helm/` + `charts/` rendered output before
PR merge.

---

### Iter 2 (2026-05-22) — Phase 6 T6.1 + T6.2 COMPLETE (regression lock active)

**Commits:** (current) — `feat(nonewbash): Phase 6 T6.1+T6.2 — D3 gate + allowlist baseline`

**Done:**
- `pkg/quality/nonewbash/` (210 LOC) — Check + parseAllowlist + verifyRefReachable + gitDiffAdded
- `pkg/quality/nonewbash/check_test.go` — 12 testify tests GREEN, coverage 88.2%
- EC-04 implementation: `BASE_REF_UNREACHABLE` typed error with `fetch-depth` hint
- EC-06 implementation: rename (R status) treated as new via `--diff-filter=AR`
- `.claude/rules/shell-scripts-allowlist.md` — baseline with 257 grandfathered entries
- `cmd/loopzero-ops/quality_subcmd.go` — wires `no-new-bash` subcommand
- `.github/workflows/no-new-bash-gate.yml` — CI gate with mandatory `fetch-depth: 0`
- Smoke test PASS contra branch (HEAD vs HEAD = empty diff = PASS expected)

**Why this matters:** D3 gate ATIVO antes de Phases 1-5 reduzem inventário. Sem isso,
qualquer sessão futura pode adicionar `.sh` novos e o trabalho de redução evapora.
Regression locked NOW.

---

### Iter 1 (2026-05-22) — Phase 0 T0.1 COMPLETE (engineering side)

**Commit:** `bd5ffa1e` — `feat(scriptsaudit): Phase 0 T0.1 — scripts audit Go tool + baseline CSV`

**Done:**
- `pkg/quality/scriptsaudit/` package (440 LOC) — ScanRepo + Disposition heuristics + CSV emitter
- `pkg/quality/scriptsaudit/check_test.go` — 21 tests GREEN, coverage 91.8%
- `cmd/loopzero-ops/` minimal — `quality scripts-audit` subcommand wired
- `docs/scripts-audit/2026-05-22.csv` — baseline 257 scripts / 42,793 LOC
- `docs/scripts-audit/REVIEWED.md` — operator review template + disposition distribution
- `.gitignore` exception for `/cmd/` (prior catastrophic block — same lesson as `/scripts` 2026-05-20)
- `go.work` adds `./cmd/loopzero-ops` module

**Pending (operator HUMAN GATE):**
- Operator signature in `REVIEWED.md` confirming baseline review before Phase 1 destructive actions

---

## Phase status matrix

| Phase | Task | State | Owner |
|-------|------|-------|-------|
| **0 Real Usage Audit** | T0.1 audit Go tool + baseline CSV | ✅ **DONE** (engineering); operator review PENDING | Ralph + Operator |
| **1 Delete Dead Code** | T1.1 dead-set validation (manual) | ✅ **DONE** (iter 11) — validated dead-set EMPTY; prevented 15-script destructive delete | Ralph |
| | T1.2 bulk delete validated set | ✅ **NO-OP** (iter 11) — dead-set empty; nothing safely deletable on this branch | Ralph |
| **2 Delegate to Helmfile** | T2.1 audit bare-helm calls | ✅ **DONE** (iter 6) — mapping doc with gaps + EC-01/EC-02 plan | Ralph |
| | T2.2 fix helmfile releases (gitea PASSWD + kratos/hydra YAML escape) | ✅ **DONE** (iter 6) — single-quoted YAML EC-02 | Ralph |
| | T2.3 rewrite infra:install:mgmt (T2.3.a master-sync + T2.3.b portable base64) | ✅ **DONE** (iter 6) — Taskfile fully migrated to helmfile sync | Ralph |
| | T2.4 validate cleanroom funcional | 🔴 **HARD STOP** — operator runs `task clean:all && task setup && task doctor` | Operator |
| **3 Deprecate task setup** | T3.1 update CLAUDE.md + README + dev-loop.md | ✅ **DONE** (iter 7) | Ralph |
| | T3.2 deprecation warning in task setup | ✅ **DONE** (iter 7) — [NOTICE] echoes in setup target | Ralph |
| **4 Migrate to Ginkgo** | T4.1 audit parity/dogfood scripts | ⚪ Parallel-eligible post-Phase-3 | Ralph |
| | T4.2 convert top-5 parity scripts | ⚪ Pending T4.1 | Ralph |
| | T4.3 convert remaining parity + dogfood | ⚪ Pending T4.2 | Ralph |
| | T4.4 delete _legacy/ | ⚪ Pending T4.3 | Ralph |
| **5 SDK-native Go** | T5.1 Pulumi Automation API (pulumi-archive + pulumi-orphan-rm) | ⚪ Parallel-eligible post-Phase-3 | Ralph |
| | T5.2 migrate generators to //go:generate | ✅ **DONE** (iter 8) — gen-state-machine-dots.sh → cmd/gen-dots + //go:generate; script DELETED | Ralph |
| | T5.3 remaining pure-logic gates | ⚪ Pending audit results | Ralph |
| **6 Lock The Door** | T6.1 allowlist.md | ✅ **DONE** (iter 2) — baseline grandfathered 257 entries | Ralph |
| | T6.2 no-new-bash gate (+ EC-04 fetch-depth + EC-06 rename) | ✅ **DONE** (iter 2) — gate ATIVO em CI | Ralph |
| | T6.3 no-shell-wrap audit gate | ✅ **DONE** (iter 5) — AST walker + 14 hermetic tests + CI workflow | Ralph |
| | T6.4 final inventory validation | ⚪ Pending Phases 1-5 + T6.3 | Ralph |
| **7 Adopt External Tools** | T7.1 Pluto CI gate | ✅ **DONE** (iter 3) — workflow + 7 hermetic tests + runbook | Ralph |
| | T7.2 OpenCost helm + Prometheus integration | ✅ **DONE** (iter 4) — wrapper chart + helmfile entry + ServiceMonitor + 9 hermetic tests + runbook | Ralph |
| **Final Gates** | `/cross-validation 2026-05-22-shell-script-strategic` | 🔴 **HARD STOP** | Operator + Ralph |
| | `/dogfood full` on clean infra | 🔴 **HARD STOP** | Operator |
| | `/chaos-test --full` validation | 🔴 **HARD STOP** | Operator |

Legend: ✅ done · 🟡 in-progress · ⚪ pending · 🔴 hard stop (operator-required action)

---

## Next iteration: recommended action

**Option A (default — RECOMMENDED):** Continue with Phase 6+7 paralelo (zero dependency on Phase 1-5 for engineering side). T6.2 (no-new-bash gate) is high-value gate that locks regression even before phases 1-5 reduce inventory. T7.1 (Pluto) and T7.2 (OpenCost) are independent and unblock go-to-market gates.

**Option B:** Implement Phase 2 engineering (T2.1 + T2.2 + T2.3) preparing for operator validation in T2.4. EC-01 + EC-02 must be applied (master-sync, portable base64).

**Option C:** Wait for operator review of T0.1 baseline before any further work.

**Why not Phase 1.2 (delete):** T1.1 validation requires operator manual review per plan. Ralph cannot mass-delete without that signature.

---

## HARD STOPS for promise eligibility

Promise statement requires "CLEANROON, DOGFOOD E CHAOS-TEST FUNCIIONAIS" — all 3 require LIVE infrastructure that Ralph cannot operate:

| Gate | Requirement | Ralph capability |
|------|-------------|------------------|
| Cleanroom | `task clean:all` (destroys 4 k3d clusters) + `task setup` (30+min) + `tilt up` + `task doctor` PASS=38 FAIL=0 | ❌ Cannot — `task clean:all` is destructive operator action |
| Dogfood | `/dogfood full` on clean infra + 12 journey suite PASS + Prova de Vida + Prova de Morte | ❌ Cannot — requires LIVE pods + JWT (TTL=1h) |
| Chaos-test | `/chaos-test --full` 10 scenarios with chaos-mesh injection + baseline measure + cleanup | ❌ Cannot — requires running cluster + chaos-mesh |

**Conclusion:** Ralph can complete all engineering side (Phases 0-7 code/tests/CI gates) but the 3 LIVE gates require operator handoff. Promise will remain false until operator validates.

---

## Honest assessment

- **Engineering progress:** 1/22 tasks (T0.1 complete)
- **Hard stops:** 4 (T2.4 cleanroom + final dogfood + chaos + cross-validation operator action)
- **Promise eligibility:** FALSE until all 22 done + 4 LIVE gates PASS
