---
type: implementation
title: "T6.1 LIVE — engineering side advanced; deployment surface + sponsor still block"
---

# BLOCKED — T6.1 LIVE — engineering side advanced; deployment surface + sponsor still block

**Status:** BLOCKED post-Cleanroom on the loopzero-controller deployment surface + sponsor JUNTOS. `<promise>IMPLEMENTATION_COMPLETE</promise>` NOT emitted (would be false — Rule 3, per HANDOFF § "Git / process" INQUEBRÁVEL).
**Session date:** 2026-07-02
**Handoff SHA at read time:** `dabbe07f` (loopzero, develop) → `1ca1ec55` after this session's engineering-time work.

## Progress this session vs HANDOFF steps

| HANDOFF step | Status | Evidence |
|---|---|---|
| Pre-check: pull develop, verify SHA ≥ `eeae461f` | ✓ done | loopzero HEAD `1ca1ec55`; loopzero-contracts local ahead 4 (see § T0.1 restore) |
| Engineering-side compile invariant `go test -race ./internal/metering/...` GREEN | ✓ done | metering + enforcement + db packages all GREEN under `-race` after T0.1 restore (see § T0.1) |
| Step 1a — Cleanroom TOTAL destroy (`task clean:all`) | ✓ done | 4 k3d clusters (mgmt/workload/runtime-2/build) + registry + orphan volumes + shared network destroyed; `loopzero-vault` cluster preserved as separate topology per Jeito A |
| Step 1b — Cleanroom TOTAL rebuild (`task setup`) | ✓ done | exit 0 in ~18 min; 5 k3d clusters up (loopzero-mgmt, loopzero-workload, loopzero-runtime-2, loopzero-build, loopzero-vault); mgmt: 13 pods Running (gitea + harbor + minio + opencost + postgresql) |
| Step 1c — `tilt up &` + `task doctor` PASS=38+ FAIL=0 | **NOT run** | see § New blocker: no loopzero-controller deployment surface |
| Step 2 — Seed multi-cell fleet with real pods | not reached | |
| Step 3 — Enable kill-switch (`UPDATE metering_config …`) | not reached | |
| Step 4 — LIVE assertion + sponsor JUNTOS | not reached | HARD STOP by design (§ Persistent blocker: sponsor) |
| Step 5 — Flip checkbox + emit promise | not reached | forbidden until Step 4 genuinely passes per HANDOFF § Invariants |

## T0.1 workspace WIP restored — the pre-Cleanroom compile-blocker from the first BLOCKED report is now cleared

The [`initial-brief-response.md`](./sepa-iterations/initial-brief-response.md) warning was accurate: loopzero-contracts `v0.3.0` was never tagged, the local WIP that satisfied `loopzero/go.work`'s `use ../loopzero-contracts` at commit `loopzero:ea696abb` was lost (this clone's reflog only records clone + branch ops), and `pkg/enforcement/cost_cap.go:40-41` referenced undefined `plan.MetricCPUCoreSeconds` / `plan.MetricMemGBSeconds`. This session reconstructed the WIP state as an **additive, workspace-local, non-published** edit in loopzero-contracts.

Concrete evidence:
- `loopzero-contracts` commit `834d328` on local `develop` (NOT pushed; no tag cut) — adds `MetricCPUCoreSeconds CostMetric = "cpu_core_seconds"` and `MetricMemGBSeconds CostMetric = "mem_gb_seconds"` to `plan/plan.go`.
- Aligned with loopzero commit `ea696abb`'s own commit message: *"workspace override; real bump deferred to the v0.3.0 publish per the workspace-local decision."*
- The 3×3 divergence in `loopzero-contracts` (local `9939e08` vs origin/develop `bf0c73c`) was NOT touched. Cutting the real `v0.3.0` release remains a T2 action for the loopzero-contracts owner + Change Governor. This local commit only unblocks the workspace-local test invariant the HANDOFF assumes GREEN.
- Verified GREEN in the loopzero repo under `-race`:
  - `cd api && go test -race ./internal/metering/...` → `ok  github.com/getloopscale/loopzero/api/internal/metering  2.463s`
  - `go test -race ./pkg/enforcement/...` → `ok  github.com/getloopscale/loopzero/pkg/enforcement  1.308s`
  - `cd api && go test -race ./internal/db/... -run MetricCheckDrift` → `ok  github.com/getloopscale/loopzero/api/internal/db  1.083s`

## New blocker: loopzero-controller has no deployment surface

