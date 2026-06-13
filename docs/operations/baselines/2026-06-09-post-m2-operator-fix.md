# Steady-state baseline — 2026-06-09 post M2 operator canonical fix

> 4-layer baseline per `.claude/rules/steady-state-baseline.md` captured at
> close of `/implement` halt-loop for plan
> `.claude/knowledge-base/plans/2026-06-09-m2-operator-canonical-fix-plan.md` v1.4
> (verdict SHIPPABLE_WITH_CAVEATS, final score 89.0; TDD shape gate 9/9 PASS).

## Why this baseline

This baseline is the post-fix bisect anchor for the M2 operator canonical fix
delivery. The plan shipped 9 tasks (T0.1, T5.1, T1.1, T2.1, T2.2, T3.1, T4.1,
T6.1, T7.1) closing the M2 operator drift identified LIVE during T6.1 testing
of plan `2026-06-08-pre-release-test-rigor` and documented in
`feedback_m2_canonical_drift_operator_renderer` memory: the manifest renderer
was already emitting the M2 canonical 3-segment namespace
`loopzero-<tenant>-<project>-<env>` while TenantController + EnvironmentController
were still emitting the M1 2-segment `loopzero-<tenant>-<env>`. Production deploys
cascaded `AppProject reject destination → namespace not found → SA not found
→ pods Forbidden`, requiring 4 manual operator workarounds per deploy.

Post-fix, the operator emits M2 canonical end-to-end:

- TenantController.reconcileArgoCDApp emits AppProject destinations from the
  per-project (project, env) Cartesian product (T2.1 + T2.2).
- EnvironmentController.reconcileNamespace computes canonical M2 namespace
  from Spec.Project + Spec.Environment with EC-3 63-char overflow guard (T3.1).
- POST /api/v1/projects auto-creates production env via EnvironmentEnsurer
  (T4.1, ADR D4 Vercel-style).
- Migration tool migrate-legacy-namespaces.sh provides dual-mode safe cleanup
  with EC-6 PVC safety gate + EC-7 active-release precondition (T5.1).
- LIVE E2E test dev-public-production-deploy_test.sh asserts the full chain
  with ZERO manual workarounds (T6.1).

The prior milestone baseline is
`docs/operations/baselines/2026-06-09-pre-m2-operator-fix.md`. Any
regression that surfaces post-merge is diff-bounded between these two
snapshots.

---

## Layer 1 — Code state

```
commit:               ba8e90db (test(e2e): T6.1 — dev-public-production-deploy LIVE chain + hermetic structural)
branch:               worktree-glowing-growing-babbage
relation:             8 task commits ahead of pre-fix baseline (088158f9..ba8e90db)
prior baseline:       docs/operations/baselines/2026-06-09-pre-m2-operator-fix.md
working tree:         pending T7.1 commit (this baseline)
plan slug:            2026-06-09-m2-operator-canonical-fix
plan version:         v1.4 (SHIPPABLE_WITH_CAVEATS 89.0)
```

Commit sequence (from oldest to newest):

```
088158f9 feat(baseline):  T0.1 — pre M2 operator canonical fix steady-state baseline
7c18a8a5 feat(migration): T5.1 — dual-mode migrate-legacy-namespaces.sh with EC-6 PVC safety + EC-7 active-release gate
566613ee feat(crd):       T1.1 — TenantEnvironmentEntry.Project required + RFC 1123 Pattern at CRD level
1afce5b7 refactor(operator): T2.1 — canonicalDestNamespace fail-loud when Project empty
c36a7ad9 refactor(operator): T2.2 — extract buildAppProjectDestNamespaces + fail-loud per-project Cartesian
1a014d11 feat(operator):  T3.1 — EnvironmentController computes M2 canonical namespace + EC-3 NameTooLong
5d47a7ca feat(api):       T4.1 — POST /api/v1/projects auto-creates production env (ADR D4 Vercel-style)
ba8e90db test(e2e):       T6.1 — dev-public-production-deploy LIVE chain + hermetic structural
```

Files in scope (per plan § Baseline Context + Files to edit):

