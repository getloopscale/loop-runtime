# Monitoring & Observability

Centralized observability stack for the LoopZero platform. All system components — API, operators, ArgoCD, Argo Workflows, Gitea, MinIO, Traefik, PostgreSQL — are scraped by a single Prometheus instance and visualized through Grafana dashboards with alerting rules.

The code itself is the spec.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         MGMT CLUSTER (k3d-loopzero-core)                    │
│                                                                         │
│  ┌─────────────┐    scrape     ┌──────────────────┐                     │
│  │ Prometheus   │◄────────────►│ loopzero-api          │ ServiceMonitor      │
│  │ (monitoring) │◄────────────►│ PostgreSQL        │ Chart ServiceMonitor│
│  │              │◄────────────►│ Gitea             │ ServiceMonitor      │
│  │              │◄────────────►│ MinIO             │ ServiceMonitor      │
│  │              │◄────────────►│ ArgoCD (×3)       │ Chart ServiceMonitor│
│  │              │◄────────────►│ Tenant Controller │ ServiceMonitor      │
│  │              │              └──────────────────┘                     │
│  │              │                                                       │
│  │              │  additionalScrapeConfigs                              │
│  │              │◄──── host.k3d.internal:30090 ─── Argo Workflows      │
│  │              │◄──── host.k3d.internal:30091 ─── Traefik             │
│  │              │              │                                        │
│  └──────┬───────┘              │                                        │
│         │                      │                                        │
│         ▼                      ▼                                        │
│  ┌─────────────┐        ┌──────────────┐    ┌───────────────┐          │
│  │ Alertmanager │        │ Grafana       │    │ OTel Collector │          │
│  │ :30093       │        │ :3002         │    │ :4317 (gRPC)   │          │
│  └─────────────┘        │ 6 dashboards  │    │ :4318 (HTTP)   │          │
│                          └──────────────┘    └───────────────┘          │
└─────────────────────────────────────────────────────────────────────────┘

┌──────────────────────────┐   ┌──────────────────────────────┐
│ BUILD CLUSTER            │   │ WORKLOAD CLUSTER              │
│ (k3d-loopzero-build)         │   │ (k3d-loopzero-cell-0)           │
│                          │   │                                │
│  Argo Workflows          │   │  Traefik                      │
│  controller :9090        │   │  metrics  :9100                │
│      │                   │   │      │                         │
│      ▼                   │   │      ▼                         │
│  NodePort :30090 ────────┼───┼── NodePort :30091 ─────────── │
│  (metrics-service)       │   │  (metrics-service)             │
└──────────────────────────┘   └──────────────────────────────┘
```

### Scraping Strategy

| Component | Cluster | Method | How |
|-----------|---------|--------|-----|
| loopzero-api | mgmt | ServiceMonitor | `servicemonitor.yaml` → port `http` (3001) |
| PostgreSQL | mgmt | Chart ServiceMonitor | `metrics.serviceMonitor.enabled: true` in Bitnami chart |
| ArgoCD | mgmt | Chart ServiceMonitor | `metrics.serviceMonitor.enabled: true` on server/controller/repoServer |
| Gitea | mgmt | ServiceMonitor | `servicemonitor-gitea.yaml` → port `http` (3000) |
| MinIO | mgmt | ServiceMonitor | `servicemonitor-minio.yaml` → port `http` (9000), path `/minio/v2/metrics/cluster` |
| Tenant Controller | mgmt | ServiceMonitor | `servicemonitor-tenant-controller.yaml` → port `metrics` (8080) |
| Argo Workflows | build | additionalScrapeConfigs | NodePort 30090 → `host.k3d.internal:30090` |
| Traefik | workload | additionalScrapeConfigs | NodePort 30091 → `host.k3d.internal:30091` |

**Why two methods?** ServiceMonitors only work within the same cluster as Prometheus. For cross-cluster scraping (build and workload clusters), we expose metrics via NodePort Services and use Prometheus `additionalScrapeConfigs` to reach them through the k3d host network.

---

## Dashboards

Six Grafana dashboards organized in a hub-and-spoke navigation pattern. All dashboards are stored as JSON in `dashboards/` and loaded via Grafana sidecar (ConfigMap with label `grafana_dashboard=1`).

```
                     ┌──────────────┐
                     │  System Home │
                     │  (loopzero-home) │
                     └──┬──┬──┬──┬─┘
          ┌─────────────┘  │  │  └──────────────┐
          ▼                ▼  ▼                  ▼
   Platform Overview    Build Pipeline    Drift Detection
   Per-Tenant Health                      Failure Investigation
          │                │                     │
          └────────────────┴─────────────────────┘
                    ← Home (all link back)