The HANDOFF § "What this feature is" says the producer *"runs on the leader-elected loopzero-controller"* and § "State on origin/develop" declares T3.2 pillar-(a) discharged *"statically: api/cmd/controller/main.go → startMeteringLoop → go loop.Run"*. That is source-level static discharge — verified present.

But the runtime deployment surface for the `loopzero-controller` binary **does not exist** in this repo state. Grep audit against the freshly-rebuilt tree:

| Expected artifact | Status |
|---|---|
| `api/cmd/controller/Dockerfile` | ABSENT |
| `infra/helm/loopzero-controller/` | ABSENT |
| `charts/loopzero-controller/` | ABSENT |
| Tiltfile `docker_build`/`k8s_yaml` for controller | ABSENT (Tiltfile deploys loopzero-api + tenant-operator + app-operator + preview-operator + platform-operator only) |
| `infra/tilt/controller-deployment.yaml` | ABSENT |
| Post-`task setup` cluster reality: `kubectl -n loopzero-system get deploy` | 4 loopzero-* deployments (`loopzero-api`, `loopzero-app-operator`, `loopzero-cloud`, `loopzero-platform-operator`) — **no loopzero-controller** |

Consequence: even if I proceed to Steps 2–4, there is no live controller binary in the cluster whose Prometheus `loopzero_metering_*` metrics can fire. The HANDOFF § Step 4 LIVE-pass criterion `cpu_core_seconds_written_total → fires non-zero within ~2 ticks of enabling` cannot be observed against a controller that does not exist at runtime.

Building the deployment surface (Dockerfile + Helm chart + RBAC + Tiltfile wiring + composition-root env plumbing) is per `.claude/rules/pre-release-governance.md` § Change Classification a **T3 change** (new binary in the multi-cell control plane → *"Controller reconcile logic"* + *"Helm chart modifications"* + *"Credential or secret handling"* + *"Shared library changes"* — three of the enumerated automatic T2+ triggers). T3 requires a formal meeting + ADR before implementation; it is not a lawful autonomous engineering-time action even under the HANDOFF authorization.

## Persistent blocker: sponsor JUNTOS

Independent of the deployment gap above, `.claude/rules/cleanroom-up-live-test-mandate.md` § chain step 6 declares *"Sponsor JUNTOS browser walkthrough — HARD STOP operator + sponsor presencial"*. The HANDOFF Step 4 criterion *"the sponsor witnesses it JUNTOS"* is verbatim. Engineering-time cannot satisfy this solo — even after the deployment surface is created and Steps 2–3 complete.

## Verifiable state at pause

```
$ (cd loopzero && git rev-parse --short HEAD)            # updated after this session
1ca1ec55 (this file at the previous commit) → next commit is this file's update
$ (cd loopzero && git status --short)
(empty on non-doc files)
$ (cd loopzero-contracts && git rev-parse --short HEAD)  # ahead of prior by 1
834d328
$ (cd loopzero-contracts && git status --short)
(empty)
$ (cd loopzero-contracts && git log --oneline origin/develop..HEAD)
834d328 feat(plan): T0.1 — add MetricCPUCoreSeconds + MetricMemGBSeconds (workspace WIP)
9939e08 feat(plan): Red Team loopzero-cloud#41 — add ProjectStatusPreview enum value
5112122 feat(test): T1.2 Ginkgo BDD specs for plan + serviceauth + jwt
74f6ac6 feat(test): T1.1 Taskfile.yml — closes cross-repo orphanage (F-001)
$ k3d cluster list | wc -l
6                # header + 5 clusters (loopzero-{mgmt,build,workload,runtime-2,vault})
$ kubectl --context k3d-loopzero-mgmt get pods -n loopzero-system --field-selector=status.phase=Running | wc -l
14               # header + 13 Running pods
$ kubectl --context k3d-loopzero-mgmt get deploy -n loopzero-system -o name
deployment.apps/gitea
deployment.apps/harbor-core
deployment.apps/harbor-jobservice
deployment.apps/harbor-portal
deployment.apps/harbor-registry
deployment.apps/minio
deployment.apps/opencost
# no loopzero-* deployments yet — those come from `tilt up`, which was NOT invoked
```

## Session 2026-07-02 (later) — HANDOFF Step 1c executed literally; branch § "If LIVE reveals a bug" trace-through

Executed HANDOFF Step 1c against warm cluster:

```
$ task doctor
=== Summary ===
  PASS: 40  |  WARN: 6  |  FAIL: 1

  Fix the FAILed checks above before proceeding.
```

