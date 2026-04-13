# Harbor Gaps Resolution Plan

## Status

**Implemented** (2026-04-13) — All 7 gaps resolved. Pending deployment validation on DO.

## Context

Harbor integration (MEET-048, ADR-002) was implemented with correct architecture but has 7 operational gaps that must be resolved before the DOCR-to-Harbor migration can begin. Two gaps (G3, G5) are migration blockers explicitly defined in MEET-048's mandatory gates.

### Source of truth

- **Architecture decision:** `docs/adr/ADR-002-registry-migration-harbor-spaces.md`
- **Meeting record:** `docs/meetings/records/MEET-2026-04-11-048.md`
- **Helm chart:** `infra/helm/harbor/`
- **Pulumi bootstrap:** `infra/pulumi/src/components/harbor-bootstrap.ts`
- **Go client:** `operators/internal/harbor/client.go`
- **Tenant integration:** `operators/internal/controller/tenant_harbor.go`
- **Init script:** `infra/scripts/harbor-init/main.go`

---

## Gap Inventory

| ID | Gap | Severity | Tier | Blocker? | Status |
|----|-----|----------|------|----------|--------|
| G1 | values.yaml vs Pulumi replica divergence | Medium | T1 | No | **Done** — replicas=1, PDB conditional |
| G2 | Robot secret not recoverable if K8s Secret deleted | Medium-High | T1 | No | **Done** — secretExists check + force recreate |
| G3 | Monitoring incomplete (alerts + dashboard + canary) | Critical | T2 | **Yes (MEET-048)** | **Done** — 6 alerts + dashboard + canary CronJob |
| G4 | Manual JSON quoting instead of encoding/json | Low | T1 | No | **Done** — json.Marshal + typed structs |
| G5 | Dual-push missing for tenant builds | Critical | T2 | **Yes (MEET-048)** | **Done** — crane copy mirror step in DAG |
| G6 | harbor-init not automated in pipeline | Medium | T1 | No | **Done** — Helm post-install Job + CI build |
| G7 | HarborProjectReady not a required condition for Active | Medium | T1 | No | **Done** — conditional required condition |

---

## Execution Order

```
Phase 1 — T1 fixes (independent, no meeting required)
┌──────┐  ┌──────┐  ┌──────┐  ┌──────┐
│  G1  │  │  G4  │  │  G7  │  │  G2  │
│values│  │ JSON │  │ cond │  │secret│
│align │  │marshal│  │ req  │  │recov │
└──────┘  └──────┘  └──────┘  └──────┘
  (all can run in parallel)

Phase 2 — Automation (prerequisite for Phase 3)
┌──────┐
│  G6  │
│harbor│
│ init │
│ auto │
└──┬───┘
   ↓

Phase 3 — MEET-048 blocker gates
┌──────┐  ┌──────┐
│  G3  │  │  G5  │
│monit │  │ dual │
│ pkg  │  │ push │
└──────┘  └──────┘
  (can run in parallel, G5 depends on G6)

Phase 4 — Migration gate checklist
  All DoDs met → DOCR→Harbor migration can begin
```

---

## G1 — Align values.yaml with Pulumi (replicas)

### Problem

`values.yaml` declares `core.replicas: 2` and `registry.replicas: 2`, but Pulumi (`harbor-bootstrap.ts:106-133`) overrides both to `1`. The `values.yaml` becomes misleading documentation. Additionally, PDB with `minAvailable: 1` on a single replica blocks node drain during cluster upgrades.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G1.1 | Update values.yaml to reflect dev deployment | `infra/helm/harbor/values.yaml` | `core.replicas: 1`, `registry.replicas: 1` |
| G1.2 | Add comment explaining prod override via Pulumi | `infra/helm/harbor/values.yaml` | Comment on core and registry sections |
| G1.3 | Make PDB conditional on replicas > 1 | `infra/helm/harbor/templates/pdb-core.yaml` | `{{- if gt (int .Values.harbor.core.replicas) 1 }}` |
| G1.4 | Make PDB conditional on replicas > 1 | `infra/helm/harbor/templates/pdb-registry.yaml` | Same conditional |
| G1.5 | Validate manifests | — | `task validate:manifests` |

### Acceptance criteria

- [ ] `values.yaml` and `harbor-bootstrap.ts` declare the same replica count for dev (1)
- [ ] PDB is not rendered when replicas = 1 (no node drain blockage)
- [ ] PDB is rendered when replicas > 1 (prod protection preserved)
- [ ] `task validate:manifests` passes
- [ ] Comment explains how to scale for prod