```

### Dashboard Index

| Dashboard | UID | File | Purpose |
|-----------|-----|------|---------|
| **System Home** | `loopzero-home` | `home.json` | Health of all components at a glance. Entry point for operators. |
| **Platform Overview** | `loopzero-platform-overview` | `platform-overview.json` | Build success rate, deploy latency, API error rate, drift count. |
| **Build Pipeline** | `loopzero-build-pipeline` | `build-pipeline.json` | Build duration percentiles, queue depth, step breakdown. |
| **Per-Tenant Health** | `loopzero-per-tenant-health` | `per-tenant-health.json` | Tenant-scoped metrics: builds, quotas, active previews. |
| **Drift Detection** | `loopzero-drift-detection` | `drift-detection.json` | Drift events timeline, mirror status, ArgoCD sync, reconciler health. |
| **Failure Investigation** | `loopzero-failure-investigation` | `failure-investigation.json` | Error timeline, lock contention, dependency health, DB connections. |

### Home Dashboard Panels

| Row | Panels |
|-----|--------|
| Component Health | API, PostgreSQL, Gitea, ArgoCD, Argo Workflows, MinIO, Traefik, Active Alerts |
| API & Builds | Requests/s, Latency p95, Active Builds, Build Queue |
| ArgoCD & Deployments | Sync Status (pie), Deploy Duration p95, Lock Conflicts, Idempotency |
| Infrastructure | PostgreSQL Connections, Traefik Requests/s, MinIO Storage, SSE Connections |

---

## Alerting Rules

23 alerting rules defined in two PrometheusRule resources. Every rule has a `runbook` annotation with diagnosis steps.

### Critical (11 rules) — `prometheusrule-critical.yaml`

| Alert | Expression | For | Trigger |
|-------|-----------|-----|---------|
| `LoopZeroDriftDetected` | `loopzero_drift_detected == 1` | 5m | Cluster state diverges from Git |
| `LoopZeroBuildClusterUnreachable` | `up{job="argo-workflows"} == 0` | 2m | Build cluster down |
| `LoopZeroPostgresDown` | `pg_up == 0` | 1m | Database unreachable |
| `LoopZeroPostgresBackupStale` | `time() - pg_last_backup_timestamp > 90000` | 0m | Backup older than 25h |
| `LoopZeroGiteaDown` | `up{job=~".*gitea.*"} == 0` | 2m | Git server unreachable |
| `LoopZeroGiteaBackupStale` | `time() - loopzero_gitea_last_backup_timestamp > 25200` | 0m | Backup older than 7h |
| `LoopZeroArgoCDCircuitOpen` | `loopzero_argocd_circuit_state == 1` | 0m | Circuit breaker tripped |
| `LoopZeroGitMirrorExhausted` | divergence + mirror FAILED | 3h | All mirror retries exhausted |
| `LoopZeroFallbackStuck` | fallback active + mirror FAILED | 3h | Emergency fallback with no recovery |
| `LoopZeroBuildEgressStale` | `loopzero_build_egress_connectivity == 0` | 5m | Build pods can't reach services |
| `LoopZeroBreakGlassAccess` | `loopzero_break_glass_sessions_active > 0` | 0m | Emergency direct cluster access |

### Warning (12 rules) — `prometheusrule-warning.yaml`

| Alert | Expression | For | Trigger |
|-------|-----------|-----|---------|
| `LoopZeroAPIHighLatency` | p99 > 5s | 5m | API response time degraded |
| `LoopZeroTenantQuotaNearLimit` | usage > 90% | 10m | Tenant approaching resource limits |
| `LoopZeroBuildQueueBacklog` | queue > 10 | 5m | Build queue growing |
| `LoopZeroReconcilerUnhealthy` | 5+ errors in 5m | — | Reconciler producing errors |
| `LoopZeroSecretSyncFailed` | 3+ mismatches in 15m | — | Secret sync failing |
| `LoopZeroTenantPodsStarved` | pending > 5m (p99) | — | Pods stuck pending |
| `LoopZeroMirrorSyncStale` | mirror FAILED | 1h | Mirror not recovering |
| `LoopZeroGitDesiredStateDivergence` | divergence without mirror issue | 10m | Unexpected Git state |
| `LoopZeroArgoCDAppCountWarning` | count > 200 | 5m | Approaching performance limit |
| `LoopZeroArgoCDAppCountCritical` | count > 400 | 5m | Performance degradation likely |
| `LoopZeroArgoCDSyncSlow` | sync p95 > 30s | 5m | Sync latency elevated |
| `LoopZeroMinIODown` | `up{job=~".*minio.*"} == 0` | 2m | Object storage unreachable |

### Alertmanager Routing

```
┌─────────────────────────┐
│ All alerts               │
├─────────┬───────────────┤
│ critical│ group_wait: 10s│ → receiver: critical (webhook)
│         │ repeat:    1h  │
├─────────┼───────────────┤
│ warning │ group_wait: 30s│ → receiver: default (webhook)
│         │ repeat:    4h  │
└─────────┴───────────────┘
group_by: [alertname, tenant]
```

In dev, receivers point to `/dev-null`. For production, replace with Slack/email/PagerDuty webhook URLs.

---

## File Structure

```
infra/helm/monitoring/
├── README.md                              ← this file
├── values.yaml                            ← kube-prometheus-stack + Alertmanager + additionalScrapeConfigs
├── dashboards/
│   ├── home.json                          ← System Home (entry point)
│   ├── platform-overview.json             ← Platform health
│   ├── build-pipeline.json                ← Build metrics
│   ├── per-tenant-health.json             ← Per-tenant drill-down
│   ├── drift-detection.json               ← GitOps drift monitoring
│   └── failure-investigation.json         ← Incident investigation
└── manifests/
    ├── servicemonitor.yaml                ← loopzero-api (existing)
    ├── servicemonitor-gitea.yaml          ← Gitea
    ├── servicemonitor-minio.yaml          ← MinIO
    ├── servicemonitor-tenant-controller.yaml ← Operator metrics
    ├── metrics-service-argo-workflows.yaml   ← NodePort for cross-cluster (build)
    ├── metrics-service-traefik.yaml          ← NodePort for cross-cluster (workload)
    ├── prometheusrule-critical.yaml       ← 11 critical alerts
    └── prometheusrule-warning.yaml        ← 12 warning alerts
