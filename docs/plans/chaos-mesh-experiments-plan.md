# Plan: Chaos Mesh Experiments — Prove Resilience with Evidence

> **Version 1.1** (edge case review incorporated: EC-1 defer ordering, EC-2 namespaceSelector matchExpressions, EC-3 auth token) — Operationalize the existing Chaos Mesh infrastructure (9 manifests + Ginkgo framework in `infra/tests/chaos/`) by closing 6 gaps: 3 missing experiments, dedicated CI workflow, Prometheus SLO rules, and Helmfile bootstrap integration. The outcome is a weekly automated chaos suite that proves LoopZero self-heals under 12 failure scenarios, producing regression matrix entries and SLO evidence for the maturity 5.0 path.

## Context

Chaos Mesh 2.7.0 CRDs are registered in all 4 clusters (Sprint 8 T0.3, `infra/helm/chaos-mesh/`). A Ginkgo test framework (`infra/tests/chaos/chaos_test.go`, 387 LOC) and 9 experiment manifests exist but have **never been validated end-to-end** against live infrastructure. The gap is not tooling — it's operationalization.

**What exists:**
- 9 chaos manifests in `infra/tests/chaos/manifests/` (PodChaos ×4, NetworkChaos ×3, StressChaos ×1, PodChaos fixed-percent ×1)
- Ginkgo test suite with helpers: `applyChaos()`, `waitForPodReady()`, `checkAPIHealth()`, `checkMetricExists()`
- CI integration in `operator-split-soak.yml` and `pkg-soak.yml` (chaos injected mid-soak, not standalone)
- Helmfile repo registered (`chaos-mesh` in `mgmt.helmfile.yaml.gotmpl` line 73-74) but **no release defined**
- Comprehensive annotations (`loopzero.io/hypothesis`, `loopzero.io/slo-query`, `loopzero.io/dod-test`) on every manifest

**What's missing:**
1. **3 experiments not covered:** cell isolation cross-cluster partition (E2 from roadmap), random steady-state pod kill in tenant namespaces (E6), IOChaos disk pressure on BuildKit (E7)
2. **Dedicated CI workflow** — chaos only runs mid-soak in other workflows, not as standalone weekly gate
3. **PrometheusRule CRDs** — SLO queries exist as annotations but are not actualized as firing alerts
4. **Scenario 9 is a stub** — `chaos_test.go` Context "Cell network partition" only calls `checkAPIHealth()` without applying any NetworkChaos CRD
5. **Helmfile release** — `chaos-mesh` installed manually/out-of-band, not in bootstrap flow
6. **Regression matrix entries** — zero chaos entries in `docs/runbooks/bootstrap-regression-matrix.md`
7. **Never validated** — no evidence that any experiment has been applied against a running cluster

**Evidence motivating this work:**
- `docs/maturity-assessment.md` line 67: "Full chaos engineering suite" listed as path to 5.0
- `docs/strategy/resilience-roadmap.md`: P0 priority, estimated medium effort
- Sprint 8 T0.3 installed CRDs but no Phase 5 (chaos validation gate) ever ran
- Zero entries in regression matrix for chaos resilience scenarios

## Objective

**Done =** `LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v` passes all 12 scenarios against live k3d clusters, weekly CI workflow `chaos-suite.yml` runs with zero failures, and 12 regression matrix entries document each chaos scenario with permanent protection.

Specific goals:
1. All 12 chaos experiments validated against k3d (9 existing + 3 new)
2. Scenario 9 (cell partition) completed with real NetworkChaos CRD
3. CI workflow `chaos-suite.yml` on weekly cron (Sunday, like `cell2-soaks.yml`)
4. PrometheusRule `prometheusrule-chaos.yaml` with SLO alerts for chaos windows
5. Helmfile release for chaos-mesh in build/workload clusters
6. 12 regression matrix entries added

## ADRs

### D1 — No new chaos framework; extend existing Ginkgo suite
**Decision:** Add 3 new experiments and fix scenario 9 within the existing `infra/tests/chaos/` structure. Do not introduce a separate framework, DSL, or runner.
**Rationale:** `chaos_test.go` already has helpers, opt-in control (`LOOPZERO_CHAOS_TEST=1`), and the annotation convention (`loopzero.io/hypothesis`, `loopzero.io/slo-query`). Adding a framework for 3 new experiments violates KISS/YAGNI.
**Consequences:** All 12 experiments share the same helpers and CI workflow. New experiments follow the existing manifest + Ginkgo pattern.

