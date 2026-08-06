---
type: baseline
title: "Steady-State Baseline — 2026-06-10 J-22 Layer 2 Code-Review Fixes (pre-fix)"
---

# Steady-State Baseline — 2026-06-10 J-22 Layer 2 Code-Review Fixes (pre-fix)

> Snapshot of the system state BEFORE applying the 7-phase plan
> `2026-06-10-j22-layer2-code-review-fixes` v1.1. Bisect anchor for the
> downstream regression-check phase (Phase 6).
>
> Per `.claude/rules/steady-state-baseline.md` § Triggers — "Pre-validation
> (antes de `/cleanroom` / `/dogfood full` / `/chaos-test`)" + planned
> code mutations across 6 files.

**Date:** 2026-06-10
**Plan slug:** `2026-06-10-j22-layer2-code-review-fixes` v1.1
**Plan verdict:** `SHIPPABLE_WITH_CAVEATS` (final score 89.0 via `/plan-confidence`)
**Linked artifacts:** [`plan v1.1`](../../.claude/knowledge-base/plans/2026-06-10-j22-layer2-code-review-fixes-plan.md) · [`edge-case review`](../../.claude/knowledge-base/reviews/2026-06-10-j22-layer2-code-review-fixes-edge-cases-2026-06-10.md) · [`deps audit`](../../.claude/knowledge-base/audits/2026-06-10-j22-layer2-code-review-fixes-deps-audit-2026-06-10.md) · [`loop-code-review final report`](../../code-review-output/final_report.md) · upstream baseline [`2026-06-10-j22-layer2-eso-sync-live-green.md`](./2026-06-10-j22-layer2-eso-sync-live-green.md)

---

## Layer 1 — Code state

```
$ git rev-parse HEAD
07e886df8 [test(j22): align transit_client_test.go with canonical hyphen-prefix path]
$ git branch --show-current
worktree-glowing-growing-babbage
$ git log --oneline -5
07e886df test(j22): align transit_client_test.go with canonical hyphen-prefix path
a5fe7797 fix(j22): escape Go-template tokens in CSS YAML comments
572911de feat(j22): Layer 2 ESO sync LIVE GREEN end-to-end + 9 LIVE-caught fixes
097e9c50 feat(helmfile): add external-secrets chart to mgmt cluster (Option B B1)
ee7487e6 docs(rules): codify INQUEBRÁVEL Cleanroom + UP + LIVE TEST mandate
$ git status --porcelain | wc -l
7 (untracked artifacts only: plan v1.1, edge-case, deps-audit, code-review-output/, impl progress json, halt-loop prompt, openbao implementation review)
```

Working tree is clean of production code modifications — all 7 untracked entries are review/plan artifacts produced during the cycle-plan chain.

## Layer 2 — Runtime state (snapshot at HEAD=07e886df)

Per CLAUDE.md and the upstream LIVE-GREEN baseline:

| Cluster | Helm releases (selected) | State |
|---|---|---|
| `k3d-loopzero-mgmt` | loopzero-api, loopzero-cloud, openbao, external-secrets, ory-kratos, ory-hydra | LIVE-GREEN |
| `k3d-loopzero-build` | argo-workflows, gitea | OPERATIONAL |
| `k3d-loopzero-workload` | external-secrets, traefik, KEDA | LIVE-GREEN (ESO `loopzero-broker` CSS Ready) |

Validated end-to-end on 2026-06-10 walkthrough (see [`2026-06-10-j22-layer2-eso-sync-live-green.md`](./2026-06-10-j22-layer2-eso-sync-live-green.md) Layer 2 § "Materialized state"):

- ExternalSecret CRs `es--teste` + `es--testedddd` `SecretSynced=True` in workload tenant ns `loopzero-tn-0ca75f9f32a0193d-teste-production`
- K8s Secret materialized with plaintext matching sponsor input
- `loopzero_broker_endpoint_round_trip_ms` ~20ms average
- 28 audit_log entries by `broker:eso-webhook` actor

## Layer 3 — Config state (key surfaces this plan touches)