```

---

## Usage

### Install everything

```bash
task monitoring:install
```

This runs `infra:install:monitoring` (Helm charts) followed by `dashboards:install` (ConfigMaps, ServiceMonitors, PrometheusRules, cross-cluster Services).

### Validate targets

```bash
task monitoring:validate-targets
```

Checks the Prometheus API and reports whether all scrape targets are UP. Exits with code 1 if any target is DOWN.

### Check status

```bash
task monitoring:status
```

Shows three sections: Prometheus targets (with last scrape time), active alerts, and Grafana dashboards.

### Access UIs

| UI | URL | Credentials |
|----|-----|-------------|
| Grafana | `http://localhost:3002` | admin / `kubectl -n monitoring get secret grafana -o jsonpath='{.data.admin-password}' \| base64 -d` (T1.3: chart-generated random) |
| Prometheus | `http://localhost:9090` | — |
| Alertmanager | `http://localhost:30093` | — |

---

## Extending

### Adding a new metric to the API

1. Define the metric in the domain file under `api/internal/metrics/` (e.g. `build_metrics.go`)
2. Register and emit it from the appropriate service/middleware
3. Write a test in `api/internal/metrics/metrics_test.go`
4. Add a panel to the relevant dashboard JSON
5. If it needs an alert, add a rule to the appropriate PrometheusRule

### Adding a new dashboard

1. Create `dashboards/<name>.json` (use an existing dashboard as template)
2. Set a unique `uid` field (convention: `loopzero-<name>`)
3. Add a link to Home in `home.json` and a "Back to Home" link in the new dashboard
4. Run `task dashboards:install` to deploy

### Adding a new alert rule

1. Add the rule to `prometheusrule-critical.yaml` or `prometheusrule-warning.yaml`
2. Include `severity` label and `runbook` annotation
3. Run `task dashboards:install` to apply
4. Verify in Prometheus UI → Status → Rules

### Adding a new scrape target (same cluster)

1. Create `manifests/servicemonitor-<name>.yaml`
2. Set `namespace: monitoring` and label `release: monitoring`
3. Point `namespaceSelector` and `selector` to the target Service
4. Add the file to the `kubectl apply` list in `dashboards:install` task

### Adding a new scrape target (different cluster)

1. Create a NodePort Service in `manifests/metrics-service-<name>.yaml`
2. Add the port mapping to the appropriate `infra/k3d/<cluster>.yaml`
3. Add a `static_configs` entry under `additionalScrapeConfigs` in `values.yaml`
4. Add the `kubectl apply` command (with correct `--context`) to `dashboards:install`
5. Recreate the k3d cluster for the port mapping to take effect

---

## Production Considerations

| Topic | Dev (current) | Production |
|-------|--------------|------------|
| Cross-cluster scraping | NodePort + host.k3d.internal | Thanos sidecar + remote-write or Prometheus federation |
| Alertmanager receivers | Webhook to /dev-null | Slack, PagerDuty, or email via secrets |
| Dashboard provisioning | ConfigMap + sidecar | Same pattern (GitOps) or Grafana Terraform provider |
| Storage | 2Gi PVC, 15d retention | Sized per ingestion rate, consider Thanos for long-term |
| Grafana auth | Static admin password | SSO via OAuth2 (GitHub, OIDC) |
| High availability | Single replica | 2+ replicas for Prometheus, Alertmanager, Grafana |
| TLS | None (dev) | cert-manager certificates on all endpoints |

---

## Observability Coverage

| Area | Requirement | Status |
|------|-------------|--------|
| Three pillars | Metrics, traces, logs correlated | Metrics ✅, Traces infra ready, Logs pending |
| Key metrics | 109 loopzero_* metrics emitting | API metrics ✅, Build/Deploy/Drift metrics emit when features active |
| Correlation ID | End-to-end request tracing | Infrastructure exists, not yet validated E2E |
| SLOs | Dashboard panels reflect SLO targets | ✅ |
| Alerting rules | 23 rules (11 critical + 12 warning) | ✅ |
| Dashboards | 6 dashboards (Home + 5 domain-specific) | ✅ |
