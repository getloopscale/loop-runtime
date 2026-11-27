# loop-runtime-preview-operator — Config manifest (install-config-contract)

Non-secret config the `loop-runtime-preview-operator` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Per-binary ISP — only the env vars THIS operator's binary reads. Secrets: this operator reads **no chart secret** (config-only).

Set-equal to the rendered ConfigMap keys (hermetic Go guard).

| Key | Default | Description |
|---|---|---|
| `INGRESS_NAMESPACE` | `traefik` | operator runtime config |
| `LOOP_RUNTIME_PPROF_ADDR` | (empty) | operator runtime config |
| `LOOP_RUNTIME_PPROF_ENABLED` | `false` | operator runtime config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | operator runtime config |
| `LOOP_RUNTIME_ENV` | `production` | operator runtime config |
| `LOOP_RUNTIME_TRACING_ENABLED` | `false` | operator runtime config |

## Deliberately NOT chart-ConfigMap keys (deployment-set / platform-mounted)

- **CELL_0_KUBECONFIG** — file-PATH value set directly on the Deployment `env:`; the kubeconfig **content** is mounted as a volume from the platform Secret `loop-runtime-cluster-kubeconfigs` (not a chart ConfigMap/Secret key). EC-1. (`BUILD_KUBECONFIG` is read only by the tenant binary.)
