# Helm Chart Render-Time Tests

Tier 2 of the chart validation pipeline. Renders each chart with
`gruntwork-io/terratest`, unmarshals into typed K8s API structs, and
asserts structural invariants. **Catches drift between `helm lint`
passing and chart deploying broken structures (selector orphans,
security context gaps, registry typos, etc.).**

Plan: `git show 4f728eb7a:docs/plans/completed/2026-05-20-helm-render-time-testing-terratest-plan.md`

## Quick start

```bash
# Build gate (fast, ~5s) — catches helpers-API drift
task helm:test:vet

# Full suite (~5min, 20 packages, ~190 assertions)
task helm:test

# Single chart (~1-30s) — fastest iteration loop
task helm:test:chart -- loopzero-api
task helm:test:chart -- harbor
```

## Why a separate test tier?

| Tier | What it validates | Tool | When it fails |
|------|-------------------|------|---------------|
| 1 — Schema | `values.yaml` structure | `helm lint --strict` + `values.schema.json` | Wrong input types/missing required fields |
| **2 — Render (this)** | **Output structural correctness** | **`helm template` + Go assertions** | **Wrong output shape (selector drift, security gap, etc.)** |
| 3 — LIVE drill | Real cluster behavior under load | `task drill:l2-scale` | Race conditions, real-resource timing |

Tier 1 passing doesn't imply tier 2 passing: a chart can lint clean
while emitting a Deployment whose selector doesn't match its pod
template (orphaned ReplicaSet on every redeploy), or whose container
lacks `runAsNonRoot` (policy-controller rejects).

## Test layout

```
infra/tests/helm/
├── helpers/              ← Shared utilities (build-gate, render, parse, dep cache)
│   ├── helpers.go
│   ├── helpers_test.go   ← Self-tests for helpers
│   └── build_gate_test.go ← Meta-defense: go vet ./helm/...
├── values/               ← Plain-YAML env stubs (dev/drill/prod)
│   ├── dev/loopzero-api.yaml
│   ├── drill/loopzero-api.yaml
│   ├── prod/loopzero-api.yaml
│   └── README.md         ← Sync protocol
├── loopzero-api/             ← One package per chart
│   ├── defaults_test.go  ← Microsoft 2026 6 categories
│   ├── dev_test.go       ← Per-env stub validation
│   ├── drill_test.go
│   └── prod_test.go
└── <17 more chart pkgs>
```

Every test file has `//go:build helm_render` so it's isolated from the
default test runner — they only execute under
`go test -tags helm_render ./helm/...`.

## Pattern Catalog

Six reusable patterns, applied based on chart shape:

### Pattern 1 — Single-deployment chart (loopzero-api, loopzero-cloud, loopzero-dashboard)

Apply Microsoft 2026 6 categories:
- Identity & Labels — name, selector matches pod, helm labels present
- Configuration — envFrom/env present
- Container — image registry format, port matches binary bind addr
- Security — non-root pod + hardened container (or document exemption)
- Reliability — liveness + readiness + resources.limits
- Networking & Scaling — replicas explicit, port matches Service

```go
func renderDefaultDeployment(t *testing.T) appsV1.Deployment {
    chartPath, _ := helpers.ChartPath(chartName)
    opts, _ := helpers.NewOptions(chartPath, "")
    out := helpers.RenderTemplate(t, opts, chartPath, releaseName,
        []string{"templates/deployment.yaml"})
    var d appsV1.Deployment
    helpers.MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)
    return d
}
```

### Pattern 2 — Multi-replica operator (loopzero-tenant/app/platform/preview-operator)

One table-driven suite, `operatorcharts/`, covers all four: they share one
Deployment shape, so a new invariant is one check and a new operator is one row
(`task helm:test:chart -- operatorcharts`). Same as Pattern 1 plus:
- `--leader-elect=true` arg present
- Health (8081) + metrics (8443) ports
- ServiceAccount + ClusterRole + ClusterRoleBinding present

### Pattern 3 — CRD bundle (loopzero-crds)

Use `helpers.SplitYAMLByKind(out, "CustomResourceDefinition")` to get
all CRDs as a slice. Assert:
- Expected count matches (drift detector)
- No orphan CRDs (regression guard for deletion ADRs)
- All use `apiextensions.k8s.io/v1`
- All have scope + exactly one storage version
- Names follow `<plural>.<group>` convention

### Pattern 4 — Policy collection (loopzero-admission-policies)