### Definition of Done

- values.yaml reflects dev reality (replicas=1)
- PDB conditional on replicas > 1
- No functional change to running system

---

## G2 — Robot secret recovery (deleted K8s Secret)

### Problem

Harbor does not return the secret for existing robot accounts. If a K8s Secret is manually deleted while the robot has TTL > 2h remaining, `ensureHarborRobotAccounts` skips secret creation (`tenant_harbor.go:76`). BuildKit fails with auth errors for up to 22 hours until TTL drops below the 2h renewal threshold.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G2.1 | Write failing test: reconcile recreates secret when K8s Secret is missing but robot has valid TTL | `operators/internal/controller/tenant_harbor_test.go` | Test with envtest client returning NotFound for secret |
| G2.2 | Add `harborBuildSecretExists` check | `operators/internal/controller/tenant_harbor.go` | `func (r *TenantReconciler) harborSecretExists(ctx, client, namespace, name string) bool` |
| G2.3 | Modify `ensureHarborRobotAccounts`: if K8s Secret missing, force delete+recreate robot | `operators/internal/controller/tenant_harbor.go` | Before checking `buildCreds.Secret != ""`, verify K8s Secret exists. If missing, delete robot via `DeleteRobotAccount` and call `EnsureRobotAccount` again to get fresh credentials |
| G2.4 | Apply same logic to pull robot (all environment namespaces) | `operators/internal/controller/tenant_harbor.go` | Same verification per env namespace |
| G2.5 | Run all operator tests | — | `cd operators && go test ./...` |

### Acceptance criteria

- [ ] K8s Secret deleted manually -> next reconcile recreates robot and writes secret (< 1 reconcile cycle)
- [ ] K8s Secret exists and is valid -> no recreation (idempotent, no churn)
- [ ] Logs clearly indicate when forced recreation occurs due to missing secret
- [ ] Pull secrets in ALL environment namespaces are verified
- [ ] Robot accounts are not needlessly churned when everything is healthy

### Definition of Done

- No 22-hour blind window for missing secrets
- Reconcile is self-healing for deleted secrets
- Tests cover: secret exists, secret missing, secret exists but robot expired

---

## G3 — Complete monitoring package (alerts + dashboard + canary)

### Problem

Prometheus scrapes Harbor exporter (`mgmt-observability.ts:56-68`, job `harbor`, target `harbor-exporter.platform-system.svc:8001`), but no Harbor-specific alerts, Grafana dashboard, or canary test exist. MEET-048 mandates: *"Registry monitoring package before migration"* as a blocking gate.

### Microtasks

#### Alerts

| # | Task | File | Change |
|---|------|------|--------|
| G3.1 | Add alert `HarborDown` — exporter unreachable > 5min | `infra/pulumi/src/components/mgmt-observability.ts` | PrometheusRule in existing alerts array. Expr: `up{job="harbor"} == 0` for 5m |
| G3.2 | Add alert `HarborStorageQuotaHigh` — project > 80% quota | `infra/pulumi/src/components/mgmt-observability.ts` | Expr: `harbor_project_quota_usage_byte / harbor_project_quota_byte > 0.8` |
| G3.3 | Add alert `HarborGCFailing` — GC job failed > 24h | `infra/pulumi/src/components/mgmt-observability.ts` | Expr: based on `harbor_jobservice_info` or GC-related metrics |
| G3.4 | Add alert `HarborPGPoolExhaustion` — connections > 8 of 10 | `infra/pulumi/src/components/mgmt-observability.ts` | Expr: `pg_stat_activity` count where `datname="harbor_db"` > 8 |
| G3.5 | Add alert `HarborPushFailures` — 5xx rate on push > 0 for 5min | `infra/pulumi/src/components/mgmt-observability.ts` | Expr: `rate(harbor_core_http_request_total{code=~"5.."}[5m]) > 0` |

#### Dashboard

| # | Task | File | Change |
|---|------|------|--------|
| G3.6 | Create Grafana dashboard JSON for Harbor | `infra/helm/monitoring/dashboards/harbor.json` | Panels: health status, push/pull request rate, request latency p99, storage usage per project, GC duration + blobs deleted, PG connection count, active robot accounts, canary status |
| G3.7 | Register dashboard in Grafana provisioning | `infra/pulumi/src/components/mgmt-observability.ts` | ConfigMap with label `grafana_dashboard: "1"` (follows existing pattern) |

