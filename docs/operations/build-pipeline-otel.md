---
name: build-pipeline-otel
description: OpenTelemetry sampling policy for tenant build pipeline (Argo Workflows) — head-based 10% with LOOPZERO_BUILD_FORCE_TRACE escape hatch per ADR D3 (v1.1 of plan 2026-05-27-tenant-build-pipeline-gaps-closure).
type: operational-runbook
created_at: 2026-05-27
updated_at: 2026-05-27
updated_by: tenant-build-pipeline-gaps-closure plan T4.1
---

# Tenant Build Pipeline — OpenTelemetry Sampling

> **Per ADR D3 (v1.1) of plan 2026-05-27-tenant-build-pipeline-gaps-closure-plan.md.**
>
> Head-based 10% sampling for build workflows + `LOOPZERO_BUILD_FORCE_TRACE=true` escape hatch for forensics. NOT tail-based 100% on errors (incompatible with head-based decision timing per v1.1 EC-NEW-9 fix).

## Sampling Policy

| Path | Sampling rate | Rationale |
|------|---------------|-----------|
| Default (all builds) | **10% head-based** | Cost predictability; cluster cost linear with build volume |
| `LOOPZERO_BUILD_FORCE_TRACE=true` | **100% head-based** | Operator forensic flow (next deploy traced fully) |
| Errors at runtime | **10% of sampled** (90% not traced) | Tail-based would require collector buffering ALL spans = expensive |

### Why head-based 10% (NOT tail-based)

**Head sampling** decides at trace creation whether to record subsequent spans. Decision is final + cheap (no buffering).

**Tail sampling** requires the OTel collector to buffer EVERY span until the trace completes, then decide based on full trace properties (e.g., "did it error?"). Buffering costs scale with build volume × span count × buffer duration — at 20k tenants × 5 builds/day × 30 spans/build, this is 3M spans/day. Tail buffering also adds 2-5min latency to trace emission.

Head-based 10% trades forensic completeness for cost predictability:
- **Lost:** 90% of errors at runtime are NOT traced (operator can't see post-hoc what went wrong on that specific build)
- **Gained:** Cost linear with build volume; no surprise spikes during error storms; collector simple

### v1.1 EC-NEW-9 fix history

Original v1.0 ADR D3 said "head 10% + tail 100% on errors" — **physically incompatible** (head decision happens BEFORE error observable; tail requires full buffering). External /edge-case-plan review caught this contradiction. v1.1 picks head-based only with explicit escape hatch.

## Cost math

Reference: Tempo storage at $0.06/GB/month (DigitalOcean Spaces equivalent).

| Sampling rate | Builds/day | Spans/day | Bytes/day (1KB/span avg) | Monthly cost |
|---------------|------------|-----------|---------------------------|--------------|
| 100% | 100 | 3,000 | 3 MB | ~$0.05/mo |
| 100% | 500 (10x growth) | 15,000 | 15 MB | ~$0.27/mo |
| **10% (default)** | **100** | **300** | **0.3 MB** | **~$0.005/mo** |
| 10% | 500 | 1,500 | 1.5 MB | ~$0.03/mo |
| 100% (FORCE_TRACE) | 100 builds + 10 force-traced | 3,300 | 3.3 MB | ~$0.06/mo |

**At GA scale (20k tenants × 5 builds/day = 100,000 builds/day):**
- 100% sampling: ~3 GB/day = ~$5/mo
- 10% sampling: ~0.3 GB/day = ~$0.50/mo

**10x reduction in OTel storage cost — predictable + linear with build volume.**

## Escape hatch — operator forensic flow

When a specific build fails OR an operator needs trace forensics for a customer issue:

### Step 1 — Identify the build

```bash
# Get build_id from loopzero deploy output OR from Workflow CR
kubectl get workflows -n argo -l tenant_id=tn-X --sort-by=.metadata.creationTimestamp | tail -5
```

### Step 2 — Re-trigger with FORCE_TRACE

```bash
# Re-deploy with env var forcing 100% trace on next build
LOOPZERO_BUILD_FORCE_TRACE=true loopzero deploy --project my-app
```

OR via API:

```bash
curl -X POST https://api.loopzero.cloud/api/v1/builds \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "project": "my-app",
    "force_trace": true
  }'
```

### Step 3 — Find the new trace in Tempo

```bash
# Tempo TraceQL query — filter by build_id label
curl -G http://tempo.monitoring:3200/api/search \
  --data-urlencode 'q={ .build_id = "<NEW_BUILD_ID>" }' \
  --data-urlencode 'limit=50'
```

OR via Grafana Tempo UI:
- Datasource: Tempo
- Query: `{ .build_id = "<NEW_BUILD_ID>" }`

### Step 4 — Drill into failing span

Tempo trace view shows the full DAG. Errors appear in red. Click into the failing span for:
- Container logs (linked Loki)
- Resource utilization at failure moment
- Downstream dependencies (Argo Workflow link)

## Sampling implementation

### Where the sampling decision lives

Tenant build pipelines run as Argo Workflows. OTel sampling is configured at **2 layers**:

1. **Argo Workflows controller** (cluster-wide default):
   - File: `infra/helm/argo-workflows/values.yaml`
   - Config: `controller.telemetryConfig.otelEnabled: true` + `otelSamplingRatio: 0.1`

2. **Per-Workflow override** (via labels):
   - `metadata.labels.loopzero.io/force-trace: "true"` → controller bumps sampling to 100% for this Workflow
   - Set automatically by API when `LOOPZERO_BUILD_FORCE_TRACE=true` env var present at `loopzero deploy`

### Wiring `LOOPZERO_BUILD_FORCE_TRACE` env var (engineering deliverable)

API reads env var from CLI request → sets label on Workflow CR:

```go
// api/internal/services/build_orchestrator.go (illustrative)
if os.Getenv("LOOPZERO_BUILD_FORCE_TRACE") == "true" || req.ForceTrace {
    workflow.Labels["loopzero.io/force-trace"] = "true"
}
```

The Argo Workflows controller respects this label to bump sampling per-Workflow.

**Status:** Currently NOT wired. Operator can add via short PR (~30min) when needed. Documented here so the runbook flow exists even before code is shipped.

## Tempo retention

Build traces retained for **30 days** in Tempo (default). Forensic flow assumes operator triggers FORCE_TRACE re-build within 30d of original.

For longer retention: archive specific trace IDs to S3 via Tempo's `tempo-cli` (operator manual procedure).

## Anti-patterns

- ❌ **DON'T enable 100% sampling globally** — cost explosion at GA scale
- ❌ **DON'T add tail-based sampling later** — incompatible with head; would require pipeline refactor
- ❌ **DON'T rely on logs for forensics** — Loki retention shorter than Tempo; trace gives DAG context logs can't
- ✅ **DO use FORCE_TRACE escape hatch** — reproducible issues investigated cheaply
- ✅ **DO add span attributes for known forensic fields** — build_id, tenant_id, app_name, commit_sha (already in spans via loopzero-forge auto-instrumentation)

## Related

- ADR D3 (v1.1) of plan: `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-plan.md`
- Edge case review (v1.1 EC-NEW-9): `docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-edge-case-review.md`
- Argo Workflows OTel docs: https://argo-workflows.readthedocs.io/en/stable/metrics/
- OpenTelemetry sampling spec: https://opentelemetry.io/docs/concepts/sampling/
- Tempo TraceQL: https://grafana.com/docs/tempo/latest/traceql/
- Related runbooks: `docs/operations/sigstore-storage-model.md`