For N-VAP + N-Binding charts:
- VAP count == Binding count (1:1 pairing)
- Binding policyName references existing VAP
- VAPs use `admissionregistration.k8s.io/v1`
- Pod-targeting VAPs with `failurePolicy: Fail` MUST exempt system
  actors in `matchConditions` (Bug #33 protection)

### Pattern 5 — Monitoring bundle (loopzero-monitoring)

For PrometheusRule + Dashboard ConfigMap collections:
- Rules count matches expected
- All rules have `release` label (Prometheus operator discovery)
- All alerts have severity label (critical|warning|info)
- All alerts have summary annotation (oncall context)
- Dashboards have `grafana_dashboard=1` label (sidecar discovery)
- Dashboard JSON parses + has `title`

### Pattern 6 — Umbrella (loopzero-platform)

Focus on umbrella-specific concerns:
- All sub-charts contribute when enabled (`task helm:test:chart -- loopzero-platform`)
- Disabling a sub-chart removes its contribution
  (e.g., `loopzero-crds.enabled=false` → 0 CRDs)
- No duplicate resource names per (kind, namespace, name) tuple
- Global value overrides propagate (e.g., `global.registry`)
- Render is deterministic (byte-identical across runs)

## Adding a new chart

1. Create `infra/tests/helm/<chart-name>/defaults_test.go`
2. Use the appropriate pattern above
3. `task helm:test:chart -- <chart-name>` until GREEN
4. `task helm:test:vet` to confirm no build breaks elsewhere
5. `task helm:test` to verify full suite still passes
6. Commit — CI workflow runs both gates automatically

For per-env tests (only when helmfile env values differ meaningfully):

1. Create `infra/tests/helm/values/<env>/<chart-name>.yaml` (plain YAML, no `{{`)
2. Create `infra/tests/helm/<chart-name>/<env>_test.go` referencing the stub
3. Assert env-specific invariants (registry, replicas, JWT issuer URL, etc.)

## Edge cases handled

- **EC-2 — chart with dependencies block:** `helpers.NewOptions` auto-invokes
  `helm dep update` (cached via `sync.Once` per chart path)
- **EC-3 — library chart:** `helpers.IsLibraryChart` returns true for charts
  with `type: library`; render-time tests are not applicable
- **EC-4 — empty containers slice:** every test asserts `NotEmpty` before
  indexing `Containers[0]`
- **EC-5 — chart path resolution from non-CWD:** `helpers.repoRoot` walks
  back from `runtime.Caller` location, independent of test working dir
- **EC-6 — registry exact value:** tests assert *structure* (OCI ref regex)
  not exact values
- **D10 — helmfile templated values:** `helpers.NewOptions` refuses any
  values file containing `{{` directives; redirects operator to plain-YAML stub
- **D11 — dependency resolution:** auto-invoked + cached per chart path
- **D12 — library exemption:** documented in helper + per-chart tests
- **B33 — VAP namespace deletion deadlock:** asserted on the one
  admission-policies chart (charts/loopzero-admission-policies)

## Helper API

| Function | Purpose |
|----------|---------|
| `ChartPath(name)` | Resolve chart name to absolute path (infra/helm/ then charts/) |
| `IsLibraryChart(path)` | Detect `type: library` (test exemption) |
| `NewOptions(chartPath, envValues)` | Build terratest helm.Options; runs `helm dep update` if needed |
| `RenderTemplate(t, opts, chartPath, release, templates)` | Thin wrapper around `helm.RenderTemplate` |
| `MustUnmarshalK8SYaml(t, out, &struct)` | Single-doc unmarshal (errors if multi-doc) |
| `MustUnmarshalK8SYamlByKind(t, out, "Deployment", &d)` | Multi-doc — pick first doc with matching Kind |
| `SplitYAMLByKind(out, "CRD")` | Multi-doc — return ALL matching docs as raw YAML for iteration |

## CI / CD integration

A `render-tests` job would run the following. This named `.github/workflows/helm-charts.yml` until 2026-09-11; that workflow exists in no commit, and Actions was retired as the pipeline in 2026-05-23:
1. Runs `task helm:test:vet` (~5s build gate)
2. Runs `task helm:test` (~5min full suite)
3. Gates `package-push`: broken charts cannot reach DOCR

Path triggers include `infra/helm/**` and `infra/tests/helm/**` so any
drift in either source or test code re-runs validation.

## Anti-patterns to avoid

- **Don't assert exact values** when structure is what matters. Use
  regex for image refs, not string equality.
- **Don't render the same chart 13 times** when 13 parallel tests share
  setup — use `t.Parallel()` + helpers `sync.Once` cache for deps.
- **Don't bypass build gate** by removing tests instead of fixing
  helpers symbol. Per the standing instruction "if you find a bug, fix it without a
  workaround" — fix the root, not the surface.
- **Don't add `// readOnlyRootFilesystem` exemption** without
  documenting WHY in the test (e.g., nginx needs writable /tmp).
- **Don't commit a test that passes only "by coincidence"** — e.g.,
  using `MustUnmarshalK8SYaml` on a multi-doc template silently parses
  the first doc; use `MustUnmarshalK8SYamlByKind` instead.

## Related plans

- **Phase 6 candidate (out of scope):** published-variant coverage for
  the 2 overlapping charts (loopzero-api + loopzero-cloud). Tracked as task #128.