```
operators/api/v1/tenant_types.go                       (T1.1 — TenantEnvironmentEntry.Project required + Pattern)
operators/api/v1/environment_types.go                  (T3.1 — EnvironmentCRSpec.Project field added)
operators/api/v1/zz_generated.deepcopy.go              (regenerated)
operators/config/crd/bases/loopzero.io_tenants.yaml        (T1.1 — Pattern + minLength + required)
operators/config/crd/bases/loopzero.io_environments.yaml   (T3.1 — Project schema)
operators/internal/tenant/tenant_gitops.go             (T2.1 + T2.2 — fail-loud + buildAppProjectDestNamespaces)
operators/internal/tenant/tenant_gitops_test.go        (T2.1 + T2.2 — table + RED tests)
operators/internal/tenant/tenant_controller.go         (T3.1 — propagate Project to env CR)
operators/internal/tenant/tenant_controller_test.go    (T1.1 fixtures updated)
operators/internal/tenant/environment_controller.go    (T3.1 — canonical M2 + NameTooLong)
operators/internal/tenant/environment_controller_t31_test.go  (NEW — T3.1 envtest specs)
operators/internal/tenant/environment_crd_validation_test.go  (NEW — T1.1 envtest + schema contract)
operators/internal/tenant/tenant_gitops_test.go        (fixtures updated)
operators/internal/tenant/resource_quotas_test.go      (T1.1 fixtures updated)
api/internal/routes/projects.go                        (T4.1 — envEnsurer wiring)
api/internal/routes/projects_t41_test.go               (NEW — T4.1 RED tests)
api/internal/server/server.go                          (T4.1 — Config.ProjectEnvEnsurer)
api/internal/server/router.go                          (T4.1 — nil-safe wire)
api/cmd/main.go                                        (T4.1 — production wire-up)
infra/scripts/local/migrate-legacy-namespaces.sh       (T5.1 — dual-mode + safety gates)
infra/scripts/local/preflight-t1.1-no-legacy-envs.sh   (NEW — T1.1 ADR D5 gate)
infra/tests/check-migrate-legacy-namespaces_test.sh    (NEW — T5.1 hermetic 26/26)
infra/tests/e2e/dev-public-production-deploy_test.sh   (NEW — T6.1 LIVE 13-step)
infra/tests/check-dev-public-production-deploy-syntax_test.sh (NEW — T6.1 hermetic 30/30)
docs/operations/pv-rebind-runbook.md                   (NEW — T5.1 EC-6 deliverable)
docs/operations/baselines/2026-06-09-pre-m2-operator-fix.md   (T0.1 — bisect anchor pre)
docs/operations/baselines/2026-06-09-post-m2-operator-fix.md  (T7.1 — bisect anchor post; THIS file)
.claude/rules/shell-scripts-allowlist.md               (4 entries added)
```

---

## Layer 2 — Runtime state (M2 invariants restored)

Post-fix the operator emits canonical M2 end-to-end. The 4 LIVE workarounds
documented in `feedback_m2_canonical_drift_operator_renderer` are now
ELIMINATED BY CONSTRUCTION:

| Pre-fix workaround | Post-fix invariant | Where enforced |
|---|---|---|
| (a) `kubectl patch appproject ... destinations` to add M2 ns | TenantController emits per-project Cartesian destinations | T2.2 — `buildAppProjectDestNamespaces` helper |
| (b) `kubectl create namespace loopzero-<tenant>-<project>-<env>` | EnvironmentController computes canonical from Spec.Project | T3.1 — `environment_controller.go:163-202` |
| (c) `kubectl get sa loopzero-app ... \| apply -f -` cross-namespace | EnvironmentController.reconcileRBAC populates SA in canonical ns | T3.1 — existing reconcile sub-steps consume canonical `nsName` |
| (d) `kubectl patch app .../sync revision force` | ArgoCD AppProject destinations match by construction; sync needs no force | T2.2 + T3.1 chain |

T6.1's hermetic test enforces these invariants via 4 anti-workaround grep
guards. Any future PR that reintroduces one of the workarounds breaks the
T6.1 test on CI — canonical-by-construction guarantee with regression
protection.

---

## Layer 3 — Config state

```
schema_migrations:          unchanged (no new migrations for M2 operator fix)
operator CRDs:              tenants.loopzero.io v1 (project required + Pattern at TenantEnvironmentEntry)
                            environments.loopzero.io v1 (Project field added to EnvironmentCRSpec)
plan-confidence verdict:    SHIPPABLE_WITH_CAVEATS 89.0 (unchanged from pre-fix; T1.1+T3.1 keep verdict at >= 70)
deps audit verdict:         PASS_WITH_CAVEATS cap 89 (unchanged; 4 informational CVEs in deps NOT touched)
TDD shape gate:             9/9 PASS (unchanged; canonical RED-test shape in every task)
governance scope:           extended 2026-06-09 (sponsor verbatim "Eu estou permitindo alterar a governanci agora somente agora")
                            to admit T3.1 operator/api/v1/environment_types.go + 23 downstream files
```

---

## Layer 4 — LIVE functional probes (post-fix expected behavior)

These probes are the post-fix SLO floor. Operator session against
cleanroom-fresh dev-public must reach all PASS:

