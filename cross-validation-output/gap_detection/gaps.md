# Gap Analysis — what sealos has that loopzero lacks

> Honesty pass applied: every claim re-checked against loopzero source. Rate-limiting was NOT registered as a gap (loopzero has `api/internal/middleware/ratelimit.go`). Metering gap was narrowed — loopzero HAS `usage_records` + `cost_counters` + an aggregator; the specific gap is the per-namespace compute-resource reconcile loop.

## Actionable gaps (prioritized)

### 🔴 HIGH — Per-tenant-namespace compute-resource metering loop (CPU/mem/storage)
- **Reference:** `controllers/resources/controllers/monitor_controller.go:393` (`monitorResourceUsage`) — periodic reconcile sums pod CPU/mem (from `pod.Spec.Resources.Requests` = allocated capacity, AWS-style), PVC storage, backups, services, object storage per namespace into time-series records.
- **loopzero today:** flat per-plan tiers + 3 usage metrics (egress/docr-pulls/build-minutes); `ComputeMonthlyCost(replicas, podCostPerHour)` (`pkg/enforcement/scale.go:135`) is a UI **estimate only**. No loop meters actual reserved compute per tenant for billing.
- **Why it matters:** for a PaaS that scales workloads, revenue is decoupled from the compute footprint tenants reserve.
- **Adoption path (medium effort — loopzero already has tables + aggregator + namespaces):**
  1. Periodic loop — cf. `monitor_controller.go:204` (`startPeriodicReconcile`) + `:283` (`enqueueNamespacesForReconcile`).
  2. Per tenant ns sum `pod.Spec.Resources.Requests`×replicas + PVC sizes — cf. `:458` (`monitorPodResourceUsage`), `:405` (`monitorPVCResourceUsage`).
  3. Filter system namespaces — cf. `:299` (`filterNormalNamespace`).
  4. Write new metrics `cpu_core_seconds` / `mem_gb_seconds` / `storage_gb_hours` into the EXISTING `usage_records` (→Stripe) + `cost_counters` (→402 enforcement). No new storage.
  5. Price via the data-driven model below.

### 🟠 MEDIUM — Data-driven pricing model
- **Reference:** `controllers/pkg/resources/resources.go:281` — pricing is DATA (`UnitPrice` per metric); reprice/add a metric = config change.
- **loopzero today:** `ComputeMonthlyCost` hardcodes the formula; plan tiers enumerated in code.
- **Fix:** extract pricing into a table/config keyed by metric; feeds the metering loop. Enables repricing without code deploys.

### 🟡 LOW — Centralized retry/backoff helper
- **Reference:** `controllers/pkg/utils/retry/retry.go:22` (`Retry(tryTimes, sleep, action)`).
- **loopzero today:** ad-hoc retry at `cfzone/client.go`, `health/prober.go`, `services/container_delivery.go`; has `pkg/circuitbreaker` but no retry primitive.
- **Fix:** extract `pkg/retry` (or use `k8s.io/.../wait.Backoff` already imported); refactor call sites. DRY win.

### 🟡 LOW — Dedicated runtime exception-monitor (vs threshold alerts)
- **Reference:** `service/exceptionmonitor` (active watcher + notify).
- **loopzero today:** Prometheus SLO alerts + runbooks (reactive/threshold). OPTIONAL — adopt only if dogfood shows alert gaps.

### 🟡 LOW — Per-module versioning (trade-off, not a defect)
- **Reference:** `controllers/go.work:1` — 24+ independently-versioned modules.
- **loopzero today:** single coarse `go.work` (7 modules). Defensible KISS win; revisit only if independent release cadence is needed.

## Product-scope differences (intentional — NOT gaps)
- sealos: managed databases (`service/database`), cloud IDE (`controllers/devbox`/`terminal`), AI proxy (`service/aiproxy`). loopzero is CLI-first deploy by design.

## Where loopzero EXCELS over sealos
- **Typed errors** — `pkg/errors.LoopZeroError` + code→status registry vs sealos plain `%w`.
- **Test breadth** — 792 `_test.go` + 62 Ginkgo/envtest vs 161.
- **External contract discipline** — Idempotency-Key + `--json` stability + exit codes + scrub.
- **Supply chain** — cosign signing + syft SBOM + Vault Transit + zeroize/redaction.
- **Isolation** — NetworkPolicy triad + multi-cell cross-cluster placement.