#### Canary

| # | Task | File | Change |
|---|------|------|--------|
| G3.8 | Create CronJob for canary push/pull (every 5min) | `infra/helm/harbor/templates/canary-cronjob.yaml` | Push tiny image (busybox or scratch), pull, delete. Expose `harbor_canary_success` and `harbor_canary_duration_seconds` metrics via pushgateway or annotation scrape |
| G3.9 | Add alert `HarborCanaryFailing` — canary failed > 2 consecutive times | `infra/pulumi/src/components/mgmt-observability.ts` | PrometheusRule based on canary metric |

#### Validation

| # | Task | File | Change |
|---|------|------|--------|
| G3.10 | Validate alert rules syntax | — | `promtool check rules` or equivalent validation |
| G3.11 | Validate dashboard JSON structure | — | Grafana API dry-run or JSON schema validation |
| G3.12 | Validate all new manifests | — | `task validate:manifests` |

### Acceptance criteria

- [ ] 6 Harbor-specific alerts configured (Down, StorageQuota, GCFailing, PGPool, PushFailures, Canary)
- [ ] Grafana dashboard with >= 7 panels rendering real data from Harbor exporter
- [ ] Canary CronJob runs every 5min and exposes success/failure metric
- [ ] `HarborPGPoolExhaustion` fires when connections > 8 (verifiable via manual PromQL query)
- [ ] Alerts route to existing Alertmanager (same channel as platform alerts)
- [ ] `task validate:manifests` passes with all new manifests
- [ ] Dashboard is auto-provisioned (no manual import required)

### Definition of Done

- Harbor is observable: if it goes down, someone is notified within 5 minutes
- Dashboard enables diagnosis without `kubectl` access
- Canary detects push/pull failures before they impact tenant builds
- Monitoring package complete per MEET-048 mandatory gate
- All alerts tested with `promtool` or equivalent

---

## G4 — Replace manual JSON quoting with encoding/json

### Problem

`buildDockerConfigJSON` in `tenant_harbor.go:181-193` uses `fmt.Sprintf` + a custom `quote()` function that only escapes `\` and `"`. It does not escape `\n`, `\t`, `\r`, `\b`, `\f`, or Unicode control characters (< 0x20), all of which are required by the JSON spec. A robot secret containing a newline produces malformed Docker config.json.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G4.1 | Write failing test: secret with `\n` and `\t` produces valid JSON | `operators/internal/controller/tenant_harbor_test.go` | `TestBuildDockerConfigJSON_SpecialChars`: assert `json.Valid()` on output |
| G4.2 | Define typed struct for Docker config.json | `operators/internal/controller/tenant_harbor.go` | `type dockerConfigJSON struct { Auths map[string]dockerConfigAuth }` / `type dockerConfigAuth struct { Username, Password, Auth string }` |
| G4.3 | Rewrite `buildDockerConfigJSON` using `json.Marshal` | `operators/internal/controller/tenant_harbor.go` | Replace `fmt.Sprintf` + `quote()` with struct construction + `json.Marshal` |
| G4.4 | Delete `quote()` function | `operators/internal/controller/tenant_harbor.go` | Remove dead code |
| G4.5 | Run existing + new tests | — | `cd operators && go test ./internal/controller/...` |

### Acceptance criteria

- [ ] Test with secret `"abc\ndef\"ghi\\tjkl"` produces JSON parseable by `json.Unmarshal`
- [ ] Docker config.json has correct structure (`{"auths":{"host":{"username":"...","password":"...","auth":"..."}}}`)
- [ ] All existing `buildDockerConfigJSON` tests continue passing
- [ ] Zero usage of `fmt.Sprintf` for JSON construction in this file
- [ ] `quote()` function deleted

### Definition of Done

- `json.Marshal` is the only JSON serialization method
- All JSON spec special characters correctly escaped
- No dead code remaining

---

## G5 — Dual-push for tenant builds

### Problem