```
- GET /engine/api/v1/projects/teste                                  [POST-FIX: PASS expected]
- POST github webhook (canonical fixture push)                        [POST-FIX: PASS expected]
  → workflow created within 5s
- Release reaches ACTIVE phase                                        [POST-FIX: PASS expected — no manual sync needed]
- Namespace `loopzero-<tenant>-teste-production` (M2) exists              [POST-FIX: PASS expected — T3.1 EnvController]
- AppProject `<tenant>` destinations include M2 ns                    [POST-FIX: PASS expected — T2.2 Cartesian]
- ServiceAccount loopzero-app in M2 ns                                    [POST-FIX: PASS expected — T3.1 reconcileRBAC]
- Pods Running 2/2 in M2 namespace                                    [POST-FIX: PASS expected]
- ArgoCD Application <tenant>-production Synced+Healthy               [POST-FIX: PASS expected]
- HTTPS GET https://<project>.<entryApp>.<tenant>.loopzero.example → 200   [POST-FIX: PASS expected]
- Preview deploy chain (PR opened → preview env URL 200)              [POST-FIX: PASS expected — PreviewController was already M2-correct]

T6.1 LIVE wall-clock budget: < 20 min total (per plan Goal + cross-reference
to T6.1 of plan 2026-06-08-pre-release-test-rigor budget).

The 4 anti-workaround grep guards in `infra/tests/e2e/dev-public-production-deploy_test.sh`
fail-loud on any regression that reintroduces manual operator intervention.

Engineering-side validation (this T7.1 commit):
- `go test operators/internal/tenant/ -run "TestBuildAppProjectDestNamespaces|TestCanonicalDestNamespace*|TestProjectFieldHasRFC1123"` PASS
- `go test api/internal/routes/ -run TestProjectCreate` PASS
- `bash infra/tests/check-migrate-legacy-namespaces_test.sh` 26/26 PASS
- `bash infra/tests/check-dev-public-production-deploy-syntax_test.sh` 30/30 PASS

LIVE E2E (operator HARD STOP, NOT executed in engineering halt-loop):
- `bash infra/tests/e2e/dev-public-production-deploy_test.sh` — operator runs against cleanroom-fresh dev-public
- Sponsor browser walkthrough — closes the plan promise
```

---

## Known follow-ups (NOT blocking the M2 operator fix delivery)

Tracked in
`.claude/knowledge-base/implementations/.progress-2026-06-09-m2-operator-canonical-fix.json
§ known_followups`:

| ID | Scope | Resolution |
|---|---|---|
| F1 | 10 tenant_integration_test M1 expectations expecting `loopzero-<tenant>-<env>` | Update specs to expect M2 `loopzero-<tenant>-<project>-<env>` post-T3.1 alignment. Pure test-fixture sweep; not a code regression. |
| F2 | EC-5 strict PG-tx-first/K8s-after rollback on POST /api/v1/projects | Requires ProjectStore API surgery (expose BeginTx wrapper for project+environments insert pair). Current handler returns 503 on K8s failure but leaves project row committed. Functional contract preserved; rollback path is deferred. |
| F3 | Operator-run prod-identical drill GREEN | Sprint 9 gate (a) — separate plan. |
| F4 | 1 prod customer day-trial | Sprint 9 gate (b) — separate plan. |
| F5 | LIVE E2E + sponsor browser walkthrough | T7.1 acceptance criterion 7 — operator session against cleanroom-fresh dev-public + sponsor sign-off. |

---

## Cross-links

- Plan: `.claude/knowledge-base/plans/2026-06-09-m2-operator-canonical-fix-plan.md` (v1.4 SHIPPABLE_WITH_CAVEATS 89.0)
- Pre-fix baseline: `docs/operations/baselines/2026-06-09-pre-m2-operator-fix.md`
- Edge-case review: `.claude/knowledge-base/reviews/2026-06-09-m2-operator-canonical-fix-edge-cases-2026-06-09.md`
- Deps audit: `.claude/knowledge-base/audits/2026-06-09-m2-operator-canonical-fix-deps-audit-2026-06-09-v1.2.md`
- Implementation contract: `.claude/knowledge-base/implementations/2026-06-09-m2-operator-canonical-fix-implementation.md`
- Memory (root cause documentation): `feedback_m2_canonical_drift_operator_renderer`
- Memory (sponsor scope-extension pattern): `feedback_sponsor_scope_extension_pattern`
- Memory (LIVE evidence): `reference_m2_runtime_envs_per_project_session_2026_06_07`

This baseline closes the engineering side of the M2 operator canonical fix.
The remaining HARD STOPs (LIVE E2E + sponsor walkthrough) are operator-side
gates per the plan acceptance criteria — `IMPLEMENTATION_COMPLETE` is
honored ONLY after those gates clear.