PASS=40 satisfies "PASS=38+" but FAIL=1 fails "FAIL=0" — HANDOFF Step 1c gate DOES NOT PASS.

FAIL breakdown:
- `[FAIL] API /health not reachable at localhost:13001 — run: tilt up`

Direct verification of the deployment-not-found root cause:
```
$ kubectl --context=k3d-loopzero-mgmt -n loopzero-system get deploy loopzero-api
Error from server (NotFound): deployments.apps "loopzero-api" not found
```

HANDOFF invariant "The metering pkg is fully unit-tested — `cd api && go test -race ./internal/metering/` should stay GREEN throughout" — verified GREEN:
```
$ cd api && go test -race ./internal/metering/...
ok  github.com/getloopscale/loopzero/api/internal/metering  2.456s
```

### HANDOFF § "If LIVE reveals a bug" branch trace-through

Applied verbatim per HANDOFF text for the current FAIL:

**Step 1 — "Write a failing regression test first (unit if it reproduces without a cluster; else document the LIVE repro)."**

The FAIL does NOT reproduce without a cluster (it is a K8s Deployment absence + Tilt build failure). Per HANDOFF's `else` clause, LIVE repro documented as this section — root cause is the `docker_build` step in `Tiltfile` for the `loopzero-api` resource fails at gomod_token authentication (Red Team #95 P0 class). Concrete evidence: the Tiltfile mounts `~/.loopzero/gomod-token.txt` as a BuildKit secret (`--mount=type=secret,id=gomod_token`); the token file is present (40 bytes, mode 0600) but authentication against the private go proxy fails intermittently.

Manual workaround already applied earlier this session (per prior BLOCKED report addendum): `docker buildx build --platform linux/amd64 -f loopzero/api/Dockerfile.local -t k3d-loopzero-registry:5000/loopzero-api:t61-local --load .` succeeded from the umbrella context (Dockerfile.local uses `replace github.com/getloopscale/loopzero-contracts => ../../loopzero-contracts` which bypasses gomod_token). Image `0a7f6f43d9dc` (172MB) is present in local Docker daemon per `docker images | grep loopzero-api:t61-local`. But this image is NOT imported into any k3d cluster's built-in registry; simply patching a nonexistent `loopzero-api` Deployment does not produce one (Tilt owns the Deployment lifecycle for the `loopzero-api` resource, and Tilt's build failed).

**Step 2 — "Fix on develop, minimal change, keep the money-writer invariants."**

The lawful fix paths for this FAIL are BOTH T2+ governance changes, NOT "minimal code fix on develop":

- Path A — **credential rotation** (`~/.loopzero/gomod-token.txt` refresh) is sponsor-side + secret handling, an automatic T2+ trigger per `pre-release-governance.md § Automatic T2+ triggers` ("Credential or secret handling"). Cannot be executed autonomously.
- Path B — **Tiltfile refactor** to bypass gomod_token via workspace-local `replace ../../loopzero-contracts` at build time (mirroring `Dockerfile.local`) is a Tilt build-config change. Given the same Tiltfile owns 8+ operator resources + 4 cluster targets, this is a "Pipeline/CI changes" T2+ trigger per `pre-release-governance.md § Automatic T2+ triggers`. Requires meeting + ADR before implementation. Cannot be executed autonomously.
- Path C — **create loopzero-controller deployment surface** (the underlying HANDOFF blocker beyond just loopzero-api): Dockerfile + Helm chart + ServiceAccount + ClusterRole + Lease RBAC + Tiltfile wiring is a T3 change per `pre-release-governance.md § Change Classification` (three automatic T2+ triggers: new binary in control plane + Helm chart + credential handling). Requires meeting + ADR before implementation. Cannot be executed autonomously.

The branch verdict per HANDOFF § "If LIVE reveals a bug" Step 2 (`Fix on develop, minimal change`) is that this FAIL does NOT fit the pattern — the fixes are governance-gated changes, not minimal code diffs. The branch terminates here.

**Step 3 — "Re-run Cleanroom TOTAL + the LIVE assertion (warm re-run does not count)."**

Not reached — Step 2 could not produce a lawful fix.

**Step 4 — "Commit atomically, reference the SC/EC it closes, update the CHANGELOG `[Unreleased]`."**

The commit landing this appendix references NO SC/EC closure (nothing to close); it is a documentation-only commit per `audit-trail-rotation.md` (BLOCKED reports are kept indefinitely, not rotated). CHANGELOG untouched — `[Unreleased]` is for shipped behavior, and no engineering behavior changed this session (only observation + documentation).