### D2 — IOChaos for disk pressure, not StressChaos
**Decision:** Use `IOChaos` (filesystem latency injection) for BuildKit disk pressure experiment, not `StressChaos` (CPU/memory pressure).
**Rationale:** BuildKit writes to `/var/lib/buildkit` (layer cache + build context). Disk I/O latency is the realistic failure mode for "disk full" (I/O operations hang/slow). StressChaos would only stress CPU/memory, missing the actual disk path. IOChaos with `attr.percent: 100` and `delay: 500ms` simulates degraded disk without filling it.
**Consequences:** Requires Chaos Mesh daemon with `privileged: true` (already configured in `infra/helm/chaos-mesh/values.yaml` line 19). IOChaos uses kernel `fuse-overlayfs` injection.

### D3 — Cross-cluster partition via kubectl, not Chaos Mesh
**Decision:** Cell isolation experiment (E2) uses `kubectl patch` to cordon/uncordon the workload cluster context + verify mgmt API health, not a NetworkChaos CRD between clusters.
**Rationale:** Chaos Mesh CRDs operate within a single cluster (the daemon only has access to its own node's network stack). Cross-cluster network partitions require either iptables on the host or DNS manipulation — both are fragile in k3d (Docker networking). A more reliable simulation is: stop the workload cluster's API server temporarily via `k3d cluster stop loopzero-workload --no-delete` → verify mgmt API and cell-default continue operating → restart. This tests the actual DynamicRouter stale-data protection (MEET-053 A-03).
**Consequences:** E2 test is a Go test using `exec.Command("k3d", ...)`, not a Chaos Mesh manifest. This is a pragmatic deviation from the CRD-only pattern, justified by k3d's Docker networking limitations.

### D4 — SLO alerts use inhibit rules during chaos windows
**Decision:** Chaos experiments set a Prometheus label `loopzero_chaos_active=1` (via a ConfigMap annotation that the test framework toggles). PrometheusRule SLO alerts include `unless loopzero_chaos_active == 1` to prevent false pages during scheduled chaos windows.
**Rationale:** Without inhibit rules, the weekly chaos suite would trigger PagerDuty alerts for expected degradation. The chaos-specific PrometheusRule validates SLOs *within the chaos window* (separate from the always-on SLO alerts).
**Consequences:** Requires a lightweight sidecar or pushgateway for the `loopzero_chaos_active` metric. Simplest approach: `kubectl annotate` a ConfigMap + a recording rule.

### D5 — Weekly cron, not continuous chaos (GameDay approach)
**Decision:** Chaos suite runs weekly (Sunday 03:00 UTC), not continuously.
**Rationale:** Continuous chaos (kill random pods every N minutes) is a maturity 5.0+ practice. At 3.9, we need to validate known failure modes systematically before enabling random disruption. Weekly gives deterministic evidence for SLA auditing.
**Consequences:** `chaos-suite.yml` uses `schedule: cron: '0 3 * * 0'` matching `cell2-soaks.yml` pattern.

### D6 — Steady-state pod kill for tenant namespaces is bounded
**Decision:** Random pod kill experiment (E6) targets only namespaces matching `loopzero-tn-*` (tenant namespaces), uses `mode: fixed-percent` with `value: "30"` (30% of pods), and has `duration: 60s`.
**Rationale:** Killing 100% of tenant pods would test KEDA re-scaling but would take too long to recover in k3d (limited resources). 30% tests self-healing without overwhelming the cluster.
**Consequences:** Requires at least 1 deployed tenant workload in a `loopzero-tn-*` namespace during the chaos suite. The CI workflow must seed a test tenant + deploy before running E6.

## Dependency Graph

```
Phase 0 (Helmfile + Prometheus) ──▶ Phase 1 (New experiments) ──▶ Phase 2 (CI workflow)
                                         │
                                         ▼
                                   Phase 3 (Validation + matrix)
```

Phase 0 is prerequisite (bootstrap integration + alerting). Phase 1 adds 3 new experiments + fixes scenario 9. Phase 2 creates the CI workflow. Phase 3 validates everything end-to-end and writes regression entries.

---

## Phase 0: Bootstrap Integration + Prometheus SLO Rules

**Objective:** Make chaos-mesh part of the Helmfile bootstrap and add PrometheusRule CRDs for chaos SLO validation.

### T0.1 — Helmfile release for chaos-mesh

#### Objective
Add a Helmfile release so chaos-mesh is installed as part of `helmfile sync` on build and workload clusters, not manually.

#### Evidence
- `infra/helmfile/mgmt.helmfile.yaml.gotmpl` line 73-74: repo registered, no release.
- Sprint 8 T0.3 installed CRDs manually via `helm upgrade --install`.
- Without Helmfile integration, `task setup` doesn't install chaos-mesh → chaos tests fail on fresh environments.

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — add chaos-mesh release block
infra/helmfile/build.helmfile.yaml.gotmpl — add chaos-mesh release block (if exists, else mgmt only)
infra/helmfile/workload.helmfile.yaml.gotmpl — add chaos-mesh release block (if exists, else mgmt only)
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml.gotmpl`: Helmfile template that declares all mgmt cluster releases. Adding a new release block for `loopzero-chaos-mesh` follows the same pattern as existing releases (name, chart, namespace, values). Downstream: `helmfile sync` picks it up automatically. No other files depend on this change.
- Build/workload helmfiles: Same pattern. The chaos-mesh chart is cluster-scoped (`clusterScoped: true` in values), so CRDs register globally.

#### Deep Dives
- The chart is `infra/helm/chaos-mesh/` (wrapper around upstream `chaos-mesh/chaos-mesh` v2.7.0).
- Values already hardened per ADR-0016 (dashboard disabled, PSA restricted, non-root daemon except privileged for syscall injection).
- Namespace: `chaos-mesh` (create if not exists).
- Condition: Only install if `{{ .Values.chaosMesh.enabled | default false }}` — chaos-mesh should be opt-in in dev/staging, never in production.

#### Tasks
1. Add release block in mgmt helmfile with condition `{{ .Values.chaosMesh.enabled | default false }}`
2. Add `chaosMesh.enabled: true` in `infra/helmfile/environments/dev.yaml`
3. Add `chaosMesh.enabled: false` in `infra/helmfile/environments/prod.yaml`
4. Verify `helmfile diff` shows the new release without errors

#### TDD
```
RED:     TestHelmfileChaosMeshRelease — parse mgmt.helmfile.yaml.gotmpl, assert release "loopzero-chaos-mesh" exists with namespace "chaos-mesh" and condition
GREEN:   Add the release block
REFACTOR: None expected
VERIFY:  cd infra && helmfile -e dev diff 2>&1 | grep chaos-mesh
```

#### Acceptance Criteria
- [ ] `helmfile -e dev diff` shows chaos-mesh release
- [ ] `helmfile -e prod diff` does NOT show chaos-mesh (disabled by condition)
- [ ] Chart namespace is `chaos-mesh`
- [ ] Values reference `infra/helm/chaos-mesh/values.yaml`

#### DoD
- [ ] Helmfile release added for all relevant clusters
- [ ] `helmfile -e dev template` renders successfully
- [ ] Environment toggle works (dev=enabled, prod=disabled)

---

### T0.2 — PrometheusRule for chaos SLO validation

#### Objective
Create a PrometheusRule CRD that encodes the SLO queries from the chaos manifest annotations as firing alerts. These alerts validate steady-state during chaos windows.

#### Evidence
- Every manifest has `loopzero.io/slo-query` annotation with PromQL (e.g., `pg-network-latency.yaml` checks p99 < 30s).
- `infra/helm/monitoring/manifests/` has 12 existing PrometheusRule files — pattern is well established.
- No chaos-specific PrometheusRule exists today.

#### Files to edit
```
infra/helm/monitoring/manifests/prometheusrule-chaos.yaml — (NEW) chaos SLO alerts
```

#### Deep file dependency analysis
- `infra/helm/monitoring/manifests/`: Directory of PrometheusRule YAML files, each picked up by the monitoring Helm chart. Adding a new file follows the same pattern — Helm chart uses `{{ .Files.Glob "manifests/*.yaml" }}` or direct reference.
- Downstream: Prometheus picks up the rules automatically via operator.

#### Deep Dives
- Each rule group maps to one chaos experiment.
- Rules use `for: 0s` (instant, not delayed) because chaos windows are short (60-180s).
- Labels: `severity: chaos`, `experiment: <manifest-name>`.
- The `loopzero_chaos_active` metric (ADR D4) inhibits normal SLO alerts but NOT chaos-specific rules.

#### Tasks
1. Extract PromQL from all 9 manifest annotations (`loopzero.io/slo-query`)
2. Create PrometheusRule CRD with one group per experiment
3. Add `chaos_active` recording rule from ConfigMap annotation
4. Validate with `promtool check rules prometheusrule-chaos.yaml`

#### TDD
```
RED:     TestChaosPrometheusRuleExists — read prometheusrule-chaos.yaml, assert it parses as valid PrometheusRule
RED:     TestChaosPrometheusRuleCoversAllExperiments — count rule groups == number of experiments
GREEN:   Create the PrometheusRule file
REFACTOR: None expected
VERIFY:  promtool check rules infra/helm/monitoring/manifests/prometheusrule-chaos.yaml
```

#### Acceptance Criteria
- [ ] `promtool check rules` passes without errors
- [ ] One rule group per chaos experiment (12 total after Phase 1)
- [ ] Each rule has `severity: chaos` label
- [ ] `kubeconform --strict` passes on the file

#### DoD
- [ ] PrometheusRule file created and valid
- [ ] All 9 existing SLO queries encoded
- [ ] `task validate:manifests` passes

---

## Phase 1: Complete Missing Experiments + Fix Scenario 9

**Objective:** Add 3 new chaos experiments and complete the stub scenario 9, bringing total coverage to 12 scenarios.

### T1.1 — Cell isolation experiment (cross-cluster partition)

#### Objective
Validate that stopping the workload cluster does not affect the mgmt API or other cells. This proves DynamicRouter stale-data protection (MEET-053 A-03).

#### Evidence
- `chaos_test.go` scenario 9 (line 377-387) is a stub — only calls `checkAPIHealth()` without applying any fault.
- DynamicRouter (`api/internal/placement/dynamic_router.go`) has stale-data protection: if refresh returns 0 cells, it refuses to clear the cell map.
- k3d allows `k3d cluster stop/start` for individual clusters.

#### Files to edit
```
infra/tests/chaos/chaos_test.go — replace stub scenario 9 with full cross-cluster test
infra/tests/chaos/manifests/cell-isolation-partition.yaml — (NEW) documentation manifest (no CRD, per ADR D3)
```

#### Deep file dependency analysis
- `chaos_test.go`: Adding a new test context replaces the existing stub at line 377-387. No downstream dependency. Uses existing `kubectl` and `kubectlContext` helpers.
- New manifest is documentation-only (the test uses `k3d cluster stop/start` directly per ADR D3). Follows the annotation convention for consistency.

#### Deep Dives
- Test flow: (1) verify API healthy, (2) `k3d cluster stop loopzero-workload`, (3) wait 30s, (4) verify API still healthy (mgmt cluster), (5) verify DynamicRouter metric `loopzero_cell_discovery_count` stays at previous value (stale-data protection), (6) `k3d cluster start loopzero-workload`, (7) verify cell recovers within 120s.
- Edge case: k3d `stop` may take 5-10s to fully stop containers. Test must poll, not rely on fixed sleep.
- Edge case: after restart, ClusterCredential refresh takes up to 60s (poll interval). Test timeout should be 180s.

#### Tasks
1. Replace stub scenario 9 with full implementation
2. Add helper `k3dClusterStop(name)` and `k3dClusterStart(name)` with `exec.Command`
3. Add helper `checkCellDiscoveryCount(expected int)` that queries Prometheus or metrics endpoint
4. Create documentation manifest `cell-isolation-partition.yaml` with hypothesis annotation
5. **Register defer for `k3d cluster start` BEFORE calling stop** (EC-1 MUST FIX — if test panics after stop but before defer registration, cluster stays stopped and all subsequent tests fail). Pattern: `defer k3dClusterStart("loopzero-workload")` on first line of `It()`, then `k3dClusterStop("loopzero-workload")` after.
6. Add `BeforeAll` guard: verify workload cluster is running, start if stopped (defensive against prior test failure)

#### TDD
```
RED:     TestChaos_CellIsolation — stop workload cluster → API healthy → restart → cell recovers (fails: stub has no fault injection)
GREEN:   Implement full k3d stop/start flow with health assertions
REFACTOR: Extract k3d helpers into shared function
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -run "Cell network partition" -v
```

#### Acceptance Criteria
- [ ] Stopping workload cluster does not crash mgmt API
- [ ] DynamicRouter stale-data protection holds (cell count not zeroed)
- [ ] Workload cluster recovers after restart within 180s
- [ ] Test has defer for cluster restart registered BEFORE stop call (EC-1)
- [ ] BeforeAll guard restores stopped cluster from prior failures

#### DoD
- [ ] Scenario 9 replaced with full implementation
- [ ] Test passes against k3d
- [ ] k3d cluster always restored (defer registered before stop)

---

### T1.2 — Steady-state random pod kill in tenant namespaces

#### Objective
Validate that randomly killing 30% of tenant workload pods results in automatic recovery via Deployment controller + KEDA re-scaling.

#### Evidence
- No existing experiment targets tenant namespaces (`loopzero-tn-*`).
- KEDA HTTP Add-on is configured for scale-to-zero; pod kill should trigger KEDA to scale back up.
- This validates the "general self-healing" capability that every PaaS must have.

#### Files to edit
```
infra/tests/chaos/manifests/steady-state-pod-kill.yaml — (NEW) PodChaos targeting tenant namespaces
infra/tests/chaos/chaos_test.go — add scenario 10: steady-state pod kill
```

#### Deep file dependency analysis
- New manifest: PodChaos CRD targeting `loopzero-tn-*` namespaces with `mode: fixed-percent`, `value: "30"`. No downstream dependencies.
- `chaos_test.go`: Adding scenario 10 after scenario 9. Uses existing `applyChaos()` helper. New assertion: verify pod count returns to pre-chaos level within 120s.

#### Deep Dives
- Selector: `namespaces: ["loopzero-tn-*"]` is NOT valid in Chaos Mesh — namespace selectors are exact matches. Solution: use `namespaceSelectors` with `matchExpressions` operator `Exists` on key `loopzero.io/tenant` (EC-2 MUST FIX — using `matchLabels` with empty value `""` would NOT match tenant namespaces where the label value is the tenant name, e.g., `loopzero.io/tenant: tn-e2e`). Confirmed in TenantReconciler envtest: label value = tenant name, never empty.
- Per ADR D6: `mode: fixed-percent`, `value: "30"`, `duration: "60s"`.
- Prerequisite: at least 1 tenant workload must be deployed. The CI workflow (Phase 2) handles seeding.
- **Pre-assertion (EC-2):** Before applying chaos, test MUST verify `kubectl get pods` with the same selector returns >= 1 pod. If 0 pods found, fail with clear message ("no tenant pods found — seed a tenant workload first"), not pass trivially.

#### Tasks
1. Create manifest `steady-state-pod-kill.yaml` with `namespaceSelectors` + hypothesis annotation
2. Add scenario 10 to `chaos_test.go`
3. Add helper `countPodsInNamespace(namespace, labelSelector)` for pre/post comparison
4. Assert pod count recovers within 120s after chaos ends

#### TDD
```
RED:     TestChaos_SteadyStatePodKill — kill 30% of tenant pods → pods recover within 120s (fails: no test exists)
GREEN:   Implement manifest + test
REFACTOR: None expected
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -run "Steady-state" -v
```

#### Acceptance Criteria
- [ ] Manifest uses `namespaceSelectors` with tenant label (not hardcoded namespace)
- [ ] 30% pod kill does not exceed cluster capacity
- [ ] Pods recover within 120s
- [ ] Duration bounded at 60s

#### DoD
- [ ] Manifest created with annotations
- [ ] Ginkgo test passes
- [ ] Recovery time < 120s confirmed

---

### T1.3 — IOChaos disk pressure on BuildKit

#### Objective
Validate that BuildKit handles degraded disk I/O gracefully — builds fail with timeout, not hang forever.

#### Evidence
- No `IOChaos` experiment exists (only PodChaos, NetworkChaos, StressChaos used today).
- `build-cluster-network-partition.yaml` tests network, not disk.
- Build orchestrator relies on `activeDeadlineSeconds` for workflow timeout — IOChaos validates this path.
- `infra/helm/chaos-mesh/values.yaml` has privileged daemon (required for IOChaos fuse injection).

#### Files to edit
```
infra/tests/chaos/manifests/buildkit-disk-pressure.yaml — (NEW) IOChaos targeting BuildKit pods
infra/tests/chaos/chaos_test.go — add scenario 11: BuildKit disk pressure
```

#### Deep file dependency analysis
- New manifest: IOChaos CRD targeting BuildKit containers in `argo` namespace. Injects filesystem latency on `/var/lib/buildkit`. No downstream dependencies.
- `chaos_test.go`: Adding scenario 11. Uses `kubectlContext(buildContext, ...)` for build cluster operations. New assertion: verify active workflows transition to Failed (not hung).

#### Deep Dives
- IOChaos spec:
  ```yaml
  spec:
    action: latency
    delay: "500ms"
    percent: 100
    path: "/var/lib/buildkit"
    selector:
      namespaces: [argo]
      labelSelectors:
        "app": "buildkit"
    duration: "120s"
  ```
- Edge case: BuildKit pod may not exist if no build is running. Test must either trigger a build first or use a pre-seeded long-running pod.
- Edge case: IOChaos requires the chaos-mesh daemon to be running on the build cluster. Helmfile integration (T0.1) covers this.
- Fallback: if IOChaos is not available (chaos-mesh daemon not on build cluster), test should `Skip` with clear message.

#### Tasks
1. Create manifest `buildkit-disk-pressure.yaml` with IOChaos spec + hypothesis annotation
2. Add scenario 11 to `chaos_test.go`
3. Add pre-check: verify chaos-mesh daemon exists on build cluster
4. Optionally trigger a build to ensure BuildKit pod is running
5. Assert: build fails with timeout, not infinite hang

#### TDD
```
RED:     TestChaos_BuildKitDiskPressure — IOChaos 500ms latency on /var/lib/buildkit → build times out gracefully (fails: no test exists)
GREEN:   Implement manifest + test with build-cluster context
REFACTOR: None expected
VERIFY:  LOOPZERO_CHAOS_TEST=1 LOOPZERO_BUILD_CONTEXT=k3d-loopzero-build go test ./infra/tests/chaos/ -run "BuildKit disk" -v
```

#### Acceptance Criteria
- [ ] IOChaos manifest targets correct path (`/var/lib/buildkit`)
- [ ] Chaos duration bounded (120s)
- [ ] Build fails with timeout, not hang
- [ ] Test skips gracefully if chaos-mesh daemon not on build cluster

#### DoD
- [ ] IOChaos manifest created
- [ ] Ginkgo test passes (or skips with message if daemon missing)
- [ ] No infinite hangs observed

---

### T1.4 — Deploy-under-chaos integration test

#### Objective
Validate the complete deploy flow while chaos is active — trigger a real `loopzero deploy` during API pod kill experiment and verify the deploy either succeeds (after recovery) or fails cleanly with typed error.

#### Evidence
- Existing tests validate recovery (pod comes back) but never test that a *user operation* completes through the chaos.
- `chaos_test.go` scenario 1 kills the API and checks health, but never attempts a deploy.
- This is the critical gap: "pod recovered" ≠ "deploy worked".
- Advisory lock release on pod death is the key invariant (`api/internal/lock/environment_lock.go` — transaction-scoped `pg_advisory_xact_lock` releases on connection drop).

#### Files to edit
```
infra/tests/chaos/chaos_test.go — add scenario 12: deploy during chaos
```

#### Deep file dependency analysis
- `chaos_test.go`: Adding scenario 12 that combines `applyChaos("pod-kill-api-leader.yaml")` with a real deploy flow. Requires E2E helpers (`deployAndWaitSuccess` pattern from `infra/tests/e2e/helpers_test.go`). Since chaos tests are in a different package, either duplicate minimal helpers or use a shared package.
- Key dependency: the deploy must target a seeded tenant/project. CI workflow (Phase 2) handles seeding.

#### Deep Dives
- Flow: (1) apply pod-kill chaos, (2) immediately start deploy (via API call, not CLI — to avoid CLI timeout complexity), (3) wait for build terminal state, (4) if SUCCEEDED: pass. If FAILED with typed error: pass (clean failure). If hung/no response: fail.
- The advisory lock is transaction-scoped (`pg_advisory_xact_lock`). When the API pod dies, the PG connection drops, the transaction rolls back, and the lock is released. The replacement pod's next deploy attempt should acquire the lock successfully.
- Timeout: 5 minutes (deploy timeout in E2E constants).
- Edge case: pod kill happens before deploy reaches PG → deploy is retried by CLI. Pod kill happens during PG transaction → lock released, deploy fails, next attempt succeeds.

#### Tasks
1. Add `resolveToken()` helper to chaos_test.go — reads `LOOPZERO_TOKEN` env var, falls back to `~/.loopzero/local-dogfood-token.txt` (EC-3 MUST FIX — deploy requires valid JWT; chaos package is separate from E2E package and has no auth helpers)
2. Add scenario 12 to `chaos_test.go`
3. Create minimal deploy helper (HTTP POST to `/api/v1/builds` with build spec + `Authorization: Bearer` header)
4. Apply pod-kill chaos → immediately POST deploy → wait for terminal state
5. Assert: build reaches terminal state (SUCCEEDED or FAILED with typed error), never hung
6. If first deploy fails, attempt second deploy → must succeed (lock released)
7. Add `skipIfNoToken()` guard — Skip with message if no auth token available

#### TDD
```
RED:     TestChaos_DeployDuringPodKill — deploy during chaos → terminal state reached within 5min (fails: no test exists)
GREEN:   Implement deploy-during-chaos test with retry logic
REFACTOR: Extract API call helper
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -run "Deploy during chaos" -v
```

#### Acceptance Criteria
- [ ] Deploy reaches terminal state (SUCCEEDED or typed FAILED) during chaos
- [ ] Advisory lock released on pod death (second attempt succeeds)
- [ ] No infinite hang or untyped error
- [ ] Test timeout at 5 minutes

#### DoD
- [ ] Scenario 12 implemented
- [ ] Test validates lock release invariant
- [ ] Passes against k3d with chaos-mesh active

---

## Phase 2: CI Workflow

**Objective:** Create a dedicated `chaos-suite.yml` GitHub Actions workflow that runs all 12 experiments weekly.

### T2.1 — Create chaos-suite.yml workflow

#### Objective
Automate the chaos suite on a weekly cron (Sunday 03:00 UTC) with manual dispatch support, following the `cell2-soaks.yml` pattern.

#### Evidence
- No `.github/workflows/*chaos*` file exists.
- `cell2-soaks.yml` is the reference pattern (cron + manual dispatch + preflight + issue on failure).
- `operator-split-soak.yml` shows chaos preflight check pattern (`kubectl -n chaos-mesh get pods | grep controller-manager`).

#### Files to edit
```
.github/workflows/chaos-suite.yml — (NEW) weekly chaos engineering workflow
```

#### Deep file dependency analysis
- New workflow file. No downstream dependencies.
- Uses existing secrets: `KUBECONFIG_DEV_MGMT`, `LOOPZERO_API_URL_DEV`, `PROM_URL_DEV`, `LOOPZERO_ADMIN_SERVICE_AUTH_DEV`.
- Depends on: chaos-mesh installed on target clusters, test tenant seeded, Prometheus reachable.

#### Deep Dives
- Job structure:
  1. `preflight` — verify chaos-mesh installed, API healthy, Prometheus reachable, test tenant exists
  2. `chaos-suite` — run `LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -timeout 30m`
  3. `report` — on failure: create GitHub issue with `priority/critical` + `area/chaos` labels

- Timeout: 45 minutes total (12 experiments, most bounded at 60-180s + recovery waits).
- Concurrency: `cancel-in-progress: false` (never cancel a running chaos run).
- Artifact: JUnit XML report via `JUNIT_REPORT_PATH` env var.

#### Tasks
1. Create `.github/workflows/chaos-suite.yml` with cron + manual dispatch
2. Add preflight job (chaos-mesh pods, API health, test tenant)
3. Add chaos-suite job with `LOOPZERO_CHAOS_TEST=1` + timeout
4. Add report job with issue creation on failure
5. Add JUnit artifact upload

#### TDD
```
RED:     TestChaosWorkflowExists — verify .github/workflows/chaos-suite.yml exists and has required fields (fails: file doesn't exist)
GREEN:   Create the workflow file
REFACTOR: None expected
VERIFY:  yamllint .github/workflows/chaos-suite.yml && gh workflow view chaos-suite.yml
```

#### Acceptance Criteria
- [ ] Workflow runs on `schedule: cron: '0 3 * * 0'`
- [ ] Manual dispatch with `workflow_dispatch` supported
- [ ] Preflight validates chaos-mesh + API + Prometheus
- [ ] `cancel-in-progress: false`
- [ ] JUnit XML uploaded as artifact
- [ ] GitHub issue created on failure with `priority/critical` label
- [ ] Timeout: 45 minutes

#### DoD
- [ ] Workflow file created and valid YAML
- [ ] `gh workflow list` shows the workflow
- [ ] Manual trigger succeeds (dry-run at minimum)

---

## Phase 3: Validation + Regression Matrix

**Objective:** Run the full chaos suite against k3d and document results in the regression matrix.

### T3.1 — End-to-end validation against k3d

#### Objective
Run all 12 chaos experiments against a fresh k3d environment and document results.

#### Evidence
- None of the 9 existing experiments have evidence of ever running successfully.
- `chaos_test.go` exists but no test results or CI logs prove execution.

#### Files to edit
```
(no files — this is a validation task, results documented in T3.2)
```

#### Deep file dependency analysis
Not applicable — validation only.

#### Deep Dives
- Prerequisites: `task setup` + `tilt up` + at least 1 tenant deployed.
- Run command: `LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -timeout 30m 2>&1 | tee /tmp/chaos-results.txt`
- Expected: all 12 scenarios pass. If any fail, fix the test or manifest (not the infrastructure — the test validates the infrastructure, and failures are valid findings).
- Capture: full Ginkgo output + Prometheus metrics during window.

#### Tasks
1. Boot fresh k3d: `task setup && tilt up && task doctor`
2. Seed test tenant: deploy a simple app to a tenant namespace
3. Run: `LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -timeout 30m`
4. Capture results (pass/fail per scenario)
5. For each failure: diagnose root cause, fix test or document as finding
6. Re-run until 12/12 pass

#### TDD
```
RED:     N/A — this is validation, not code
GREEN:   N/A
REFACTOR: N/A
VERIFY:  LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v -count=1
```

#### Acceptance Criteria
- [ ] All 12 scenarios pass against k3d
- [ ] Results captured as text output
- [ ] Any failures diagnosed and fixed

#### DoD
- [ ] 12/12 GREEN
- [ ] No flaky tests (re-run confirms deterministic)

---

### T3.2 — Regression matrix entries

#### Objective
Add 12 entries to the bootstrap regression matrix documenting each chaos scenario as a permanent protection.

#### Evidence
- `docs/runbooks/bootstrap-regression-matrix.md` has 82+ entries but zero for chaos resilience.
- Each chaos experiment proves a specific resilience invariant that must not regress.

#### Files to edit
```
docs/runbooks/bootstrap-regression-matrix.md — add 12 new entries
```

#### Deep file dependency analysis
- Regression matrix is a Markdown table. Adding rows follows the existing format (8 columns). No downstream dependencies — the matrix is documentation, consumed by humans and review skills.

#### Deep Dives
Matrix entry format per existing convention:
```
| # | Category | Problem | Resolved By | Protection | Type | Verification | Pode Recorrer? |
```

New category: `CHAOS_RESILIENCE`.

#### Tasks
1. Add entries C1-C12 for each chaos scenario
2. Each entry references the manifest and test name
3. Verification column: `LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -run "<test_name>"`
4. "Pode Recorrer?" = SIM for all (resilience can regress if code changes)

#### TDD
```
RED:     TestRegressionMatrixHasChaosEntries — read matrix, assert 12 entries with category CHAOS_RESILIENCE (fails: no entries exist)
GREEN:   Add the 12 entries
REFACTOR: None expected
VERIFY:  grep -c "CHAOS_RESILIENCE" docs/runbooks/bootstrap-regression-matrix.md  # should return 12
```

#### Acceptance Criteria
- [ ] 12 entries added with category `CHAOS_RESILIENCE`
- [ ] Each entry has verification command
- [ ] Matrix still renders correctly in Markdown

#### DoD
- [ ] 12 entries in regression matrix
- [ ] Markdown renders correctly
- [ ] No broken table formatting

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Helmfile release not defined (manual install) | T0.1 | Helmfile release with environment condition |
| 2 | SLO queries in annotations, not PrometheusRule | T0.2 | PrometheusRule CRD with 12 rule groups |
| 3 | Cell isolation (E2) not tested | T1.1 | k3d cluster stop/start test (ADR D3) |
| 4 | Random steady-state pod kill (E6) missing | T1.2 | PodChaos fixed-percent 30% in tenant namespaces |
| 5 | IOChaos disk pressure (E7) missing | T1.3 | IOChaos on /var/lib/buildkit with 500ms delay |
| 6 | Scenario 9 is stub (no fault injection) | T1.1 | Full implementation replaces stub |
| 7 | Deploy-under-chaos never tested | T1.4 | Real deploy during pod-kill + lock release validation |
| 8 | No dedicated CI workflow | T2.1 | chaos-suite.yml weekly cron |
| 9 | Never validated end-to-end | T3.1 | Full run against k3d, 12/12 GREEN |
| 10 | Zero regression matrix entries | T3.2 | 12 CHAOS_RESILIENCE entries |
| 11 | Maturity 5.0 "full chaos suite" requirement | All | 12 scenarios validated weekly |

**Coverage: 11/11 gaps covered (100%)**

## Global Definition of Done

- [ ] All phases completed
- [ ] All tests passing (`LOOPZERO_CHAOS_TEST=1 go test ./infra/tests/chaos/ -v` = 12/12 GREEN)
- [ ] Zero lint warnings (`golangci-lint run ./infra/tests/chaos/...`)
- [ ] `task validate:manifests` passes (new YAML files valid)
- [ ] `promtool check rules` passes on chaos PrometheusRule
- [ ] Helmfile `diff` shows chaos-mesh release for dev, not for prod
- [ ] **`/cross-validation chaos-mesh-experiments` PASSED** — zero CRITICO, zero GRAVE
- [ ] CI workflow `chaos-suite.yml` triggers successfully (manual dispatch)
- [ ] 12 regression matrix entries added
- [ ] **`/dogfood full` PASSED**

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> **No plan is considered DONE without passing `/dogfood full`.** Unit tests, integration tests, and lint are necessary but NOT sufficient. Only real end-to-end validation via dogfood proves the system works.

After ALL phases are complete and ALL tests pass, the final validation is a full dogfood audit.

### Execution

```bash
# 1. Fresh infrastructure (operator must run before dogfood)
task teardown && task setup && tilt up
task doctor  # verify health

# 2. Run full dogfood
/dogfood full
```

### Pass Criteria

- [ ] `/dogfood full` executed on clean infrastructure
- [ ] Pre-Flight Check: ALL checks PASS
- [ ] Zero S0 (Bloqueante) findings
- [ ] Zero S1 (Grave) findings related to this plan's scope
- [ ] Prova de Vida passes for every deploy performed during dogfood
- [ ] Prova de Morte passes for every delete performed during dogfood
- [ ] No regressions in previously passing flows
- [ ] Observability check passes (logs, metrics, CRs in correct state)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure

If `/dogfood full` finds S0 or S1 issues:
1. Fix ALL S0/S1 findings
2. Re-run `/dogfood full` on fresh infrastructure
3. Repeat until zero S0/S1

S2-S4 findings are documented but do not block plan completion.