| Surface | Current state (pre-fix) |
|---|---|
| `api/cmd/setup_secret_fetch.go` brokerBearerMiddleware | trusts caller-supplied `X-Cell-ID` header (#13 consensus-CRITICAL forgery vector); falls back to magic literal `"cell-default"` (#13 sub-finding) |
| `api/cmd/setup_secret_fetch.go` rawSecretReader | queries `WHERE tenant_id = $1 AND name = $2 LIMIT 1` (#15 scope-drop — drops project/env/app composite key) |
| `api/cmd/setup_secret_fetch.go` audit-writer adapter | `_ = AuditWriter.WriteAudit(...)` silently discards error on cross_tenant_attempt (#16) |
| `api/internal/k8s/secret_k8s_manager.go` namespace formula | `fmt.Sprintf("loopzero-%s-%s-%s", ...)` hardcoded twice instead of `pkg/naming.Namespace` (M2 DRY MEDIUM) |
| `api/cmd/setup_secret_fetch.go` pgx error matching | `strings.Contains(err.Error(), "no rows")` fragile (MEDIUM) |
| `infra/helm/loopzero-external-secrets/values.yaml` broker.apiURL | default `http://...` without `helm fail` guard on plaintext bearer (#23) + CSS URL drift from LIVE NodePort (#27) |
| `api/cmd/setup_secret_fetch_test.go` | DOES NOT EXIST — 171 LOC NEW security-sensitive file has zero unit tests (#113) |
| `api/internal/k8s/tenant_cr_manager_test.go` | 5 RED tests missing `spec.environments[].project` required field (introduced by 2026-06-09 M2 fix) |
| `api/internal/routes/projects_test.go` | 1 RED test missing same field |

Schema migrations at HEAD: PG `schema_migrations.version = 30` (or current — verify post-fix this is unchanged; plan adds NO migrations).

## Layer 4 — LIVE functional probes (SLO floor)

Per [`2026-06-10-j22-layer2-eso-sync-live-green.md`](./2026-06-10-j22-layer2-eso-sync-live-green.md) Layer 4:

- ✅ `GET /api/v1/secrets` returns 200 with sponsor-created TESTE + TESTEDDDD
- ✅ POST `/internal/v1/secret-fetch/{tenant_id}/{secret_name}` returns 200 + plaintext_b64 = sponsor input
- ✅ ExternalSecret CR status `SecretSynced True` propagates K8s Secret to tenant ns
- ✅ ESO broker round-trip p95 ≤ 20ms
- ✅ vault:v1: ciphertext at rest in PG `secrets.encrypted_value` (zero plaintext leak per D5 invariant)
- ✅ `go test ./pkg/vault/... -race` 18/18 PASS (transit_client_test.go GREEN per `07e886df`)
- ⚠️ `go test ./api/internal/k8s/... ./api/internal/routes/...` has 6 pre-existing RED tests (root cause: 2026-06-09 M2 fixture omission — addressed by this plan's Phase 5)

`/loop-code-review` final report at `code-review-output/final_report.md`:
- **74 findings** total
- 0 CRITICAL OPEN (consensus-level) — #1 + #66 resolved mid-review in `a5fe7797` + `07e886df`
- 1 consensus-CRITICAL **#13** open (X-Cell-ID forgery) — Phase 1 of this plan closes
- 5 HIGH open (#15/#16/#23/#27/#113) — Phase 2 + Phase 3 close
- 11 MEDIUM (M2 DRY + k8sName shadow + IsConflict retry + label-omit + pgx err) — Phase 4 closes

## Promised post-fix state (Phase 6 verification target)

The Phase 6 Integration Validation MUST observe the following deltas:

| Probe | Pre-fix (this baseline) | Post-fix (Phase 6 target) |
|---|---|---|
| `brokerBearerMiddleware` ignores client X-Cell-ID | ❌ trusts header | ✅ server-side `placement.DefaultCellID` const |
| `rawSecretReader` SQL query | scopes by `(tenant_id, name)` only | scopes by `(tenant_id, project, environment, app, name)` |
| `WriteAudit` error path | silently discarded | logged via `slog.Error` + `loopzero_secret_audit_write_failed_total` counter increment |
| `helm template ... --set envName=prod --set broker.apiURL=http://x` | exit 0 | exit 1 with `fail: insecure apiURL` |
| `helm template ... --set broker.apiURL=http://x` (no envName) | exit 0 | exit 1 (EC-6 absorption — fail-CLOSED on unknown envName) |
| `setup_secret_fetch_test.go` | DOES NOT EXIST | 10+ named tests covering 4 components, race-clean |
| `tenant_cr_manager_test.go` + `projects_test.go` | 6 RED | 6 GREEN |
| LIVE walkthrough | sponsor stuck on #13 forgery latent risk | sponsor verbatim "FUNCIONOU CANONICAL!" + zero regression on TESTE/TESTEDDDD CRs |
| `/loop-code-review --diff-base 07e886df` re-run | 1 CRITICAL + 5 HIGH | **0 CRITICAL / 0 HIGH on NEW code introduced by this plan** (per EC-9 absorption) |

## Bisect anchor invariants (DO NOT regress)

These functional behaviors are LIVE-validated at `07e886df` and MUST NOT regress through this plan's implementation:

1. ExternalSecret CR `loopzero-broker` ClusterSecretStore resolves and propagates K8s Secret in workload tenant ns
2. PG `secrets.encrypted_value` stays vault:v1: ciphertext (zero plaintext leak)
3. loopzero-api `/api/v1/secrets` POST → ESO sync chain end-to-end ~20ms p95
4. cleanroom-soft from upstream baseline (4-layer config drift gates GREEN per `task quality:gates`)

Per EC-1 absorption: post-Phase 2 commit, the 2 pre-existing LIVE ExternalSecret CRs (`es--teste` + `es--testedddd`) will be deleted from workload tenant ns BEFORE sponsor walkthrough re-validation, so they get re-emitted with the canonical 4-segment URL shape. This is deliberate migration, NOT a data-loss event (PG `secrets` table is canonical).

---

**Status:** ✅ CAPTURED — pre-fix bisect anchor for the 12-task (T0.1..T6.1) plan delivery.