`infra-deploy.yml` already implements dual-push for **platform images** (loopzero-api, tenant-controller, etc., lines 763-774). Tenant builds (images produced by BuildKit during `loopzero deploy`) still push only to DOCR. ADR-002 requires dual-push during migration: *"Dual-push during migration (both DOCR and Harbor receive images)"*.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G5.1 | Add `HarborRegistryURL` to API config | `api/internal/config/config.go` | New field: `HarborRegistryURL string` via env `HARBOR_REGISTRY_URL`, empty = disabled |
| G5.2 | Write test: build plan generates two push destinations when dual-push enabled | `api/tests/unit/build_plan_test.go` | Test with `HarborRegistryURL` set and unset |
| G5.3 | Modify build plan: when `HarborRegistryURL` configured, add secondary push destination | `api/internal/services/build_plan.go` (or equivalent build orchestration) | Add Harbor image reference to workflow parameters |
| G5.4 | Write test: build plan with single destination (backward compat) | `api/tests/unit/build_plan_test.go` | Test without `HARBOR_REGISTRY_URL` — identical to current behavior |
| G5.5 | Modify CWT build template to accept multi-destination push | `infra/helm/argo-workflows/templates/cwt-build.yaml` (or equivalent) | Parameter `secondary-registry` (optional). If set, push to both. Secondary push failure = warning, not error |
| G5.6 | Write test: CWT renders correctly with and without secondary registry | — | Template rendering test or manual validation |
| G5.7 | Configure `HARBOR_REGISTRY_URL` in Pulumi for API deployment | `infra/pulumi/src/components/mgmt-bootstrap.ts` | Add env var to loopzero-api deployment when `harborExternalUrl` is set |
| G5.8 | Validate: `loopzero deploy` pushes to both registries | — | E2E: deploy → verify image exists in DOCR AND Harbor |

### Acceptance criteria

- [ ] `loopzero deploy` with dual-push enabled: image appears in DOCR AND Harbor
- [ ] `loopzero deploy` without dual-push (env var unset): behavior identical to current (DOCR only)
- [ ] Failure pushing to Harbor does NOT block the deploy (Harbor is secondary during migration)
- [ ] Failure pushing to DOCR DOES block the deploy (DOCR is primary during migration)
- [ ] Logs clearly indicate which registry received the push and success/failure per registry
- [ ] Build plan tests cover: no Harbor, with Harbor, with Harbor down (graceful degradation)
- [ ] No internal terms in any error messages (Rule 3)

### Definition of Done

- Dual-push functional for tenant builds
- DOCR remains primary (failure = build fails)
- Harbor is secondary (failure = log warning, build continues)
- Config flag allows disabling dual-push without code redeploy (env var)
- Backward compatible: zero impact if `HARBOR_REGISTRY_URL` not set
- Tests cover all permutations

---

## G6 — Automate harbor-init in pipeline

### Problem

`harbor-init` (`infra/scripts/harbor-init/main.go`) is a manual script. After `pulumi up`, Harbor may be running with default password (`Harbor12345`), no `loopzero-platform` project, and no CI robot account. The TenantController would fail to create tenant projects because system configuration hasn't been applied.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G6.1 | Create Dockerfile for harbor-init | `infra/scripts/harbor-init/Dockerfile` | Multi-stage Go build, distroless final image |
| G6.2 | Add harbor-init image build to CI workflow | `.github/workflows/build-images.yml` | Additional step in build matrix |
| G6.3 | Create Helm post-install Job template | `infra/helm/harbor/templates/post-install-init.yaml` | Job with `helm.sh/hook: post-install,post-upgrade`, `helm.sh/hook-weight: "10"`, `helm.sh/hook-delete-policy: before-hook-creation` |
| G6.4 | Configure Job environment from Secrets | `infra/helm/harbor/templates/post-install-init.yaml` | `HARBOR_URL` from values, `HARBOR_ADMIN_PASSWORD_NEW` from K8s Secret (ESO-synced) |
| G6.5 | Add values for init Job configuration | `infra/helm/harbor/values.yaml` | Section `harbor.init.enabled`, `harbor.init.image`, `harbor.init.passwordSecretRef` |
| G6.6 | Configure init values in Pulumi | `infra/pulumi/src/components/harbor-bootstrap.ts` | Pass init image reference and secret name |
| G6.7 | Add fallback step in infra-deploy.yml | `.github/workflows/infra-deploy.yml` | Conditional step: run harbor-init via `kubectl run` if Job didn't complete |
| G6.8 | Test idempotency: run init twice | — | Verify exit code 0 on both runs, no duplicate resources |

### Acceptance criteria

