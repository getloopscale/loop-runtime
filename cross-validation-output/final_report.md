# Cross-Validation Report — loopzero vs sealos

**Engagement:** loop-cross-validation  ·  **Date:** 2026-06-30  ·  **Reference:** labring/sealos (Apache-2.0)
**Scope:** Go control-plane layer (operators, multi-tenancy, deploy, billing/metering, security, observability) — the surface where both projects compete. sealos's TS cloud-desktop, cloud-IDE, managed-DB and AI-proxy surfaces have no loopzero counterpart and are out of scope (product divergence, not scored).

---

## 1. Executive summary

loopzero (a CLI-first managed Kubernetes-backed PaaS, Go-dominant, ~632K LOC) was cross-validated against sealos (an AI-native Cloud OS on Kubernetes, TS-dominant, ~495K LOC, Apache-2.0) across **12 weighted dimensions** with code-level evidence cited file:line from both projects. loopzero is a **disciplined Go control-plane codebase** that meets or beats the reference on error handling, test breadth, and external-API contract discipline, and trails it only on continuous compute-resource metering and module-boundary granularity.

**Overall Score: 4.04 / 5.0** (weighted) · 4.04 / 5.0 (simple) — ≈81%

**Top 3 gaps:**

1. **[high]** No periodic per-tenant-namespace compute-resource metering loop (CPU/mem/storage) (`controllers/resources/controllers/monitor_controller.go:393`)
2. **[medium]** Pricing is hardcoded in Go (ComputeMonthlyCost) rather than a data-driven model (`controllers/pkg/resources/resources.go:281`)
3. **[low]** No centralized retry/backoff helper — retry logic is duplicated ad-hoc (`controllers/pkg/utils/retry/retry.go:22`)

---

## 2. Score card

