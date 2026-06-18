# loopzero-app-operator — Config manifest (install-config-contract)

Non-secret config the `loopzero-app-operator` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Per-binary ISP — only the env vars THIS operator's binary reads. Secrets: this operator reads **no chart secret** (config-only).

Set-equal to the rendered ConfigMap keys (hermetic Go guard).

| Key | Default | Description |
|---|---|---|
| `BASE_DOMAIN` | (empty) | operator runtime config |
| `LOOPZERO_REQUIRE_RUNTIME_KUBECONFIG` | `false` | operator runtime config |
| `LOOPZERO_REAPER_GRACE_PERIOD_SECONDS` | `300` | operator runtime config |
| `LOOPZERO_PPROF_ADDR` | (empty) | operator runtime config |
| `LOOPZERO_PPROF_ENABLED` | `false` | operator runtime config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | operator runtime config |
| `LOOPZERO_ENV` | `production` | operator runtime config |
| `LOOPZERO_TRACING_ENABLED` | `false` | operator runtime config |

## Deliberately NOT chart-ConfigMap keys (deployment-set / platform-mounted)

- **RUNTIME_KUBECONFIG / BUILD_KUBECONFIG** — file-PATH values set directly on the Deployment `env:`; the kubeconfig **content** is mounted as a volume from the platform Secret `loopzero-cluster-kubeconfigs` (not a chart ConfigMap/Secret key). EC-1.
