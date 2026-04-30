# Plan: Normalize `infra/` + Merge `refactorhelms` + 100% CLI Functional + Multi-Cell Live

> **Version 1.0** — Consolidate all open structural issues in `infra/` (Tilt↔Helmfile contradiction, 4 paralelas Pulumi stacks, 3 orphan Tilt yamls, dead-code, doc drift), formalize the merge of branch `refactorhelms` (PR #51) into `develop`, audit + fix every command of `loopzero` CLI to a 100% functional bar against the post-merge architecture, and activate the second runtime cluster (cell-2) end-to-end so the platform actually executes in multi-cell mode. Expected outcome: `task parity:k3d-virgin` GREEN with 4 split operators + helmfile reflecting the same; every CLI command returns the documented exit code and JSON shape; `loopzero deploy` against a tenant pinned to cell-2 produces HTTP 200 in `runtime-2`; ADR-0021/0022/0023 ratify the structural decisions; and CI gates all of it permanently.

## Context

The 2026-04-30 deep review of `infra/` (run on `fix/parity-on-refactorhelms` after PR #51) surfaced **eight structural issues** plus an unfinished feature. They are documented findings, not speculation:

1. **S0 — Tilt ≠ Helmfile (Rule 10 break).** PR #51 split the operator monolith into 4 binaries (`cmd/{app,platform,preview,tenant}-operator/main.go`), shipped 4 charts in `charts/loopzero-*-operator`, and updated `Tiltfile` to deploy them. **`infra/helmfile/mgmt.helmfile.yaml.gotmpl` still references the legacy monolith chart `tenant-controller`** — so `loopzero platform install` (canonical install path per ADR D2) produces 1 monolithic pod while `tilt up` produces 4 split pods. Local ≠ production. Evidence: `grep -E "^  - name:" infra/helmfile/mgmt.helmfile.yaml.gotmpl | grep -i controller` shows `loopzero-controller` + `tenant-controller` only.

2. **S1 — 4 Pulumi stacks, 3 orphans.** `infra/pulumi/` (canonical, wired via `cli/src/installer/pulumi-runner.ts:resolvePulumiProjectDir`) coexists with `infra/pulumi-infra/`, `infra/pulumi-platform/`, `infra/pulumi-control-plane/`. The latter 3 are referenced **only** by `infra/scripts/provision-homolog.sh`. Two of them (`pulumi-platform`, `pulumi-control-plane`) import `@pulumi/kubernetes` (5 + 1 imports respectively), violating ADR-0020 D7 ("Pulumi is L1, Helmfile is L2"). `pulumi-infra/Pulumi.yaml` declares `name: loopzero-infra` — **identical** to `infra/pulumi/Pulumi.yaml`, risking state collision.

3. **S1 — 3 orphan Tilt deployments.** `infra/tilt/{operator,controller,cloud}-deployment.yaml` are not referenced by `Tiltfile` after PR #51's restructure. Last touched by `65543040`/`55ce9881`/`c53e3a74` (pre-split). Latent re-import risk per CLAUDE.md "Implemented but not wired" pattern.

4. **S2 — `regression-checks.sh` profile-aware undocumented.** Accepts `LOOPZERO_REGRESSION_PROFILE=standalone` to skip multi-cluster checks, but no runbook explains it. Operators rediscover the flag by reading source.

5. **S2 — `regression_test.go` string-literal anti-pattern.** This session fixed 4 fails (B25/B28/B31/F12) caused by tests asserting on implementation-string-literals (gate names, tag patterns, helper names). Any semantic-equivalent refactor breaks the suite. Behavioural tests required.

6. **S2 — `infra/cell-templates/v1.0.0` dead.** No README; CLAUDE.md says "Cell Phase 2b activation pending Sprint 9". Contributors cannot tell if this is live or deferred.

7. **S3 — `charts/loopzero-platform/Chart.yaml` umbrella requires `helm dependency build`.** Not in `task validate:manifests`; a fresh clone fails `helm template charts/loopzero-platform/`.

8. **S3 — Sloth SLO files (`infra/sloth/*.yaml`) bypass kubeconform.** Pre-processed format. No `task validate:slos` calling `sloth validate`.

**Unfinished feature — Multi-Cell.** Per CLAUDE.md: "Cell architecture Phase 0+1+1.5 complete (runtime-2 physically provisioned but not routed; cell-2 activation Sprint 9). Cell Phase 2b activation (code 100%, Pulumi.dev.yaml multi-cell config missing)." `infra/pulumi/src/components/infra.ts:160-165` provisions a `${baseName}-runtime-2` DOKS cluster but `Pulumi.dev.yaml` lacks `cellId`/`runtime2*` keys required by `placement/dynamic_router.go:44` to route tenants to it.

**Branch state.** `refactorhelms` (PR #51 head `29b4d392`) was reviewed and rebased on top of `develop`. This session pushed `fix/parity-on-refactorhelms` (commit `1f36d5aa`) with 4 test alignments that make every suite GREEN on top of the PR. Per the user, "o merge foi feito" — semantically the PR is approved and the alignment branch is the post-merge baseline; this plan formalizes that as the develop tip.

Cross-references:
- `docs/decisions/ADR-0020-infra-parity-decisions.md` (D1-D7 in force)
- `docs/runbooks/bootstrap-regression-matrix.md` (135 entries, 109/135 protected)
- `docs/runbooks/parity-validation.md` (T5.1 GREEN run 6, T6.1/T7.1/T8.1 cloud-pendentes)
- CLAUDE.md "Helm Chart Dual System" (added by PR #51)
- CLAUDE.md "Known Systemic Patterns" → "Implemented but not wired"

## Objective

**Done = `task parity:k3d-virgin` GREEN with 4 split operators living, every `loopzero` CLI command exits 0/1/2/3/4/5 with the documented JSON, and a tenant pinned to cell-2 returns HTTP 200 from `runtime-2` end-to-end.**

Specific measurable goals:
1. **Zero S0/S1 findings** open: helmfile installs the 4 split operators behind a feature flag that defaults to `true` outside `standalone`; `provision-homolog.sh` + 3 orphan Pulumi stacks deleted OR formally promoted via ADR-0021; 3 orphan Tilt yamls deleted.
2. **`refactorhelms` branch fully integrated**: develop tip equals (or fast-forwards to) the post-merge state including the 4 test fixes.
3. **12 CLI command groups audited and tested**: `auth`, `deploy`, `domain`, `env`, `infra`, `init`, `platform`, `preview`, `project`, `scale`, `team`, plus internals. Each command has at least one integration test exercising the success path against post-PR-#51 code, exit code matrix verified, JSON schema matched.
4. **Multi-cell live**: `Pulumi.dev.yaml` has `cellId` + `runtime2*` keys; `dynamic_router.go` routes a tagged tenant to `runtime-2`; ArgoCD app for cell-2 syncs; HTTP 200 reachable on a cell-2 tenant URL.
5. **CI gates all of it**: `parity-validation.yml` + new `cli-commands.yml` + new `multi-cell.yml` PR-gate green on every push.
6. **Documentation updated**: ADR-0021 (split-operator helmfile rollout), ADR-0022 (Pulumi consolidation decision), ADR-0023 (cell-2 activation), runbook updates, CHANGELOG entries, regression matrix new entries.

## ADRs

### D1 — Helmfile owns operator topology, not Tilt

**Decision:** `infra/helmfile/mgmt.helmfile.yaml.gotmpl` becomes the single source of truth for which operator binaries are deployed. Tilt re-uses the helmfile output (via `helmfile template` → `k8s_yaml` ingestion) rather than declaring deployments in `infra/tilt/*-deployment.yaml`. The 4 split operators (`loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`) become helmfile releases, gated by a values flag `splitOperators` (default `true` for `dev`/`prod`/`standalone-multi`, `false` for `standalone` to preserve degrau-1 GREEN until validated).

**Rationale:** Today Tilt deploys 4 split pods while Helmfile deploys 1 monolith — Local ≠ Production (Rule 10 violation). Two source-of-truth files is the disease; the cure is one. Helmfile wins because (a) it's referenced by ADR-0020 D2 as the canonical install path, (b) it already gates by environment, (c) Tilt is the developer tool, not the production tool.

**Consequences:**
- Enables the binary split benefits (blast-radius reduction, scoped RBAC, leader election per domain) end-to-end, not just in `tilt up`.
- Forces a transitional phase: `tenant-controller` chart (monolith) and 4 split charts coexist behind the flag for one full sprint while we validate split behaviour.
- The 3 orphan `infra/tilt/{operator,controller,cloud}-deployment.yaml` get deleted (no longer reachable from any path).

### D2 — Single Pulumi stack until proven otherwise

**Decision:** `infra/pulumi/` is the only Pulumi project. Delete `infra/pulumi-infra/`, `infra/pulumi-platform/`, `infra/pulumi-control-plane/`, `infra/pulumi-shared/`, and `infra/scripts/provision-homolog.sh`. If/when L1/L2/L3 split is needed, ADR-0021 (NEW) records the new layout with rationale.

**Rationale:** YAGNI. The 3 paralelas dirs were referenced only by `provision-homolog.sh` (homolog isn't an active environment per CLAUDE.md). They duplicate code (`infra/pulumi/src/components/infra.ts` ≠ `infra/pulumi-infra/src/components/infra.ts` in subtle ways), import `@pulumi/kubernetes` (violating ADR-0020 D7), and risk state collision (`pulumi-infra/Pulumi.yaml` declares `name: loopzero-infra` identical to canonical). Cost of keeping > cost of deleting.

**Consequences:**
- Removes ~90 .ts files from the repo. Lint/grep/audit surface area drops.
- Forecloses the homolog environment until explicitly resurrected with an ADR.
- If split-by-tier is later needed, that's a fresh design with no half-finished baggage.

### D3 — Multi-cell activates via Pulumi config + database tag, not new code

**Decision:** Cell-2 activation is a **configuration change**, not new code. The runtime-2 DOKS cluster + `dynamic_router.go` are already in place (Phase 0+1+1.5 done per CLAUDE.md). Activation = (a) add `cellId: "cell-2"` to runtime-2 Pulumi config + emit `runtime2Kubeconfig` + `runtime2K8sVersion` outputs that helmfile already consumes; (b) populate the `cells` table via migration with cell-1 + cell-2 rows; (c) tag a test tenant with `cell_id = 'cell-2'`; (d) ArgoCD sync produces the same tenant Application but in `runtime-2` cluster; (e) HTTP 200 verified.

**Rationale:** The hard work is done. Adding more code now would be premature abstraction. The activation path is the simplest one that exercises the full chain.

**Consequences:**
- Forces verification that `dynamic_router.go` actually reads the `cells` table and routes correctly — this hasn't been live-tested per CLAUDE.md.
- Establishes the precedent that adding cell-N is a config change forever.
- ADR-0023 records the activation playbook (NEW runbook).

### D4 — CLI command bar: every group has at least one integration test

**Decision:** Each of the 12 register groups (`auth`, `deploy`, `domain`, `env`, `infra`, `init`, `platform`, `preview`, `project`, `scale`, `team`) has at least one integration test in `cli/tests/integration/` that spawns the binary, hits the success path, asserts exit code + JSON shape (when `--json` applies). Unit tests remain (1171 today) but are not sufficient for "1000% functional" — fork-based exit-code contract test (`exit-code-contract.test.ts` pattern) is the floor.

**Rationale:** Unit tests mock dependencies; they catch most regressions but miss plumbing bugs (the F-02 audit found `bin/loopzero` exit-code wiring needed fork-based test to surface). User said "1000% funcional"; that's only meaningful if the test bar matches.

**Consequences:**
- Adds ~12 integration test files. Runtime <60s total against a mock API (per existing pattern: `LOOPZERO_URL=http://127.0.0.1:1`).
- Forces the audit: any command that has no integration test is suspected dead-code or partially-wired.
- CI gate `cli-commands.yml` runs these on every PR.

### D5 — Validation matrix gates the merge

**Decision:** Before declaring this plan DONE, the following must all be GREEN simultaneously: (a) `task parity:preflight`, (b) `task parity:k3d-virgin`, (c) full CLI suite (1171+12 new), (d) full Pulumi vitest (96), (e) `infra/tests/...` Go suite (5 packages), (f) end-to-end multi-cell smoke (`scripts/cell2-smoke.sh` NEW), (g) `task validate:manifests` + `task validate:helm-deps` (NEW) + `task validate:slos` (NEW), (h) zero `@pulumi/kubernetes` imports anywhere except via gate-test exception list, (i) regression matrix integrity audit (`audit-regression-matrix.sh`), (j) audit golden test (`audit-golden-test.sh`).

**Rationale:** "All green" is the only objective signal. Any individual gate green is necessary; the conjunction is sufficient.

**Consequences:**
- One end-of-plan validation script (`infra/tests/parity/preflight-extended.sh` or extended `preflight.sh`) runs all 10 gates.
- CI `multi-cell.yml` gates merges.

### D6 — `regression_test.go` strings-literais → behavioural tests (DEBT registered, deferred)

**Decision:** This plan does NOT refactor `regression_test.go` to behavioural tests. The DEBT is registered with a follow-up task in the regression matrix; the present plan only *aligns* tests with current implementation strings (already done in this session for B25/B28/B31/F12). Future plan addresses pattern.

**Rationale:** Refactoring the regression suite is 1-2 days of work that does not move the needle on the user's three goals (clean infra, CLI functional, multi-cell live). YAGNI for this plan; explicit DEBT in matrix.

**Consequences:**
- Future infra refactors will continue to need test-string updates until the DEBT is paid.
- Matrix gets entry #136 marking the DEBT.

### D7 — refactorhelms merge is recorded as fast-forward of `fix/parity-on-refactorhelms` to `develop`

**Decision:** Treat the merge as already conceptually done (per user). The artifact: `develop` advances to `1f36d5aa` (current `fix/parity-on-refactorhelms` tip = PR #51 head + 4 test alignment). Phase 0 of this plan ratifies it via PR if not already merged.

**Rationale:** The user stated the merge happened. If the GitHub state disagrees (since `origin/develop` is still `8b46661a` per check), Phase 0 reconciles via a clean fast-forward push. Either way, the post-merge state is what's reviewed.

**Consequences:**
- Phase 0 is short and safe.
- The 4 test-alignment fixes ride along.

## Dependency Graph

```
Phase 0 (merge ratify) ──▶ Phase 1 (Tilt↔Helmfile) ──▶ Phase 2 (Pulumi consolidation)
                                  │
                                  ▼
                         Phase 3 (CLI audit + tests)
                                  │
                                  ▼
                         Phase 4 (Multi-cell activation) ──▶ Phase 5 (CI gates)
                                                                     │
                                                                     ▼
                                                            Phase 6 (Validation matrix)
                                                                     │
                                                                     ▼
                                                            Phase 7 (Docs + close)
```

- Phase 0 (small, blocking).
- Phase 1 + Phase 2 are independent of each other but both block Phase 3 (CLI audit must run against the final operator topology AND the final Pulumi layout).
- Phase 3 blocks Phase 4 (CLI must be functional before pinning a tenant to cell-2 via CLI).
- Phase 5 (CI) and Phase 6 (validation matrix) can start once Phase 4 is in progress; both block Phase 7 (close).

---

## Phase 0: Ratify refactorhelms merge

**Objective:** Make `develop` reflect the post-PR-#51 state including the 4 test alignments shipped this session.

### T0.1 — Fast-forward `develop` to `fix/parity-on-refactorhelms` head

#### Objective
If `origin/develop` is behind the post-merge state, push a fast-forward (or merge PR if upstream policy requires). After T0.1, `git log origin/develop -1` returns `1f36d5aa` (or its squash-merge equivalent on develop).

#### Evidence
`git ls-remote origin refs/heads/develop` returns `8b46661a` (pre-PR-#51); `fix/parity-on-refactorhelms` is `1f36d5aa` and is GREEN end-to-end. User declared the merge done — Phase 0 makes that durable in git.

#### Files to edit
```
(none — git operations only)
```

#### Deep file dependency analysis
- No source file edits. `git switch develop && git merge --ff-only fix/parity-on-refactorhelms && git push origin develop`. If non-fast-forward, open PR for upstream review.
- Downstream phases assume `develop` tip = post-PR-#51-with-test-alignments.

#### Deep Dives
- **Invariant**: `develop` history retains every commit from this session (`cb5290af`..`8b46661a` already present) plus `1f36d5aa` (test alignments).
- **Edge case**: if upstream policy disallows direct push to `develop`, Phase 0 opens a PR titled "Ratify PR #51 + parity test alignment" and waits for approval. Block is documented; rest of plan blocks on it.

#### Tasks
1. Verify `origin/develop` HEAD (`git ls-remote origin refs/heads/develop`).
2. If HEAD ≠ `1f36d5aa`, attempt `git push origin fix/parity-on-refactorhelms:develop --dry-run`.
3. If fast-forward possible, perform real push.
4. If non-fast-forward, open PR via `gh pr create --base develop --head fix/parity-on-refactorhelms`.
5. Once develop matches, delete remote `fix/parity-on-refactorhelms` branch (cleanup).

#### TDD
```
RED:     test_develop_at_post_pr51_baseline() — assert `git log origin/develop -1 --format=%H` returns `1f36d5aa` or its equivalent
GREEN:   Push or merge per Tasks 1-4
REFACTOR: None
VERIFY:  git fetch origin && git rev-parse origin/develop
```

#### Acceptance Criteria
- [ ] `origin/develop` tip = post-merge state
- [ ] All commits from this session preserved in develop history
- [ ] No `--force` push used
- [ ] Branch `fix/parity-on-refactorhelms` deleted from origin (or kept as PR record)

#### DoD
- [ ] `develop` advanced
- [ ] CI on `develop` GREEN
- [ ] Memory entry updated (`reference_infra_parity_2026_04_30.md` notes the ratification)

---

## Phase 1: Resolve Tilt↔Helmfile contradiction (S0)

**Objective:** Make `loopzero platform install -f values.yaml` deploy the same 4 split operator pods that `tilt up` deploys. Enforce via test.

### T1.1 — Add 4 split-operator releases to mgmt helmfile behind `splitOperators` flag

#### Objective
Add `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator` as releases in `infra/helmfile/mgmt.helmfile.yaml.gotmpl`, gated by `splitOperators` value (default true outside `standalone`). The legacy `tenant-controller` and `loopzero-controller` releases stay during the transition, gated to `not splitOperators` so they coexist for rollback safety.

#### Evidence
`grep -E "^  - name:" infra/helmfile/mgmt.helmfile.yaml.gotmpl | grep -i controller` shows only `loopzero-controller` + `tenant-controller`. PR #51 created `charts/loopzero-{tenant,app,preview,platform}-operator` — fully formed Helm charts, not referenced by helmfile.

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — add 4 new releases, gate legacy on negation of splitOperators
infra/helmfile/environments/dev.yaml — splitOperators: true
infra/helmfile/environments/prod.yaml — splitOperators: true
infra/helmfile/environments/standalone-multi.yaml — splitOperators: true
infra/helmfile/environments/standalone.yaml — splitOperators: false (degrau-1 protection until T1.5 dogfood)
charts/loopzero-tenant-operator/values.yaml — sane defaults (image, resources, RBAC)
charts/loopzero-app-operator/values.yaml — same
charts/loopzero-preview-operator/values.yaml — same
charts/loopzero-platform-operator/values.yaml — same
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml.gotmpl`: today references `../helm/loopzero-controller` + `../helm/tenant-controller`. New releases reference `../../charts/loopzero-{tenant,app,preview,platform}-operator` (the OCI-published, more complete charts that PR #51 created). Gate logic: `{{- if .Values.splitOperators }}` block adds 4; `{{- else }}` block adds the 2 legacy.
- `environments/*.yaml`: thin overrides, just the flag.
- `charts/loopzero-*-operator/values.yaml`: each ship-ready chart needs verified default `image.repository`, `resources`, `serviceAccount.create: true`, scoped `rbac.role`, leader-election lease name, probes.
- Downstream: `cli/src/installer/status.ts` `LOOPZERO_COMPONENTS` must include the new operators (or be made flag-aware).

#### Deep Dives
- **Invariant**: with `splitOperators=true`, helmfile sync produces 4 operator deployments + 0 monolith deployment. With `false`, exactly the inverse.
- **Edge case — RBAC overlap**: legacy `tenant-controller` ClusterRole and split operators' ClusterRoles overlap (both reference Tenant CR). If both are installed simultaneously, leader election prevents double-reconcile but RBAC is loud. Helmfile `needs:` ensures legacy is uninstalled before split is installed during the transition (`helmfile sync` removes orphans).
- **Edge case — image tag**: 4 new operators must have published images. Phase 1 assumes they exist (PR #51 added Dockerfiles + CI workflow); if registry is empty, T1.1 includes a sub-task to push placeholder `:latest` images locally.
- **Edge case — `getPlatformStatus`**: must report `Ready` for both topologies. Either make LOOPZERO_COMPONENTS environment-aware or have it accept both controller naming.

#### Tasks
1. Audit `charts/loopzero-*-operator/` Chart.yaml + values.yaml for production-readiness gaps (probes, resources, security context).
2. Fill gaps with secure defaults (matching `infra/helm/tenant-controller` baseline).
3. Add `{{- if .Values.splitOperators }}` block in mgmt.helmfile with 4 releases, ordered by `needs:`.
4. Add `{{- else }}` block keeping legacy.
5. Update each `environments/*.yaml` with `splitOperators` flag.
6. Update `cli/src/installer/status.ts` `LOOPZERO_COMPONENTS` to dynamically check both topologies (`splitOperators === true` → query split; else → query legacy).
7. Update `cli/tests/unit/getPlatformStatus-charts.test.ts` to cover both topologies.

#### TDD
```
RED:     test_mgmt_helmfile_renders_split_operators_when_flag_true() — render `helmfile -e dev list` and assert 4 split releases present, 0 legacy
RED:     test_mgmt_helmfile_renders_legacy_when_flag_false() — render `helmfile -e standalone list` and assert 2 legacy releases present, 0 split
RED:     test_charts_loopzero_tenant_operator_has_resource_limits() — helm template + grep for `limits:`
RED:     test_charts_loopzero_tenant_operator_runs_as_nonroot() — helm template + grep `runAsNonRoot: true`
RED:     test_LOOPZERO_COMPONENTS_aware_of_split_topology() — getPlatformStatus with splitOperators=true returns Ready when 4 split deployments healthy
GREEN:   Implement Tasks 1-7
REFACTOR: Extract `splitOperatorReleases()` helper in helmfile.gotmpl if syntax allows, else inline
VERIFY:  helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e dev list 2>&1 | grep -E "tenant-operator|app-operator|preview-operator|platform-operator" | wc -l == 4
```

#### Acceptance Criteria
- [ ] Helmfile renders 4 split releases in dev/prod/standalone-multi
- [ ] Helmfile renders 2 legacy in standalone
- [ ] All 4 split charts pass `helm lint` + `kubeconform --strict`
- [ ] `task validate:manifests` GREEN
- [ ] CLI `getPlatformStatus()` returns `Ready` against both topologies
- [ ] `cli/tests/unit/getPlatformStatus-charts.test.ts` GREEN with new cases
- [ ] Pass: code-audit complexity check (cyclomatic <= 10)
- [ ] Pass: code-audit lint check (zero warnings)

#### DoD
- [ ] `helmfile -e dev list` and `-e standalone list` both work
- [ ] `helm template charts/loopzero-tenant-operator` GREEN
- [ ] CLI tests 1171 + 4 new GREEN
- [ ] `task validate:manifests` GREEN

---

### T1.2 — Migrate `Tiltfile` to consume helmfile output

#### Objective
`Tiltfile` stops referencing `infra/tilt/{api,*-operator}-deployment.yaml` directly. Instead, it runs `helmfile -e standalone-multi build` and feeds the output to `k8s_yaml`. This guarantees `tilt up` deploys exactly what `loopzero platform install` would.

#### Evidence
Today `Tiltfile` at lines 114, 134, 148, 162, 176 hard-codes 5 deployment yamls. Manually keeping these in sync with helmfile is the source of the S0 finding.

#### Files to edit
```
Tiltfile — replace k8s_yaml(infra/tilt/*-deployment.yaml) with helmfile-rendered output
infra/tilt/api-deployment.yaml — DELETE (rendered by helmfile loopzero-api release)
infra/tilt/tenant-operator-deployment.yaml — DELETE
infra/tilt/app-operator-deployment.yaml — DELETE
infra/tilt/preview-operator-deployment.yaml — DELETE
infra/tilt/platform-operator-deployment.yaml — DELETE
infra/tilt/operator-deployment.yaml — DELETE (orphan, never referenced post PR #51)
infra/tilt/controller-deployment.yaml — DELETE (orphan)
infra/tilt/cloud-deployment.yaml — DELETE (orphan)
```

#### Deep file dependency analysis
- `Tiltfile`: 210 lines today. Replacement uses `local('helmfile -f infra/helmfile/mgmt.helmfile.yaml.gotmpl -e standalone-multi build', echo_off=True)` and pipes to `k8s_yaml(blob(...))`. Tilt re-runs this on file change in `infra/helmfile/` or `charts/loopzero-*-operator/`.
- 8 yaml deletions: confirmed orphan (3) or rendered-by-helmfile-now (5).
- Downstream: dev loop docs in `docs/runbooks/` may reference the deleted paths.

#### Deep Dives
- **Invariant**: `tilt up` produces the exact set of K8s resources that `helmfile -e standalone-multi sync` produces.
- **Edge case — image rebuild**: Tilt's hot-reload (`docker_build` + `live_update`) targets `loopzero-api`, etc. The deployment names + container names rendered by helmfile must match what `docker_build` references. Verify via `tilt up` after T1.2 — if image not picked up, adjust `docker_build` to use the helmfile-rendered Deployment name.
- **Edge case — `tilt down`**: should still leave infra (PG, Gitea, MinIO) alone per existing CLAUDE.md contract. Helmfile-rendered manifests inherit Tilt's `tilt_down` semantics; verify the labels match.

#### Tasks
1. Write Tiltfile helper `helmfile_yaml(env)` that executes helmfile build and returns blob.
2. Replace 5 explicit `k8s_yaml(...)` calls with helper invocations.
3. Delete 8 yaml files in `infra/tilt/`.
4. Test `tilt up` end-to-end: 4 operators + loopzero-api + loopzero-cloud running, hot-reload on `api/` change still works.
5. Update `docs/runbooks/dev-loop.md` (if referenced).

#### TDD
```
RED:     test_tiltfile_does_not_reference_orphan_yamls() — grep Tiltfile for "operator-deployment.yaml" excluding the 4 split-operator names
RED:     test_helmfile_rendered_pods_match_tilt_pods() — `kubectl get deploy` after `tilt up` == helmfile build deploy names
GREEN:   Implement helmfile_yaml() helper + delete files
REFACTOR: None
VERIFY:  tilt up && kubectl --context k3d-loopzero-mgmt get deploy -n loopzero-system | grep -E "tenant-operator|app-operator|preview-operator|platform-operator|loopzero-api"
```

#### Acceptance Criteria
- [ ] `Tiltfile` references zero files in `infra/tilt/`
- [ ] 8 orphan yamls deleted
- [ ] `tilt up` boots cleanly in <90s
- [ ] Hot-reload on api/ change updates pod
- [ ] `tilt down` does NOT remove infra (PG, Gitea, MinIO)

#### DoD
- [ ] `tilt up` works
- [ ] `tilt down` works as documented
- [ ] No orphan yamls in `infra/tilt/`

---

### T1.3 — `splitOperators=true` GREEN end-to-end on virgin k3d

#### Objective
Run `task parity:k3d-virgin` after T1.1+T1.2 with `splitOperators=true` in standalone-multi. Confirm 4 split operators reach `phase=Ready`. This validates the canonical install path uses split operators.

#### Evidence
T5.1 run 6 (2026-04-30) was GREEN against the monolith. After this plan, the gate must remain GREEN against the new topology.

#### Files to edit
```
infra/tests/parity/k3d-virgin-install.sh — accept SPLIT_OPERATORS env, default true post-T1.1
infra/tests/parity/standalone-values.yaml — splitOperators flag explicit
docs/runbooks/parity-validation.md — add Achado entries for findings discovered during this task
docs/runbooks/bootstrap-regression-matrix.md — add new entries for new findings
```

#### Deep file dependency analysis
- `k3d-virgin-install.sh`: env-driven flag, defaults to true.
- `standalone-values.yaml`: thin override.
- Runbooks: new troubleshooting entries for any Achados surfaced (expected ≥0).

#### Deep Dives
- **Invariant**: Step 6 of `loopzero platform install` reports `phase=Ready` with 4 split operators visible.
- **Edge case — RBAC propagation race**: split operators install ClusterRoles in parallel; if `needs:` ordering is wrong, one might start before its CRD is registered. Validate via dogfood, document in matrix.

#### Tasks
1. Run `task parity:k3d-virgin` baseline (with monolith, splitOperators=false in standalone).
2. Switch standalone to `splitOperators=true`, re-run.
3. For each Achado: fix in code, document in matrix, retry.
4. Iterate until exit 0 + phase=Ready.

#### TDD
```
RED:     test_parity_k3d_virgin_with_split_operators() — script exits 0
GREEN:   Run script, fix Achados as they surface
REFACTOR: None expected (any structural fixes feed back into T1.1)
VERIFY:  bash infra/tests/parity/k3d-virgin-install.sh; kubectl get deploy -n loopzero-system | grep -E "operator" | wc -l == 4
```

#### Acceptance Criteria
- [ ] `task parity:k3d-virgin` exits 0
- [ ] 4 operator deployments Ready in loopzero-system
- [ ] 0 monolith pods
- [ ] All matrix Achados from this run captured

#### DoD
- [ ] Live GREEN run logged
- [ ] Matrix updated
- [ ] Audit-script GREEN (`task parity:audit`)

---

## Phase 2: Pulumi consolidation (S1)

**Objective:** Single source of truth for cloud infra Pulumi.

### T2.1 — Delete 3 orphan Pulumi stacks + provision-homolog

#### Objective
Remove `infra/pulumi-infra/`, `infra/pulumi-platform/`, `infra/pulumi-control-plane/`, `infra/pulumi-shared/`, `infra/scripts/provision-homolog.sh`, `infra/scripts/seed_e2e.go` (homolog-only).

#### Evidence
`grep -rn "pulumi-platform\|pulumi-control-plane" cli/src api/internal Taskfile.yml` shows zero references. Only `provision-homolog.sh` references them. Homolog is not active per CLAUDE.md.

#### Files to edit
```
infra/pulumi-infra/ — DELETE entire dir
infra/pulumi-platform/ — DELETE
infra/pulumi-control-plane/ — DELETE
infra/pulumi-shared/ — DELETE
infra/scripts/provision-homolog.sh — DELETE
infra/scripts/seed_e2e.go — DELETE (only used by provision-homolog)
docs/decisions/ADR-0021-pulumi-single-stack.md — (NEW) record decision
CHANGELOG.md — Removed entry
```

#### Deep file dependency analysis
- 4 directories: ~109 files total. Confirmed isolation via grep.
- ADR-0021 documents what was deleted, why (D2), and what the resurrection path looks like (greenfield rewrite, not revival of these dirs).
- CHANGELOG entry references the ADR.

#### Deep Dives
- **Invariant**: `infra/pulumi/` becomes the only Pulumi project. `find infra -name 'Pulumi.yaml'` returns 1 result.
- **Edge case — vitest tests**: Pulumi tests live in `infra/pulumi/src/__tests__/`. After deletion, vitest still runs from `infra/pulumi/` only.
- **Edge case — node_modules**: deleted dirs have node_modules; `du` confirms ~500MB recoverable.

#### Tasks
1. Confirm zero references via grep one more time.
2. `rm -rf` the 4 dirs + 2 scripts.
3. Write ADR-0021 with rationale.
4. Add CHANGELOG entry.
5. Run `task validate:manifests` + Pulumi vitest to confirm no broken imports.

#### TDD
```
RED:     test_only_one_pulumi_project_exists() — find infra -name 'Pulumi.yaml' | wc -l == 1
RED:     test_no_orphan_pulumi_imports_in_codebase() — grep -r "pulumi-platform\|pulumi-control-plane\|pulumi-shared" — zero hits
GREEN:   rm -rf the dirs + write ADR
REFACTOR: None
VERIFY:  find infra -name 'Pulumi.yaml' | wc -l; cd infra/pulumi && npx vitest run --reporter=basic
```

#### Acceptance Criteria
- [ ] 1 Pulumi project remains (`infra/pulumi/`)
- [ ] 0 grep hits for orphan stack references in source
- [ ] ADR-0021 committed
- [ ] CHANGELOG entry
- [ ] Pulumi vitest 96/96 GREEN

#### DoD
- [ ] Repo clean
- [ ] Vitest GREEN
- [ ] No regressions in `task validate:manifests`

---

### T2.2 — Sprint8-fixes test extends to detect any future Pulumi paralelas

#### Objective
Hardening: `infra/pulumi/src/__tests__/sprint8-fixes.test.ts` already audits zero `@pulumi/kubernetes` in `infra/pulumi/`. Extend it to fail if ANY `infra/pulumi*/` directory appears outside the canonical `infra/pulumi/`.

#### Evidence
T2.1 deletes the orphans now; T2.2 prevents future re-introduction.

#### Files to edit
```
infra/pulumi/src/__tests__/sprint8-fixes.test.ts — add Fix #10: only one Pulumi project root
```

#### Deep file dependency analysis
- Single test file edit. Reads filesystem via `fs.readdirSync('infra')`; counts entries matching `pulumi*`.

#### Deep Dives
- **Invariant**: `fs.readdirSync('infra').filter(d => d.startsWith('pulumi') && d !== 'pulumi').length === 0`.
- **Edge case — pulumi-shared as type-only ref**: still rejected. Forces ADR for any future split.

#### Tasks
1. Add `describe('Fix #10: ...')` block.
2. Implement filesystem check.
3. Run test, confirm GREEN post-T2.1.

#### TDD
```
RED:     test_only_canonical_pulumi_dir_exists() — fs.readdirSync('infra') filtered for 'pulumi*' has length 1
GREEN:   Add the test (it should immediately pass post-T2.1)
REFACTOR: None
VERIFY:  cd infra/pulumi && npx vitest run src/__tests__/sprint8-fixes.test.ts
```

#### Acceptance Criteria
- [ ] Test added, currently PASS
- [ ] Test FAILS if a new `infra/pulumi-foo/` is added (validated by adding then removing dummy dir)

#### DoD
- [ ] Pulumi vitest 97/97 GREEN
- [ ] Test demonstrably catches reintroduction

---

## Phase 3: CLI commands audit + integration tests (D4)

**Objective:** Each of the 12 register groups has at least one integration test against post-PR-#51 code; every `--json` invocation matches the spec'd schema; every exit code is 0/1/2/3/4/5.

### T3.1 — Inventory + classify all CLI commands

#### Objective
Produce `docs/cli-commands-audit.md` (NEW) listing every subcommand of every register, its expected exit codes, JSON schema, and current integration-test status.

#### Evidence
12 register files exist; CLAUDE.md contract demands JSON contract + exit code matrix.

#### Files to edit
```
docs/cli-commands-audit.md — (NEW) inventory + audit
```

#### Deep file dependency analysis
- Audit doc only. Source: `cli/src/register/*.ts` + existing `cli/tests/integration/exit-code-contract.test.ts`.

#### Deep Dives
- **Invariant**: every command has a row.
- **Edge case — alias commands**: e.g., `loopzero team list` and `loopzero teams list` if both exist; one row per source-of-truth command.

#### Tasks
1. Read each register file, extract command list.
2. For each: document exit codes (success path + error paths), JSON shape (if `--json` is supported), current integration-test coverage.
3. Mark gaps: which commands have ZERO integration test.
4. Output table.

#### TDD
```
RED:     test_audit_doc_lists_every_register_command() — grep audit doc for every command name found in cli/src/register/
GREEN:   Write audit doc
REFACTOR: None
VERIFY:  awk-style cross-check
```

#### Acceptance Criteria
- [ ] Every command listed
- [ ] Exit-code matrix per command
- [ ] JSON schema per command
- [ ] Coverage gap list explicit

#### DoD
- [ ] Doc committed
- [ ] Cross-check script passes

---

### T3.2 — Add 12 integration tests (1 per register group)

#### Objective
Each `cli/tests/integration/{group}-success.test.ts` (12 NEW files) spawns `loopzero {group} {success-command}` against a mock API (`LOOPZERO_URL=http://127.0.0.1:1` + nock or fixture API), asserts exit 0 + JSON shape.

#### Evidence
T3.1 audit reveals N gaps; T3.2 closes them.

#### Files to edit
```
cli/tests/integration/auth-success.test.ts — (NEW) `loopzero whoami --json`
cli/tests/integration/deploy-success.test.ts — (NEW) `loopzero deploy --json` (mock API)
cli/tests/integration/domain-success.test.ts — (NEW) `loopzero domain list --json`
cli/tests/integration/env-success.test.ts — (NEW) `loopzero env list --json`
cli/tests/integration/infra-success.test.ts — (NEW) `loopzero infra list --json`
cli/tests/integration/init-success.test.ts — (NEW) `loopzero init --json`
cli/tests/integration/platform-success.test.ts — (NEW) `loopzero platform status --json`
cli/tests/integration/preview-success.test.ts — (NEW) `loopzero preview list --json`
cli/tests/integration/project-success.test.ts — (NEW) `loopzero project list --json`
cli/tests/integration/scale-success.test.ts — (NEW) `loopzero scale --json` (stub OK per CLAUDE.md)
cli/tests/integration/team-success.test.ts — (NEW) `loopzero team list --json`
cli/tests/integration/_fixtures/mock-api.ts — (NEW) shared API mock harness
```

#### Deep file dependency analysis
- 12 + 1 NEW files. Reuse `exit-code-contract.test.ts` pattern (spawnSync + tmpHome).
- Mock harness exposes `mockEndpoint(path, response)` for fixtures.

#### Deep Dives
- **Invariant**: each test cleans up tmpHome on completion.
- **Edge case — `init`**: writes `loopzero.yaml` + `.loopzero/` config; tmpHome scopes side-effects.
- **Edge case — `deploy`**: requires source bundle upload; mock HTTP server with nock + fixture.

#### Tasks
1. Build mock harness (Express or http.Server in fixture).
2. For each register: identify simplest success command + write test.
3. Wire into `npm test` default suite via Jest config.
4. Validate: each test runs in <5s.

#### TDD
```
RED:     test_auth_whoami_returns_json_with_user_field() — exit 0, parse stdout, assert user shape
RED:     test_deploy_dry_run_returns_json_with_build_id_field() — same
... (10 more)
GREEN:   Implement each test + mock harness
REFACTOR: Extract common tmpHome setup to fixture helper
VERIFY:  cd cli && npx jest tests/integration/
```

#### Acceptance Criteria
- [ ] 12 integration test files exist
- [ ] All pass in CI in <60s total
- [ ] Each asserts JSON shape per spec
- [ ] Each asserts exit code per Rule 4

#### DoD
- [ ] 1171 unit + 12 integration GREEN
- [ ] CI gate `cli-commands.yml` (NEW) runs them on every PR

---

### T3.3 — Fix all CLI bugs surfaced by T3.2

#### Objective
Any command failing T3.2 gets fixed in code, then test re-greens. This is the catch-all task that catches the "1000% functional" requirement.

#### Evidence
T3.2 will surface bugs (PR #51 changed call paths; some commands likely regress).

#### Files to edit
```
cli/src/commands/*.ts — fixes per finding
cli/src/register/*.ts — fixes per finding
api/internal/routes/*.go — if API contract drifted
```

#### Deep file dependency analysis
- Per-finding. Each bug = 1 micro-PR within the plan.

#### Deep Dives
- Heuristic: if `--json` shape mismatch, fix the emitter.
- Heuristic: if exit code wrong, fix the bin/loopzero plumbing.
- Heuristic: if API call fails, fix the API route or the CLI client.

#### Tasks
1. Run T3.2 suite, log failures.
2. For each failure: root-cause, fix, re-test.
3. Document in CHANGELOG.

#### TDD
```
RED:     each T3.2 test that fails is the RED
GREEN:   fix per finding
REFACTOR: per finding
VERIFY:  cli/tests/integration GREEN
```

#### Acceptance Criteria
- [ ] T3.2 suite 100% GREEN
- [ ] Each fix has a CHANGELOG entry

#### DoD
- [ ] CLI integration GREEN
- [ ] CLI unit 1171 still GREEN (no regressions)

---

## Phase 4: Multi-cell activation (D3)

**Objective:** Tenant tagged `cell_id='cell-2'` deploys to `runtime-2` cluster end-to-end. HTTP 200.

### T4.1 — Pulumi config for runtime-2 (cell-2)

#### Objective
`Pulumi.dev.yaml` declares `cellId: cell-2` for runtime-2; outputs `runtime2Kubeconfig`, `runtime2K8sVersion`, `runtime2NodeClass` as first-class.

#### Evidence
`infra/pulumi/src/components/infra.ts:160-165` provisions runtime-2 but Pulumi.dev.yaml lacks `cellId`. `placement/dynamic_router.go:44` requires the cell metadata to route.

#### Files to edit
```
infra/pulumi/Pulumi.dev.yaml — add cellId, runtime2K8sVersion, runtime2MinNodes, runtime2MaxNodes, runtime2NodeClass
infra/pulumi/src/index.ts — emit runtime2Kubeconfig output
infra/pulumi/src/components/infra.ts — read cell metadata from config
infra/pulumi/src/__tests__/cell-2-config.test.ts — (NEW)
```

#### Deep file dependency analysis
- `Pulumi.dev.yaml`: ~5 new keys.
- `index.ts`: emit output, downstream consumed by `cli/src/commands/platform.ts:pulumiOutputsToEnv` → LOOPZERO_RUNTIME2_KUBECONFIG_RAW.
- `infra.ts`: parameterize cell config from stack inputs.

#### Deep Dives
- **Invariant**: `pulumi stack output runtime2Kubeconfig` returns valid kubeconfig YAML.
- **Edge case — adopt-or-create**: runtime-2 may already exist on DO; ensure adopt-or-create logic (ADR-0015) handles it idempotently.

#### Tasks
1. Add config keys to Pulumi.dev.yaml.
2. Wire outputs in index.ts.
3. Read in infra.ts.
4. Write `cell-2-config.test.ts` validating renderer.

#### TDD
```
RED:     test_pulumi_dev_yaml_has_cell_2_config() — yaml.parse + assert keys
RED:     test_index_ts_emits_runtime2_outputs() — mock pulumi context, assert exports
GREEN:   Implement
REFACTOR: None
VERIFY:  cd infra/pulumi && npx vitest run src/__tests__/cell-2-config.test.ts
```

#### Acceptance Criteria
- [ ] Pulumi.dev.yaml has cellId
- [ ] runtime2Kubeconfig output declared
- [ ] Pulumi vitest GREEN

#### DoD
- [ ] Test GREEN
- [ ] `pulumi preview` shows runtime-2 plan delta minimal (idempotent)

---

### T4.2 — Database migration: `cells` table populated

#### Objective
Forward-only migration creates rows for cell-1 + cell-2 in the `cells` table (or equivalent). `dynamic_router.go` reads it.

#### Evidence
`api/internal/placement/dynamic_router.go` references cell metadata. CLAUDE.md confirms "Cell architecture Phase 0+1+1.5 complete (runtime-2 physically provisioned but not routed)".

#### Files to edit
```
api/internal/db/migrations/000003_cells_seed.up.sql — (NEW) INSERT INTO cells
api/internal/placement/dynamic_router_test.go — case for cell-2 routing
```

#### Deep file dependency analysis
- Migration is idempotent (`ON CONFLICT DO NOTHING`).
- Test: given tenant with cell_id='cell-2', router selects cell-2 cluster.

#### Deep Dives
- **Invariant**: migration is forward-only (Rule per CLAUDE.md).
- **Edge case — null cell_id**: tenants without cell pinning go to cell-1 (default).

#### Tasks
1. Write migration.
2. Write router test for cell-2 case.
3. Apply migration in CI (already automated).

#### TDD
```
RED:     test_router_routes_cell_2_tenant_to_runtime2() — mock cell store, assert clusterURL = runtime-2
GREEN:   Migration + router (already implemented)
REFACTOR: None
VERIFY:  go test ./api/internal/placement/...
```

#### Acceptance Criteria
- [ ] Migration applied in CI
- [ ] Router test GREEN
- [ ] No DROP in migration

#### DoD
- [ ] Migration committed
- [ ] Router covers cell-2

---

### T4.3 — `loopzero deploy --cell cell-2` CLI flag

#### Objective
Add `--cell <cell-id>` to `loopzero deploy` (and create-tenant flow) so an operator can pin a deployment to a specific cell. Exits with usage error if cell not in DB.

#### Evidence
Operators need a way to actually exercise cell-2 routing in dogfood.

#### Files to edit
```
cli/src/register/deploy.ts — add --cell option
cli/src/commands/deploy.ts — pass cellId in payload
api/internal/routes/builds.go — accept cell_id field
cli/tests/integration/deploy-cell-2.test.ts — (NEW)
```

#### Deep file dependency analysis
- CLI: thin pass-through.
- API: validates cell exists in cells table.
- Test: integration spawns CLI with --cell, mock API asserts payload.

#### Deep Dives
- **Invariant**: `--cell` invalid → exit 1 with LoopZeroError code `cell_not_found`.
- **Edge case — backward compat**: omitting `--cell` keeps existing behaviour (default cell-1).

#### Tasks
1. Wire option through Commander.
2. Pass through CLI command.
3. Validate in API.
4. Test integration.

#### TDD
```
RED:     test_deploy_with_cell_flag_includes_cell_id_in_payload()
RED:     test_deploy_with_unknown_cell_exits_1()
GREEN:   Implement
REFACTOR: None
VERIFY:  cd cli && npx jest tests/integration/deploy-cell-2.test.ts
```

#### Acceptance Criteria
- [ ] --cell flag accepted
- [ ] Invalid cell rejected
- [ ] Default cell unchanged behaviour
- [ ] JSON payload includes cell_id

#### DoD
- [ ] CLI test GREEN
- [ ] API test GREEN

---

### T4.4 — End-to-end cell-2 smoke test

#### Objective
`infra/tests/parity/cell2-smoke.sh` (NEW): provision a test tenant pinned to cell-2 via CLI, deploy a sample app, curl the resulting URL, assert HTTP 200.

#### Evidence
T4.1+T4.2+T4.3 individually tested; this is the end-to-end gate.

#### Files to edit
```
infra/tests/parity/cell2-smoke.sh — (NEW)
docs/runbooks/multi-cell-activation.md — (NEW)
docs/decisions/ADR-0023-cell-2-activation.md — (NEW)
```

#### Deep file dependency analysis
- Smoke script: idempotent, tears down test tenant on EXIT.
- Runbook: operator guide.
- ADR: rationale + activation playbook.

#### Deep Dives
- **Invariant**: HTTP 200 from runtime-2 nodes only (verify via `kubectl describe pod` showing the runtime-2 cluster context).
- **Edge case — DNS**: tenant URL must resolve via the same Cloudflare/Traefik chain as cell-1.

#### Tasks
1. Write smoke script (~150 lines).
2. Run live (cloud cost ~$0.50, requires runtime-2 already provisioned).
3. Document Achados in matrix.
4. Write ADR-0023 + runbook.

#### TDD
```
RED:     test_cell2_smoke_returns_http_200_from_runtime2() — script exit 0
GREEN:   Implement script, run live, fix Achados
REFACTOR: None
VERIFY:  bash infra/tests/parity/cell2-smoke.sh
```

#### Acceptance Criteria
- [ ] Smoke exits 0
- [ ] HTTP 200 from cell-2 tenant URL
- [ ] Pod confirmed in runtime-2 (kubectl context check)
- [ ] Test tenant cleaned up

#### DoD
- [ ] Smoke GREEN
- [ ] ADR-0023 committed
- [ ] Runbook committed
- [ ] Matrix updated

---

## Phase 5: CI gates

**Objective:** Make every check from Phases 0-4 a permanent CI gate.

### T5.1 — `cli-commands.yml` workflow

#### Objective
PR gate that runs `cli/tests/integration/` on every push.

#### Files to edit
```
.github/workflows/cli-commands.yml — (NEW)
```

#### Deep file dependency analysis
- Mirrors `parity-validation.yml` structure: ubuntu-latest, node 20, npm ci, npx jest tests/integration.

#### Deep Dives
- **Edge case — secrets**: integration tests use mock API, no real creds needed.

#### Tasks
1. Write workflow.
2. Trigger on PR/push.
3. Validate it runs in <2min.

#### TDD
```
RED:     workflow yaml validates against GH schema
GREEN:   Write workflow
REFACTOR: None
VERIFY:  yamllint .github/workflows/cli-commands.yml + push to PR branch, observe CI
```

#### Acceptance Criteria
- [ ] Workflow runs on every PR
- [ ] <2min total
- [ ] Required check on develop branch protection

#### DoD
- [ ] Live PR run GREEN
- [ ] Branch protection updated

---

### T5.2 — `multi-cell.yml` workflow

#### Objective
PR gate that runs the cell-2 smoke when relevant files change.

#### Files to edit
```
.github/workflows/multi-cell.yml — (NEW)
```

#### Deep file dependency analysis
- Path filters: `api/internal/placement/`, `infra/pulumi/Pulumi.dev.yaml`, `infra/tests/parity/cell2-smoke.sh`.

#### Deep Dives
- **Edge case — cloud cost**: cell-2 smoke costs $. Workflow runs on `workflow_dispatch` for cloud, on `pull_request` for offline preflight (`--dry-run`).

#### Tasks
1. Write workflow with two jobs (preflight always; live on dispatch).
2. Document gate in runbook.

#### TDD
```
RED:     workflow yaml validates
GREEN:   Write workflow
REFACTOR: None
VERIFY:  yamllint
```

#### Acceptance Criteria
- [ ] Preflight runs on every PR touching multi-cell paths
- [ ] Live runs on workflow_dispatch only

#### DoD
- [ ] Live dispatch GREEN once

---

### T5.3 — Extend parity preflight to include T2.2 + T4.x

#### Objective
`infra/tests/parity/preflight.sh` adds gates for: only one Pulumi project, cell-2 config valid (offline check).

#### Files to edit
```
infra/tests/parity/preflight.sh — add 2 gates
```

#### Deep file dependency analysis
- Existing 12-gate script grows to 14.

#### Deep Dives
- Gates remain offline.

#### Tasks
1. Add `find infra -name 'Pulumi.yaml' | wc -l == 1` gate.
2. Add `yq '.config | has("loopzero-infra:cellId")' Pulumi.dev.yaml` gate.
3. Re-run preflight.

#### TDD
```
RED:     test_preflight_has_pulumi_singleton_gate() — grep preflight.sh for the check
RED:     test_preflight_has_cell_id_gate()
GREEN:   Add gates
REFACTOR: None
VERIFY:  bash infra/tests/parity/preflight.sh
```

#### Acceptance Criteria
- [ ] 14 PASS gates
- [ ] Both new gates fail-loud when violated (manually verified)

#### DoD
- [ ] Preflight GREEN

---

## Phase 6: Validation matrix

**Objective:** End-to-end gate covering Phases 0-5.

### T6.1 — Master validation script

#### Objective
`infra/tests/parity/validate-all.sh` (NEW): runs every offline gate in sequence, exit 0 only if all pass.

#### Files to edit
```
infra/tests/parity/validate-all.sh — (NEW)
Taskfile.yml — task validate:all target
```

#### Deep file dependency analysis
- Wraps existing scripts.

#### Deep Dives
- **Invariant**: zero false positives. Each gate is deterministic offline.

#### Tasks
1. Aggregate scripts.
2. Add task target.
3. Run end-to-end.

#### TDD
```
RED:     test_validate_all_passes_post_phase_5() — script exit 0
GREEN:   Wrap + run
REFACTOR: None
VERIFY:  task validate:all
```

#### Acceptance Criteria
- [ ] All gates GREEN
- [ ] <60s total runtime

#### DoD
- [ ] task validate:all GREEN

---

### T6.2 — Run matrix audit + golden test post-Phase 5

#### Objective
Confirm `audit-regression-matrix.sh` and `audit-golden-test.sh` still PASS after every Phase change.

#### Files to edit
```
docs/runbooks/bootstrap-regression-matrix.md — entries for Achados from Phases 1-4
```

#### Deep file dependency analysis
- Matrix entries.

#### Deep Dives
- Each Phase that surfaces an Achado adds entries.

#### Tasks
1. Re-run audit per Phase change.
2. Add matrix rows.
3. Confirm 0 MISS critical.

#### TDD
```
RED:     test_matrix_audit_zero_critical_miss() — script exit 0
GREEN:   Update matrix
REFACTOR: None
VERIFY:  bash infra/tests/parity/audit-regression-matrix.sh
```

#### Acceptance Criteria
- [ ] Matrix audit PASS
- [ ] Golden test PASS

#### DoD
- [ ] Both GREEN

---

## Phase 7: Documentation + close

**Objective:** ADRs ratified, runbooks updated, CHANGELOG closed, memory entry written.

### T7.1 — ADRs + runbooks + CHANGELOG

#### Objective
Land all docs.

#### Files to edit
```
docs/decisions/ADR-0021-pulumi-single-stack.md — (NEW, written in T2.1)
docs/decisions/ADR-0022-split-operator-helmfile.md — (NEW)
docs/decisions/ADR-0023-cell-2-activation.md — (NEW, written in T4.4)
docs/runbooks/multi-cell-activation.md — (NEW)
docs/runbooks/parity-validation.md — extend with cell-2 section
docs/runbooks/bootstrap-regression-matrix.md — finalize
CHANGELOG.md — Added/Changed/Removed sections for this plan
~/.claude/projects/-home-paulo-loopzero/memory/reference_infra_normalize_2026_05_XX.md — (NEW)
~/.claude/projects/-home-paulo-loopzero/memory/MEMORY.md — index entry
```

#### Deep file dependency analysis
- Pure docs; no code.

#### Deep Dives
- Each ADR follows ADR-0019/0020 format.
- CHANGELOG follows Keep a Changelog (Rule 6).

#### Tasks
1. Write each doc.
2. Cross-link.
3. Verify ADRs referenced from plan.
4. Update memory.

#### TDD
```
RED:     test_each_adr_referenced_in_plan() — grep cross-links
GREEN:   Write docs
REFACTOR: None
VERIFY:  manual cross-check
```

#### Acceptance Criteria
- [ ] 3 new ADRs
- [ ] 1 new runbook
- [ ] Existing runbooks extended
- [ ] CHANGELOG closed
- [ ] Memory updated

#### DoD
- [ ] All committed
- [ ] PR description references all ADRs

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Tilt deploys 4 split operators, helmfile deploys monolith (Rule 10 break) | T1.1, T1.2, T1.3 | Helmfile owns topology; Tilt consumes helmfile output |
| 2 | 4 Pulumi stacks, 3 orphans with @pulumi/kubernetes | T2.1, T2.2 | Delete + permanent gate |
| 3 | 3 orphan Tilt yamls | T1.2 | Deleted as part of Tiltfile refactor |
| 4 | regression-checks.sh profile-aware undocumented | T7.1 | Runbook entry |
| 5 | regression_test.go string-literal anti-pattern | DEBT (D6) | Registered in matrix as #136, deferred |
| 6 | infra/cell-templates/v1.0.0 dead-code | T4.4 | Activated by cell-2 work; if templates unused, deleted |
| 7 | charts/loopzero-platform requires helm dependency build | T6.1 | task validate:helm-deps added in master validate-all |
| 8 | Sloth files bypass kubeconform | T6.1 | task validate:slos calls sloth validate |
| 9 | refactorhelms branch merge needs ratification | T0.1 | Fast-forward develop |
| 10 | All CLI commands 1000% functional | T3.1, T3.2, T3.3 | Audit + 12 integration tests + bug fixes |
| 11 | Multi-cell not active (runtime-2 idle) | T4.1, T4.2, T4.3, T4.4 | Pulumi config + DB seed + CLI flag + smoke |
| 12 | No CI gate for CLI integration | T5.1 | New cli-commands.yml workflow |
| 13 | No CI gate for multi-cell | T5.2 | New multi-cell.yml workflow |
| 14 | Parity preflight missing 2 gates (Pulumi singleton, cell config) | T5.3 | Extended preflight |
| 15 | No master validation script | T6.1 | validate-all.sh + task validate:all |
| 16 | Matrix could regress on Phase changes | T6.2 | Run audit per Phase |
| 17 | ADRs missing for new structural decisions | T7.1 | ADR-0021/0022/0023 |
| 18 | CHANGELOG not closed | T7.1 | Keep a Changelog entries |
| 19 | Memory not updated | T7.1 | New project memory entry |

**Coverage: 19/19 gaps covered (100%)**, with #5 explicitly registered as DEBT per ADR D6.

## Global Definition of Done

- [ ] All phases (0-7) completed
- [ ] All tests GREEN simultaneously:
  - [ ] CLI unit (1171) GREEN
  - [ ] CLI integration (12 NEW) GREEN
  - [ ] Pulumi vitest (96 + 1 = 97) GREEN
  - [ ] Go infra/tests (capacity + chaos + e2e + load + unit) GREEN
  - [ ] Parity preflight (12 → 14 gates) GREEN
  - [ ] Matrix audit zero critical MISS
  - [ ] Audit golden test 3/3 PASS
  - [ ] task validate:manifests GREEN
  - [ ] task validate:helm-deps GREEN (NEW)
  - [ ] task validate:slos GREEN (NEW)
  - [ ] task validate:all GREEN (NEW)
- [ ] Live gates GREEN:
  - [ ] task parity:k3d-virgin GREEN with splitOperators=true
  - [ ] cell2-smoke.sh exits 0 (HTTP 200 from runtime-2)
- [ ] Zero @pulumi/kubernetes imports in codebase (audited by sprint8-fixes.test.ts Fix #7 + Fix #10)
- [ ] Zero orphan files in infra/tilt/ (audited by find script)
- [ ] One Pulumi project (audited by sprint8-fixes.test.ts Fix #10)
- [ ] All CLI commands documented in cli-commands-audit.md
- [ ] All ADRs (0021, 0022, 0023) committed and cross-linked
- [ ] CHANGELOG closed with Added/Changed/Removed sections for this plan
- [ ] Memory entry persisted
- [ ] CI workflows: parity-validation.yml + cli-commands.yml (NEW) + multi-cell.yml (NEW) all required on develop branch protection
- [ ] No regression in 1171 CLI unit tests
- [ ] No regression in 96 Pulumi vitest
- [ ] code-audit checks passing (cyclomatic <= 10, files <= 500 lines, lint zero warnings, coverage >= 90% on changed files)
- [ ] Plan-specific: Tilt and Helmfile deploy the SAME pod set (verified by `diff <(tilt-pods) <(helmfile-rendered-pods)`)
- [ ] Plan-specific: tenant pinned to cell-2 reachable on cell-2 cluster (kubectl context confirms)