| Dimension | Category | Weight | Score | Reference file | Summary |
|---|---|:---:|:---:|---|---|
| Testing Strategy | testing | 1.4 | **4.5/5** | `controllers/resources/controllers/monitor_controller_test.go:1` | loopzero has ~5x the Go test file count and envtest-backed operator tests + property tests. On… |
| Error Handling | error_handling | 1.3 | **4.5/5** | `controllers/account/controllers/account_controller.go:335` | loopzero's typed-error system (LoopZeroError + code→status registry + scrub of internal terms) is … |
| API Design & Contracts | api_design | 1.1 | **4.5/5** | `service/account/api/card.go:88` | loopzero has stronger external-contract discipline: idempotency keys, a documented --json stab… |
| Multi-Tenancy & Isolation | security | 1.5 | **4.0/5** | `controllers/user/controllers/user_controller.go:299` | loopzero adds a 3-NetworkPolicy isolation triad + multi-cell cross-cluster placement that the … |
| Operator / Controller Design | architecture | 1.5 | **4.0/5** | `controllers/license/internal/controller/license_controller.go:89` | Both follow controller-runtime best practice (finalizers, conditions, owner refs, idempote… |
| Security & Secrets Management | security | 1.3 | **4.0/5** | `controllers/pkg/crypto/crypto.go:38` | Both handle secrets/crypto competently. loopzero adds externalized secret management (Vault Tr… |
| Observability | observability | 1.1 | **4.0/5** | `controllers/pkg/utils/logger/logger.go:127` | loopzero emits rich in-process Prometheus metrics + tracing where sealos reconcilers rely on l… |
| Folder Organization & Naming | architecture | 1.0 | **4.0/5** | `controllers/user:1` | Both use package-by-feature with specific names (no utils/common dumping grounds). Compara… |
| Build System & Dev Experience | tooling | 1.0 | **4.0/5** | `scripts/detect-build-modules.sh:20` | Both mature. loopzero optimizes the inner dev loop (Tilt + k3d parity); sealos optimizes modul… |
| Configuration Management | configuration | 0.8 | **4.0/5** | `controllers/user/deploy/charts/user-controller/values.yaml:1` | Comparable. sealos co-locates each controller's chart with the controller (deploy/charts);… |
| Module Boundaries & Layering | architecture | 1.5 | **3.5/5** | `controllers/go.work:1` | sealos enforces harder compile-time boundaries (each controller a module); loopzero trades tha… |
| Metering & Billing Pipeline | domain | 1.0 | **3.5/5** | `controllers/resources/controllers/monitor_controller.go:393` | Refined 3.0→3.5 after phase-4 discovery: loopzero's billing plumbing (separate history/enforce… |

**Weighted Average: 4.04 / 5.0** · **Simple Average: 4.04 / 5.0**

### Score distribution
```
4.5 █████████░  Testing Strategy
4.5 █████████░  Error Handling
4.5 █████████░  API Design & Contracts
4.0 ████████░░  Multi-Tenancy & Isolation
4.0 ████████░░  Operator / Controller Design
4.0 ████████░░  Security & Secrets Management
4.0 ████████░░  Observability
4.0 ████████░░  Folder Organization & Naming
4.0 ████████░░  Build System & Dev Experience
4.0 ████████░░  Configuration Management
3.5 ███████░░░  Module Boundaries & Layering
3.5 ███████░░░  Metering & Billing Pipeline
```

---

## 3. Gap analysis (prioritized)

| # | Gap | Severity | Type | Reference file |
|---|---|---|---|---|
| 1 | No periodic per-tenant-namespace compute-resource metering loop (CPU/mem/storage) | high | missing_pattern | `controllers/resources/controllers/monitor_controller.go:393` |
| 2 | Pricing is hardcoded in Go (ComputeMonthlyCost) rather than a data-driven model | medium | missing_pattern | `controllers/pkg/resources/resources.go:281` |
| 3 | No centralized retry/backoff helper — retry logic is duplicated ad-hoc | low | missing_abstraction | `controllers/pkg/utils/retry/retry.go:22` |
| 4 | No dedicated runtime exception-monitoring service (only threshold-based Prometheus alerts) | low | missing_feature | `service/exceptionmonitor:1` |
| 5 | Single coarse go.work limits independent module versioning/release cadence | low | architecture_option | `controllers/go.work:1` |

### Gap #1 — No periodic per-tenant-namespace compute-resource metering loop (CPU/mem/storage)  `[high]`

- **Type:** `missing_pattern`  ·  **Dimension target:** `operators/internal/ (new metering reconciler) + api/internal (aggregator tick) + api/internal/db/migrations (new usage_records metrics)`
- **Reference:** `controllers/resources/controllers/monitor_controller.go:393`
  - `func (r *MonitorReconciler) monitorResourceUsage(namespace *corev1.Namespace) error { ... r.monitorPodResourceUsage(...); r.monitorPVCResourceUsage(...); ... re`
- **What:** sealos runs a periodic reconcile (StartPeriodicReconcile → enqueueNamespacesForReconcile → monitorResourceUsage) that, every Interval, lists all tenant namespaces, filters system ones, and in parallel goroutines sums 5 resource categories per namespace — pod CPU/mem (from pod.Spec.Resources.Requests, i.e. ALLOCATED capacity, AWS-style), PVC storage, DB backup, services/nodeports, object storage — writing time-series Monitor records keyed by (namespace, app). loopzero bills flat per-plan tiers + 3 usage metrics (egress_bytes, docr_pulls, build_minutes) via its aggregator → usage_records/cost_counters, and computes ComputeMonthlyCost(replicas, podCostPerHour) ONLY as a UI estimate (pkg/enforcement/scale.go:135). loopzero has NO loop that meters actual allocated CPU/mem/storage per tenant namespace into usage_records for billing. For a PaaS that scales workloads, this means revenue is not tied to the compute footprint tenants actually reserve.
- **Suggestion:** Add a periodic metering reconciler modeled on sealos. loopzero ALREADY has the storage (usage_records history→Stripe + cost_counters enforcement) + cell-aware tenant namespaces + a metric aggregator, so this is medium-effort: (1) periodic loop cf. monitor_controller.go:204 startPeriodicReconcile + :283 enqueueNamespacesForReconcile; (2) per tenant namespace sum pod.Spec.Resources.Requests (cpu/mem)×replicas + PVC sizes cf. monitor_controller.go:458 monitorPodResourceUsage + :405 monitorPVCResourceUsage; (3) filter system namespaces cf. filterNormalNamespace:299; (4) write new metrics cpu_core_seconds / mem_gb_seconds / storage_gb_hours into the EXISTING usage_records + cost_counters tables (no new storage); (5) price via a data-driven model (next gap). This makes billing reflect the workload footprint loopzero provisions.

### Gap #2 — Pricing is hardcoded in Go (ComputeMonthlyCost) rather than a data-driven model  `[medium]`

- **Type:** `missing_pattern`  ·  **Dimension target:** `pkg/enforcement/ + a pricing config table or file`
- **Reference:** `controllers/pkg/resources/resources.go:281`
  - `Name: "cpu", Enum: 0, PriceType: AVG, UnitPrice: 2.237442922,`
- **What:** sealos defines pricing as DATA: each meterable resource has a PropertyType with a UnitPrice (e.g. cpu Enum=0 UnitPrice=2.237.., memory, storage, network, nodeports), so adding/repricing a metric is a config change, not a code change. loopzero's ComputeMonthlyCost (pkg/enforcement/scale.go:135) hardcodes the formula (replicas × podCostPerHour) and plan tiers are enumerated in code.
- **Suggestion:** Extract loopzero pricing into a data-driven table (DB or config) keyed by metric, mirroring sealos resources.go:281-... PropertyType/UnitPrice. Enables repricing + new billable metrics without code deploys, and directly feeds the metering loop above. Pairs with the per-namespace metering gap.

### Gap #3 — No centralized retry/backoff helper — retry logic is duplicated ad-hoc  `[low]`

- **Type:** `missing_abstraction`  ·  **Dimension target:** `pkg/ (new retry helper) + the ad-hoc retry call sites`
- **Reference:** `controllers/pkg/utils/retry/retry.go:22`
  - `func Retry(tryTimes int, trySleepTime time.Duration, action func() error) error {`
- **What:** sealos centralizes retry-with-backoff in controllers/pkg/utils/retry/retry.go (Retry(tryTimes, sleep, action) with growing backoff). loopzero implements retry ad-hoc at multiple call sites (api/internal/cfzone/client.go, api/internal/health/prober.go, api/internal/services/container_delivery.go) with no shared primitive. loopzero HAS pkg/circuitbreaker but not a reusable retry helper.
- **Suggestion:** Extract a pkg/retry helper (or adopt k8s.io/apimachinery/pkg/util/wait.Backoff which loopzero already imports) and refactor the ad-hoc retry sites to use it. Low effort, DRY win. Ref controllers/pkg/utils/retry/retry.go:22.

### Gap #4 — No dedicated runtime exception-monitoring service (only threshold-based Prometheus alerts)  `[low]`

- **Type:** `missing_feature`  ·  **Dimension target:** `infra/helm + a new watcher (or extend existing alerting)`
- **Reference:** `service/exceptionmonitor:1`
  - `service/exceptionmonitor (dedicated Go service watching for exception conditions + notifications)`
- **What:** sealos ships service/exceptionmonitor — a runtime service that actively watches workloads for exception conditions and notifies. loopzero relies on Prometheus SLO alerts (reactive, threshold-based) + runbooks (pkg/quality/alertrunbooks, docs/runbooks/alerts). The reference's approach catches exception patterns that fixed thresholds miss.
- **Suggestion:** Evaluate whether loopzero's threshold-based SLO alerts miss exception classes a watcher would catch (e.g. CrashLoopBackOff patterns per-tenant). loopzero already has strong Prometheus coverage, so this is OPTIONAL — adopt only if alert gaps are observed in dogfood. Ref service/exceptionmonitor.

### Gap #5 — Single coarse go.work limits independent module versioning/release cadence  `[low]`

- **Type:** `architecture_option`  ·  **Dimension target:** `go.work / operators/`
- **Reference:** `controllers/go.work:1`
  - `use ( ./account ./app ./devbox ... ./user )  // 12 independent controller modules`
- **What:** sealos composes 24+ independently-versioned Go modules via go.work with per-module k8s replace pinning (controllers/go.work + service/go.work), letting each controller/service release on its own cadence. loopzero's single go.work groups api/operators/pkg as 7 coarse modules — simpler to build but couples release cadence across operators.
- **Suggestion:** OPTIONAL / trade-off, not a defect: loopzero's single workspace is a defensible KISS win for one team + faster builds. Revisit per-operator modules ONLY if operators acquire independent release cadences or cross-operator coupling causes friction. Ref controllers/go.work:1-26.

---

## 4. Detailed comparisons per dimension

### Testing Strategy — **4.5/5** (testing, weight 1.4)

- **loopzero:** 792 *_test.go + 62 Ginkgo/envtest suites + property-based tests (errors) + explicit invalid-transition-panics test. (`pkg/statemachine/application_test.go:52`)
- **sealos:** 161 *_test.go (Go) + 36 *.test.ts. Metering controller has a test (monitor_controller_test.go). (`controllers/resources/controllers/monitor_controller_test.go:1`)
- **Analysis:** loopzero has ~5x the Go test file count and envtest-backed operator tests + property tests. On the Go control plane loopzero is the more rigorously tested project. (Caveat: sealos's TS frontend has its own 36 specs not counted here.)
- **Suggestion:** No action — test breadth is a strength. Maintain envtest discipline.

### Error Handling — **4.5/5** (error_handling, weight 1.3)

- **loopzero:** pkg/errors.LoopZeroError carries typed Code; pkg/errors/http maps codes→HTTP status via registry (Status()); property tests assert code preservation across Wrap. (`pkg/errors/errors.go:40`)
- **sealos:** Errors handled via standard fmt.Errorf("...%w", err) wrapping; no typed/domain error package across controllers/services. (`controllers/account/controllers/account_controller.go:335`)
- **Analysis:** loopzero's typed-error system (LoopZeroError + code→status registry + scrub of internal terms) is materially more disciplined than sealos's plain %w wrapping. This is a clear loopzero strength.
- **Suggestion:** No action — typed errors are a strength. (Reference shows the simpler %w baseline at account_controller.go:335.)

### API Design & Contracts — **4.5/5** (api_design, weight 1.1)

- **loopzero:** chi + LoopZeroError + Idempotency-Key middleware + --json stability contract + permanent exit codes (0-5) + scrub (no internal terms in output). (`api/internal/middleware/idempotency.go:1`)
- **sealos:** Gin + struct-tag validation (binding:required) + nil-field checks at handler boundary; JSON responses via gin.H. (`service/account/api/card.go:88`)
- **Analysis:** loopzero has stronger external-contract discipline: idempotency keys, a documented --json stability contract, fixed exit codes, and output scrubbing. sealos validates inputs well but exposes less formal contract guarantees.
- **Suggestion:** No action — contract discipline is a strength. (Reference input-validation baseline at service/account/api/card.go:88.)

### Multi-Tenancy & Isolation — **4.0/5** (security, weight 1.5)

- **loopzero:** Preview/tenant operators create deny-all + allow-ingress + allow-egress NetworkPolicies + ResourceQuota + LimitRange; multi-cell placement isolates tenants across clusters. (`operators/internal/preview/preview_controller.go:1`)
- **sealos:** user_controller CreateOrUpdate namespace + RoleBinding (owner role); account_controller adds per-ns ResourceQuota + LimitRange. No per-tenant NetworkPolicy or multi-cluster cell surfaced. (`controllers/user/controllers/user_controller.go:299`)
- **Analysis:** loopzero adds a 3-NetworkPolicy isolation triad + multi-cell cross-cluster placement that the reference's namespace+RBAC+quota model does not surface. sealos isolation is solid but flatter (single-cluster, no network policy in the path read).
- **Suggestion:** Maintain NetworkPolicy triad + cell isolation — a genuine strength. Ref user_controller.go:299-438 for the namespace+RBAC baseline.

### Operator / Controller Design — **4.0/5** (architecture, weight 1.5)

- **loopzero:** 4 split operator binaries; transitions go through pkg/statemachine (11 machines, invalid transition panics) + conditions + finalizers + envtest. (`pkg/statemachine/application.go:1`)
- **sealos:** Reconcile uses finalizers (AddFinalizer), SetControllerReference, meta.SetStatusCondition, CreateOrUpdate idempotent apply. (`controllers/license/internal/controller/license_controller.go:89`)
- **Analysis:** Both follow controller-runtime best practice (finalizers, conditions, owner refs, idempotent apply). loopzero adds an explicit state-machine layer enforcing legal transitions; sealos keeps state logic inline in reconcilers. loopzero's discipline is a plus; sealos's per-domain module is a plus.
- **Suggestion:** Keep the state-machine layer. Consider sealos's meta.SetStatusCondition standardization if loopzero conditions diverge. Ref license_controller.go:89, devbox/api/v1alpha2/conditions.go:46.

### Security & Secrets Management — **4.0/5** (security, weight 1.3)

- **loopzero:** pkg/vault wraps OpenBao Transit (encrypt/decrypt) with caching, zeroize, and redaction; supply-chain via cosign signing + syft SBOM; auth via Ory Kratos/Hydra + RSA JWT. (`pkg/vault/transit_client.go:1`)
- **sealos:** AES-GCM crypto (crypto.go) + JWT HS256 (jwt.go) + RSA PEM parsing; Gin handler input validation. (`controllers/pkg/crypto/crypto.go:38`)
- **Analysis:** Both handle secrets/crypto competently. loopzero adds externalized secret management (Vault Transit), memory zeroize/redaction, and supply-chain signing/SBOM that the reference's in-process AES-GCM + HS256 JWT does not cover.
- **Suggestion:** Maintain Vault Transit + cosign/SBOM. (Reference crypto baseline at controllers/pkg/crypto/crypto.go:38, jwt.go:45.)

### Observability — **4.0/5** (observability, weight 1.1)

- **loopzero:** 119 unique loopzero_* Prometheus metrics via promauto + Grafana dashboards + SLO alerts + OTel tracing + correlation-id; scrub strips internal terms from emitted events. (`api/internal/metrics/deploy_timeline.go:22`)
- **sealos:** Core reconcilers use zap structured logging + metering to DB; observability split into dedicated services (hubble network-obs, vlogs, exceptionmonitor). No Prometheus metrics in reconcilers read. (`controllers/pkg/utils/logger/logger.go:127`)
- **Analysis:** loopzero emits rich in-process Prometheus metrics + tracing where sealos reconcilers rely on logging + DB metering. Conversely sealos ships dedicated observability SERVICES (hubble/vlogs/exceptionmonitor) loopzero lacks. Different strengths; net even-to-slightly-loopzero on the control-plane layer.
- **Suggestion:** Maintain Prometheus coverage. Consider whether a dedicated log/exception service (cf. sealos service/vlogs, service/exceptionmonitor) is worth it at loopzero's scale.

### Folder Organization & Naming — **4.0/5** (architecture, weight 1.0)

- **loopzero:** api/internal has 33 descriptively-named packages (placement, cellmgr, scrub, gitops, sse); operators/internal per-domain. (`api/internal:1`)
- **sealos:** controllers/ + service/ split by domain; each domain self-contained with api/ controllers/ pkg/. (`controllers/user:1`)
- **Analysis:** Both use package-by-feature with specific names (no utils/common dumping grounds). Comparable, both strong.
- **Suggestion:** No action — naming discipline is good on both sides.

### Build System & Dev Experience — **4.0/5** (tooling, weight 1.0)

- **loopzero:** Taskfile (~5k lines across taskfiles/) + Tilt hot-reload dev loop + helmfile L2 + k3d 4-cluster local = production-parity local env. (`Taskfile.yml:1`)
- **sealos:** Per-module go.mod + scripts/detect-build-modules.sh + resolve-modules.sh feed a CI build matrix; install.sh orchestrates binary install. (`scripts/detect-build-modules.sh:20`)
- **Analysis:** Both mature. loopzero optimizes the inner dev loop (Tilt + k3d parity); sealos optimizes modular CI (per-module matrix builds). loopzero's 'local equals production' is a strong DX invariant.
- **Suggestion:** No action. If CI build times grow, sealos's per-module matrix (resolve-modules.sh:9) is a proven pattern to borrow.

### Configuration Management — **4.0/5** (configuration, weight 0.8)

- **loopzero:** Dual chart system (charts/ OCI + infra/helm bootstrap) + helmfile env overlays + Pulumi→Terraform migration. Documented overlap risk on 3 charts. (`charts:1`)
- **sealos:** Per-controller Helm chart with values.yaml (security context, probes, resource limits) + conditional feature gates. (`controllers/user/deploy/charts/user-controller/values.yaml:1`)
- **Analysis:** Comparable. sealos co-locates each controller's chart with the controller (deploy/charts); loopzero centralizes charts but runs a dual bootstrap/OCI system with a documented 3-chart overlap maintenance hazard.
- **Suggestion:** Watch the dual-chart overlap (loopzero-api/loopzero-cloud/loopzero-admission-policies duplicated). sealos's co-located per-component chart (user-controller/deploy/charts) reduces that drift risk.

### Module Boundaries & Layering — **3.5/5** (architecture, weight 1.5)

- **loopzero:** Single go.work groups api/operators/pkg as 7 coarse modules; one toolchain. (`go.work:4`)
- **sealos:** controllers/go.work declares 12 independent per-domain modules with replace-pinned k8s versions. (`controllers/go.work:1`)
- **Analysis:** sealos enforces harder compile-time boundaries (each controller a module); loopzero trades that for a simpler single-workspace build. Neither is wrong — sealos suits many independent teams; loopzero suits one team + faster builds.
- **Suggestion:** Keep single workspace unless cross-operator coupling bites. If operators/ grows independent release cadences, consider per-operator modules. Ref controllers/go.work:1-26.

### Metering & Billing Pipeline — **3.5/5** (domain, weight 1.0)

- **loopzero:** CONSOLIDATED: loopzero has mature billing PLUMBING — usage_records (append-only, →Stripe) + cost_counters (mutable, →402 enforcement) fed by an aggregator (migration 000017), + ComputeMonthlyCost server-authoritative estimate (scale.go:135) + idempotent Stripe webhooks. But METERING is partial: 3 metrics (egress/docr-pulls/build-minutes) + flat tiers; NO compute-resource (CPU/mem/storage) metering loop. (`api/internal/db/migrations/000017_cost_counters.up.sql:1`)
- **sealos:** sealos continuous monitor_controller meters CPU/mem/PVC/service/objstorage per namespace into time-series + data-driven UnitPrice; simpler MongoDB-insert billing plumbing. (`controllers/resources/controllers/monitor_controller.go:393`)
- **Analysis:** Refined 3.0→3.5 after phase-4 discovery: loopzero's billing plumbing (separate history/enforcement tables + Stripe reporting + idempotent webhooks) is arguably MORE mature than sealos's MongoDB inserts. The genuine gap is the per-namespace compute-resource metering reconcile loop + data-driven pricing (see HIGH/MEDIUM gaps). Net combined score: strong billing, partial metering = 3.5.
- **Suggestion:** Add the compute-resource metering loop (HIGH gap) + data-driven pricing (MEDIUM gap), reusing loopzero's existing usage_records/cost_counters tables. See controllers/resources/controllers/monitor_controller.go:393 + resources.go:281.

---

## 5. Improvement roadmap (prioritized, with effort)

| Priority | Action | Effort | Dimension | Reference blueprint |
|---|---|---|---|---|
| P0 (HIGH) | Add a periodic per-tenant-namespace compute-resource metering loop (CPU/mem/storage) writing cpu_core_seconds/mem_gb_seconds/storage_gb_hours into the existing usage_records + cost_counters tables | Medium (tables + aggregator + namespaces already exist) | Metering & Billing | `controllers/resources/controllers/monitor_controller.go:393,204,283,458,405,299` |
| P1 (MEDIUM) | Extract pricing into a data-driven table (per-metric UnitPrice) instead of the hardcoded ComputeMonthlyCost formula; feed the metering loop | Low-Medium | Metering & Billing | `controllers/pkg/resources/resources.go:281` |
| P2 (LOW) | Extract a shared pkg/retry helper (or adopt wait.Backoff) and refactor the ad-hoc retry call sites | Low (DRY) | Error Handling | `controllers/pkg/utils/retry/retry.go:22` |
| P3 (LOW) | Evaluate a runtime exception-monitor watcher if dogfood reveals gaps in threshold-based SLO alerts | Low (optional) | Observability | `service/exceptionmonitor` |
| P4 (LOW) | Revisit per-operator Go modules ONLY if operators acquire independent release cadences | Trade-off (not a defect) | Module Boundaries | `controllers/go.work:1` |

---

## 6. Where loopzero EXCELS over sealos

- **loopzero has a typed-error system; reference uses plain error wrapping** — loopzero's pkg/errors.LoopZeroError (typed Code) + pkg/errors/http code→status registry + scrub of internal terms is materially more disciplined than sealos's fmt.Errorf %w wrapping with no typed-error package.  (vs `controllers/account/controllers/account_controller.go:335`)
- **loopzero has supply-chain signing (cosign) + SBOM (syft) + Vault Transit; reference does not surface these** — loopzero signs images with cosign, generates SBOMs with syft, and externalizes secret crypto via OpenBao Vault Transit (pkg/vault) with zeroize + redaction. sealos uses in-process AES-GCM (controllers/pkg/crypto/crypto.go:38) + HS256 JWT and no surfaced image signing / SBOM.  (vs `controllers/pkg/crypto/crypto.go:38`)
- **loopzero has formal external-contract discipline (idempotency, --json stability, exit codes)** — loopzero enforces Idempotency-Key middleware, a documented --json stability contract, permanent exit codes (0-5), and scrub of internal terms from output. sealos validates inputs (Gin binding:required) but exposes fewer formal contract guarantees.  (vs `service/account/api/card.go:88`)
- **Multi-cell isolation** — NetworkPolicy triad (deny-all/ingress/egress) + cross-cluster placement-once-route-many, beyond the reference's single-cluster namespace+RBAC+quota model.

---

## 7. What was NOT analyzed (honesty)

- **sealos's TypeScript surface** (2,113 TS files: cloud desktop, app-store providers, per-app frontends) — loopzero has no equivalent (CLI-first). Not scored. sealos's 36 TS test specs are also not counted in the Testing dimension (which compared Go-side coverage only).
- **sealos's Rust services** (`service-rs/`, 9 files) — not analyzed.
- **Product-scope features** sealos has and loopzero does not by design: managed databases (`service/database`), cloud IDE (`controllers/devbox`/`terminal`), AI model gateway (`service/aiproxy`). Registered as an informational finding, NOT a gap.
- **Runtime behavior** — this is a static source comparison. No benchmarks, no live load, no failure-injection were run on either project.
- **sealos internals not read in depth** — only the controllers/services/scripts relevant to the 12 dimensions were inspected (7 reference files marked inspected of 24 cited; sealos has 3,895 files). Findings are evidence-anchored but not exhaustive.
- **loopzero's own dashboard/CLI TS tests** — the Testing dimension scored Go-side breadth; loopzero's 1,260 CLI Jest cases (per CLAUDE.md) were not independently re-counted here.

---

*Evidence base: 12 dimensions · 12 scored comparisons · 5 gaps · 10 findings · 24 cited reference files · 4 quality gates passed · 7,169 files inventoried. Database: `cross-validation-output/cross-validation.db` (source of truth).*