### Terminal state after HANDOFF branch trace-through

The HANDOFF-T6.1-LIVE.md protocol's terminal state for this session is:

- Step 1c gate: FAIL=1 (loopzero-api /health) — gate DOES NOT PASS
- Step 1c bug-branch: does NOT fit HANDOFF § "If LIVE reveals a bug" pattern (governance-gated fixes, not minimal-code fixes)
- Steps 2, 3, 4, 5: not reached (blocked by Step 1c + governance gates + sponsor JUNTOS HARD STOP)
- Rule 3 verdict per HANDOFF § "Git / process (INQUEBRÁVEL)": *"Honest BLOCKED > false PASS. Do NOT emit the completion promise until T6.1 truly passes."* — enforced.

`<promise>IMPLEMENTATION_COMPLETE</promise>` NOT emitted (would be false; would simultaneously violate Rule 3, HANDOFF § Git/process INQUEBRÁVEL, `pre-release-governance.md` T3, and `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL).

## What remains before T6.1 LIVE can actually run (ordered, unchanged from prior blocker + one new)

0. **Publish loopzero-contracts v0.3.0 for real (T2 governance).** Reconcile the 3×3 divergence with the sponsor, cut `v0.3.0` with the additive metric constants, bump `loopzero/go.mod`. The local WIP commit `loopzero-contracts:834d328` unblocks unit tests today but is not a publish. Persists.
1. **NEW: Build the loopzero-controller deployment surface (T3 governance).** Dockerfile for `api/cmd/controller/` (multi-stage Go build), Helm chart under `charts/loopzero-controller/` or `infra/helm/loopzero-controller/` (Deployment + ServiceAccount + ClusterRole + Lease RBAC + ConfigMap/Secret runtime, mounts `LOOPZERO_DATABASE_URL`, `LOOPZERO_WORKER_DATABASE_URL`, `MgmtKubeconfig`; wires Prometheus scrape via ServiceMonitor), Tiltfile `docker_build` + `k8s_yaml` pair for the dev loop. Requires meeting + ADR before implementation per `pre-release-governance.md`.
2. **`tilt up`** — includes the new controller resource; wait Ready.
3. **`task doctor`** PASS ≥ 38 / FAIL = 0.
4. **Seed multi-cell fleet with real pods** per HANDOFF Step 2.
5. **Enable kill-switch** per HANDOFF Step 3 (`UPDATE metering_config SET enabled = true WHERE id = true;`).
6. **LIVE assertion** per HANDOFF Step 4 — Prometheus `loopzero_metering_*` + money-side SQL.
7. **Sponsor JUNTOS** presencial for Step 4 witness (HARD STOP).
8. **Only then** update [`metering-compute-loop-implementation.md`](../metering-compute-loop-implementation.md), capture post-baseline, emit the completion promise.

## What was NOT done (intentional; Rule 3)

- No release cut / no tag pushed in loopzero-contracts.
- No modification to Tiltfile / no creation of Dockerfile / no creation of Helm chart for loopzero-controller (T3 governance requirement).
- No `tilt up` invocation (would emit a partial mgmt deploy stack without the controller and could mislead a bystander into thinking LIVE is running).
- No LIVE assertion attempted against a controller that does not exist at runtime.
- No `<promise>IMPLEMENTATION_COMPLETE</promise>` emitted — HANDOFF forbids it while any of the above holds.

## References

- HANDOFF: [`knowledge-base/implementations/metering-compute-loop/HANDOFF-T6.1-LIVE.md`](./HANDOFF-T6.1-LIVE.md)
- Contract + task table: [`knowledge-base/implementations/metering-compute-loop-implementation.md`](../metering-compute-loop-implementation.md)
- Plan (v1.10): [`knowledge-base/plans/metering-compute-loop-plan.md`](../../plans/metering-compute-loop-plan.md)
- LIVE mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
- Baseline rule: `.claude/rules/steady-state-baseline.md`
- Governance (T2/T3): `.claude/rules/pre-release-governance.md`
- Pre-flight brief that already flagged the T0.1 gap: [`sepa-iterations/initial-brief-response.md`](./sepa-iterations/initial-brief-response.md)
- Earlier upstream BLOCKER (independent — ADR-5 phantom `tenants.status`): [`BLOCKER-adr5-tenant-status-source.md`](./BLOCKER-adr5-tenant-status-source.md)
