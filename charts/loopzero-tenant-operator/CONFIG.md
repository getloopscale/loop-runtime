# loopzero-tenant-operator — Config manifest (install-config-contract)

Non-secret config the `loopzero-tenant-operator` chart injects into a **ConfigMap** (`envFrom configMapRef`).
Per-binary ISP — only the env vars THIS operator's binary reads. Secrets → [SECRETS.md](SECRETS.md).

Set-equal to the rendered ConfigMap keys (hermetic Go guard).

| Key | Default | Description |
|---|---|---|
| `ARGOCD_NAMESPACE` | `argocd` | operator runtime config |
| `GITEA_ORG` | `loopzero` | operator runtime config |
| `GITEA_URL` | (empty) | operator runtime config |
| `INGRESS_NAMESPACE` | `traefik` | operator runtime config |
| `MINIO_ENDPOINT` | (empty) | operator runtime config |
| `REGISTRY_CIDRS` | (empty) | operator runtime config |
| `LOOPZERO_TENANT_MAX_CONCURRENT` | `5` | operator runtime config |
| `WORKLOAD_CLUSTER_URL` | (empty) | operator runtime config |
| `OPERATOR_INSECURE_SKIP_TLS` | `false` | operator runtime config |
| `LOOPZERO_PPROF_ADDR` | (empty) | operator runtime config |
| `LOOPZERO_PPROF_ENABLED` | `false` | operator runtime config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (empty) | operator runtime config |
| `LOOPZERO_ENV` | `production` | operator runtime config |
| `LOOPZERO_TRACING_ENABLED` | `false` | operator runtime config |

## Deliberately NOT chart-ConfigMap keys (deployment-set / platform-mounted)

- **CELL_0_KUBECONFIG / BUILD_KUBECONFIG** — file-PATH values set directly on the Deployment `env:`; the kubeconfig **content** is mounted as a volume from the platform Secret `loopzero-cluster-kubeconfigs` (not a chart ConfigMap/Secret key). EC-1.
