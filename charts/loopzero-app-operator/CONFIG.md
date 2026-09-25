# loopzero-app-operator — Config manifest (install-config-contract)

Non-secret config the `loopzero-app-operator` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Per-binary ISP — only the env vars THIS operator's binary reads. Secrets: this operator reads **no chart secret** (config-only).

Set-equal to the rendered ConfigMap keys (hermetic Go guard).

| Key | Default | Description |
|---|---|---|
| `BASE_DOMAIN` | (empty) | operator runtime config |
| `OPERATOR_INSECURE_SKIP_TLS` | `false` | operator runtime config |
| `LOOPZERO_REQUIRE_CELL_0_KUBECONFIG` | `false` | operator runtime config |
| `LOOPZERO_PPROF_ADDR` | (empty) | operator runtime config |
| `LOOPZERO_PPROF_ENABLED` | `false` | operator runtime config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | operator runtime config |
| `LOOPZERO_ENV` | `production` | operator runtime config |
| `LOOPZERO_TRACING_ENABLED` | `false` | operator runtime config |
| `LOOPZERO_DATA_PLANE_POLLER_ENABLED` | `false` | T2.2 of plan dashboard-badge-data-plane-poller (#128) — feature gate for DataPlanePoller Runnable |
| `LOOPZERO_DATA_PLANE_POLLER_PG_URL` | (empty) | PG DSN the poller UPDATEs env_targets with; required when ENABLED=true |
| `LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL` | (empty) | Prometheus base URL the poller queries; required when ENABLED=true |
| `LOOPZERO_DATA_PLANE_POLLER_INTERVAL` | `30s` | scrape cadence |

## Deliberately NOT chart-ConfigMap keys (deployment-set / platform-mounted)

- **CELL_0_KUBECONFIG** — file-PATH value set directly on the Deployment `env:`; the kubeconfig **content** is mounted as a volume from the platform Secret `loopzero-cluster-kubeconfigs` (not a chart ConfigMap/Secret key). EC-1. (`BUILD_KUBECONFIG` is read only by the tenant binary.)