- [ ] `pulumi up` -> Harbor starts -> post-install Job runs automatically -> Harbor configured
- [ ] Job is idempotent (can run N times with exit code 0)
- [ ] Job fails visibly if Harbor is not healthy (exit 1) — not silently
- [ ] Admin password is NOT `Harbor12345` after deploy
- [ ] `loopzero-platform` project exists after deploy
- [ ] `ci-pipeline` robot account exists after deploy
- [ ] Password comes from ESO/Secret, not hardcoded in manifest
- [ ] Security context: non-root, drop ALL, no privilege escalation

### Definition of Done

- Zero manual intervention after `pulumi up`
- Job visible in cluster (`kubectl get jobs -n platform-system`)
- Job logs accessible for debugging
- harbor-init image in registry (DOCR during migration, Harbor after)
- `helm.sh/hook-delete-policy: before-hook-creation` ensures old Job is cleaned up

---

## G7 — HarborProjectReady as required condition for Active

### Problem

`TypeHarborProjectReady` is set by `ensureHarborProject()` (`tenant_harbor.go:38-46`) but is NOT in the `requiredProvisioningConditions` list (`tenant_controller.go:205-211`). A tenant can transition to Active state even if Harbor project creation failed. This means builds would fail with auth errors because the Harbor project doesn't exist.

### Microtasks

| # | Task | File | Change |
|---|------|------|--------|
| G7.1 | Write failing test: tenant does not transition to Active if HarborProjectReady=False (with HarborClient configured) | `operators/internal/controller/tenant_controller_test.go` | envtest test with Harbor-enabled reconciler |
| G7.2 | Write test: tenant transitions to Active normally without HarborClient (DOCR mode backward compat) | `operators/internal/controller/tenant_controller_test.go` | envtest test with nil HarborClient |
| G7.3 | Add `TypeHarborProjectReady` to required conditions when HarborClient is configured | `operators/internal/controller/tenant_controller.go` | Conditional append to `requiredProvisioningConditions` based on `r.HarborClient != nil` |
| G7.4 | Run all operator tests | — | `cd operators && go test ./...` |

### Acceptance criteria

- [ ] Tenant with HarborClient configured does NOT transition to Active without HarborProjectReady=True
- [ ] Tenant WITHOUT HarborClient (DOCR mode) transitions to Active normally (backward compat preserved)
- [ ] Explicit test covers both scenarios
- [ ] No regression in existing tenant controller tests

### Definition of Done

- Condition is required only when Harbor is enabled
- Backward compatibility with DOCR preserved
- Tests cover: Harbor enabled + project ready, Harbor enabled + project failed, Harbor disabled

---

## Migration Gate Checklist

When all gaps are resolved, validate before starting DOCR-to-Harbor migration:

| # | Gate | Source | How to verify |
|---|------|--------|---------------|
| 1 | Harbor alerts firing in Alertmanager | G3 | Trigger test alert, confirm delivery |
| 2 | Grafana dashboard functional with real data | G3 | Visual inspection, all panels rendering |
| 3 | Canary push/pull running > 24h without failure | G3 | `kubectl logs` + Prometheus query `harbor_canary_success` |
| 4 | Dual-push functional for tenant builds | G5 | `loopzero deploy` -> image in DOCR AND Harbor |
| 5 | harbor-init automated after pulumi up | G6 | `pulumi up` -> Job completes -> Harbor configured |
| 6 | Robot secret self-healing validated | G2 | Delete secret -> reconcile -> secret recreated |
| 7 | PDB does not block node drain | G1 | `kubectl drain` on single-replica dev cluster succeeds |
| 8 | HarborProjectReady required for tenant Active | G7 | Tenant with failed Harbor project stays in Provisioning |
| 9 | DO Spaces bucket-scoped access keys validated | ADR-002 | Keys can only access harbor bucket, not other Spaces |
| 10 | DO API Token eliminated as registry auth | ADR-002 | `buildkit-registry-config` uses Harbor robot, not DO token |

All 10 gates must pass before migration begins. Gates 9 and 10 are pre-existing ADR-002 requirements, not part of this plan's implementation scope.

---

## Governance Notes

- **G1, G2, G4, G6, G7** are T1 changes (single-component, local impact). Standard TDD workflow applies.
- **G3** is T2 (Pulumi infra + new Helm templates). It extends existing monitoring infrastructure with new alerts and dashboards — no contract changes, no state machine changes, no API changes.
- **G5** is T2 (API config + CWT modification). It adds an optional secondary push destination — DOCR behavior is unchanged, Harbor push is additive and failure-tolerant.
- If T2 classification is disputed, invoke Change Governor before implementing G3 or G5.
