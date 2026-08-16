# loopzero-platform-operator — Config manifest (install-config-contract)

Non-secret config the `loopzero-platform-operator` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Per-binary ISP — only the env vars THIS operator's binary reads. Secrets: this operator reads **no chart secret** (config-only).

Set-equal to the rendered ConfigMap keys (hermetic Go guard).

| Key | Default | Description |
|---|---|---|
| `ARGOCD_NAMESPACE` | `argocd` | operator runtime config |
| `OPERATOR_INSECURE_SKIP_TLS` | `false` | operator runtime config |
| `LOOPZERO_PPROF_ADDR` | (empty) | operator runtime config |
| `LOOPZERO_PPROF_ENABLED` | `false` | operator runtime config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | operator runtime config |
| `LOOPZERO_ENV` | `production` | operator runtime config |
| `LOOPZERO_TRACING_ENABLED` | `false` | operator runtime config |

## Deliberately NOT chart-ConfigMap keys (deployment-set / platform-mounted)

- **(none)** — the `loopzero-platform-operator` binary reads no kubeconfig env var and mounts no `loopzero-cluster-kubeconfigs` volume. `CELL_0_KUBECONFIG`/`BUILD_KUBECONFIG` are read only by the tenant/app/preview binaries. EC-1.
