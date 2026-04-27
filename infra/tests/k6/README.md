# LoopZero — k6 Load Testing Suite

Industry-standard load testing for the LoopZero platform using
[k6](https://k6.io), replacing the previous shell-script smoke tests
that could not measure client-side latency distributions.

## Why k6

Previous approach: bash + curl + jq in a Kubernetes Job, looping
sequentially through tenants. This produced a boolean pass/fail but
NOT:

- Latency distributions (p50, p95, p99) measured at the client
- Coordinated-omission-safe scheduling (request rate independent of
  response time — critical at L3+ where slow requests skew sequential
  loops)
- Virtual-user ramp-up / steady-state / ramp-down separation
- Real-time Prometheus metrics via remote-write
- Structured JSON / HTML reports
- Thresholds enforced in the script so CI can fail fast

k6 gives us all of the above in a single ~10-line script, runs as a
standalone binary or a Kubernetes Job, and its output integrates with
our existing Grafana stack via the `xk6-output-prometheus-remote`
extension.

## Suite layout

```
k6/
├── README.md                    ← this file
├── scripts/
│   ├── lib/
│   │   ├── loopzero-api.js          ← shared API client (auth headers, retry)
│   │   └── thresholds.js        ← SLO thresholds from docs/capacity/cell-budget.md
│   ├── l1-10-tenants.js         ← L1 smoke (10 VUs, 5 min)
│   ├── l2-50-tenants.js         ← L2 sustained (50 VUs, 30 min)
│   ├── l3-100-tenants.js        ← L3 sustained (100 VUs, 60 min)
│   ├── stress-break-point.js    ← Ramp 0→500 VUs until SLO breaks
│   └── soak-l2-6h.js            ← 6-hour soak at L2 to catch leaks
├── fixtures/
│   └── bundle-builder.js        ← Creates the tarball fixture each VU pushes
└── job-template.yaml            ← k8s Job template for running in-cluster
```

## Thresholds (from cell-budget.md)

Every script enforces these thresholds in its `export const options`
block. A failed threshold fails the k6 exit code, which fails the
Kubernetes Job, which fails CI.

```js
thresholds: {
  http_req_failed:   ['rate<0.01'],         // < 1% failure
  http_req_duration: ['p(99)<600000'],      // p99 < 600s deploy budget
  'build_ok_rate':   ['rate>0.99'],         // ≥ 99% builds succeed
  'deploy_duration': ['p(95)<300000'],      // p95 < 5 min
}
```

## Running

### Locally (quick smoke)

```bash
# Dependency: k6 >= 0.52
brew install k6   # or download from https://k6.io/docs/get-started/installation/

export K6_API_URL=https://api.dev.loopzero.example
export K6_SVC_AUTH=dev-service-auth-secret-default
k6 run scripts/l1-10-tenants.js
```

### In-cluster (L2+)

```bash
# Apply the k6-operator CRD once per cluster
kubectl apply -f https://raw.githubusercontent.com/grafana/k6-operator/main/bundle.yaml

# Run L2 suite
kubectl apply -f job-template.yaml
kubectl logs -n loopzero-system -l app=k6-runner -f
```

### Continuous monitoring (soak test)

```bash
# 6-hour soak catches memory leaks + file-descriptor exhaustion
kubectl apply -f manifests/soak-l2-cronjob.yaml
```

## Comparison vs previous shell-script approach

| Capability | Shell + curl (old) | k6 (new) |
|---|---|---|
| p50 / p95 / p99 latency | ❌ | ✅ |
| Coordinated omission | ❌ (sequential) | ✅ (arrival-rate scheduling) |
| VU ramp-up / ramp-down | ❌ | ✅ (`stages`) |
| Prometheus remote-write | ❌ | ✅ via xk6 |
| Thresholds block CI | ❌ (boolean only) | ✅ (enforced exit code) |
| JSON / HTML report | ❌ | ✅ `--out json` |
| Resource cost per VU | High (1 shell process) | Low (1 goroutine) |
| Typical concurrency ceiling | ~50 | ~10k |

## Roadmap 100k DoD coverage

- **Item 04** `TestL2_50Tenants` → `l2-50-tenants.js`
- **Item 04** `TestArgoCD_100Apps_SyncTime` → `l3-100-tenants.js`
- **Item 04** `TestBuildQueue_20Concurrent` → `stress-break-point.js` (first plateau)
- **Item 07** `TestGracefulDegradation_150Percent` → `stress-break-point.js` (past red line)
- **Item 08** L2/L3/L4/L5 → `l2/l3/l4/l5-*.js` (L4-L5 require 2nd cell, Item 05)

## References

- k6 official docs: https://k6.io/docs/
- xk6-output-prometheus-remote: https://github.com/grafana/xk6-output-prometheus-remote
- Coordinated omission explained: https://redhatperf.github.io/post/coordinated-omission/
- Argo Workflows stress tool: https://github.com/argoproj/argo-workflows/tree/main/test/stress
