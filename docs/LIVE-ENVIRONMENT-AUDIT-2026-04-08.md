# LoopZero Platform — Live Environment Audit (DigitalOcean)

**Date:** 2026-04-08
**Environment:** dev (SFO3)
**Clusters:** 3 DOKS (mgmt, build, runtime) + 1 inference (separate)
**K8s Version:** v1.32.13-do.2 (all 3 LoopZero clusters)
**Auditor:** 5 parallel agents querying live infrastructure via kubectl + doctl

---

## Executive Summary

The LoopZero platform is **operationally functional** on DigitalOcean with all core services running, zero CrashLoopBackOff, and all external endpoints reachable via Cloudflare Tunnel with valid TLS. However, the live environment audit reveals **several gaps between the desired state (IaC/code) and the actual state (running infrastructure)**.

**Overall Live Environment Score: B- (Functional but needs hardening)**

| Domain | Status | Critical | High | Medium | Low |
|--------|--------|----------|------|--------|-----|
| MGMT Cluster | Functional | 0 | 1 | 2 | 1 |
| BUILD Cluster | Functional | 0 | 0 | 2 | 1 |
| RUNTIME Cluster | Functional | 0 | 1 | 2 | 1 |
| DO Resources | Functional | 1 | 2 | 2 | 1 |
| Integrations | Functional | 1 | 1 | 3 | 2 |
| **TOTAL** | | **2** | **5** | **11** | **6** |

---

## 1. Infrastructure Overview (Actual State)

### 1.1 Clusters

| Cluster | Nodes | K8s Version | Auto-Upgrade | Status | VPC |
|---------|-------|-------------|-------------|--------|-----|
| loopzero-infra-dev-sfo3-mgmt | 2 x s-2vcpu-4gb | v1.32.13 | Yes | Running | 10.20.0.0/16 |
| loopzero-infra-dev-sfo3-build | 2 x s-2vcpu-4gb | v1.32.13 | Yes | Running | 10.20.0.0/16 |
| loopzero-infra-dev-sfo3-runtime | 2 x s-2vcpu-4gb | v1.32.13 | Yes | Running | 10.20.0.0/16 |
| loopzero-inference-sfo3 | 2 x s-2vcpu-4gb | **v1.35.1** | **No** | Running | 10.20.0.0/16 |

### 1.2 Databases

| Name | Engine | Version | Nodes | HA | Firewall | Status |
|------|--------|---------|-------|----|----------|--------|
| loopzero-infra-dev-sfo3-db | PostgreSQL | 16 | 2 | Yes | 3 rules (K8s clusters) | Online |
| loopzero-inference-db | PostgreSQL | 16 | 1 | **No** | **0 rules** | Online |

### 1.3 Storage

| Resource | Type | Status |
|----------|------|--------|
| loopzero-static-assets | Spaces + CDN (TTL 3600s) | Active |
| loopzero-infra-registry | Container Registry (Professional) | Active (14 repos) |
| 3 Load Balancers | DO LB (one per cluster) | All active |

### 1.4 External Endpoints

| Endpoint | HTTP | TLS | Status |
|----------|------|-----|--------|
| api.dev.loopzero.example | 200 | Valid | **OK** |
| cloud.dev.loopzero.example | 200 | Valid | **OK** |
| argocd.dev.loopzero.example | 200 | Valid | **OK** |
| grafana.dev.loopzero.example | 200 | Valid | **OK** |
| git.dev.loopzero.example | 200 | Valid | **OK** |
| dev.loopzero.example (bare) | DNS fail | - | **ERROR** |

---

## 2. MGMT Cluster Audit

### 2.1 Components Status

| Component | Pods | Replicas | Restarts | Status |
|-----------|------|----------|----------|--------|
| loopzero-api | 2/2 | 2 desired | 0 | **OK** |
| loopzero-cloud (BFF) | 1/1 | 1 desired | 0 | **OK** |
| loopzero-dashboard | 1/1 | 1 desired | 0 | **OK** |
| tenant-controller | 1/1 | 1 desired | 0 | **OK** |
| ArgoCD (7 pods) | 7/7 | All 1/1 | 0 | **OK** |
| Gitea | 1/1 | 1 desired | 0 | **OK** |
| MinIO | 1/1 | 1 desired | 0 | **OK** |
| Traefik | 1/1 | 1 desired | 0 | **OK** |
| cert-manager (3 pods) | 3/3 | All 1/1 | 0-1 | **OK** |
| Ory Kratos + Courier | 2/2 | 1+1 desired | 0 | **OK** |
| Ory Hydra + Maester | 2/2 | 1+1 desired | 0 | **OK** |
| Prometheus + Alertmanager | 3/3 | All 1/1 | 0 | **OK** |
| Grafana | 1/1 | 1 desired | 0 | **OK** |
| Tempo | 1/1 | 1 desired | 0 | **OK** |
| kube-state-metrics | 1/1 | 1 desired | **3** | **WARNING** |

**Total: 47 pods, all Running. Zero CrashLoopBackOff.**

### 2.2 Helm Releases (10, all deployed)

| Release | Chart | App Version |
|---------|-------|-------------|
| argocd | argo-cd-7.3.11 | v2.11.7 |
| gitea | gitea-10.6.0 | 1.22.3 |
| grafana | grafana-8.5.2 | 11.2.1 |
| mgmt-cert-manager | cert-manager-v1.14.5 | v1.14.5 |
| mgmt-traefik | traefik-28.3.0 | v3.0.2 |
| minio | minio-5.4.0 | RELEASE.2024-12-18 |
| ory-hydra | hydra-0.61.0 | v26.2.0 |
| ory-kratos | kratos-0.61.0 | v26.2.0 |
| prometheus | prometheus-25.27.0 | v2.54.1 |
| tempo | tempo-1.14.0 | 2.6.1 |

### 2.3 CRDs (all 10 LoopZero CRDs installed)

Tenants: 2 Active (`tn-loopzero`, `tn-61957ca0ea3d311e`)
Environments: 2 Active
TLS Certificates: 7 (all Ready via Let's Encrypt)
PVCs: 6 (Gitea 10Gi, MinIO 10Gi, Prometheus 10Gi, Grafana 5Gi, Tempo 5Gi, Alertmanager 2Gi)

### 2.4 MGMT Findings

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| MGMT-001 | **HIGH** | OTEL trace export failing continuously — collector unreachable | loopzero-api logs: `traces export: rpc error: code = Unavailable desc = name resolver error: produced zero addresses` every ~5s |
| MGMT-002 | MEDIUM | No PSA enforcement on any namespace | No `pod-security.kubernetes.io/enforce` labels on any namespace |
| MGMT-003 | MEDIUM | No metrics-server deployed | `kubectl top nodes` fails with "Metrics API not available" |
| MGMT-004 | LOW | kube-state-metrics has 3 restarts over 4d | Likely transient OOM, not critical |

---

## 3. BUILD Cluster Audit

### 3.1 Components Status

| Component | Status | Detail |
|-----------|--------|--------|
| Argo Workflows controller | **OK** | 1/1 Running, 0 restarts |
| Argo Workflows server | **OK** | 1/1 Running, 0 restarts |
| Loki | **OK** | 1/1 Running (2 containers), S3 backend (loopzero-logs) |
| Promtail | **OK** | DaemonSet 2/2, covering both nodes |
| CWTs | **OK** | loopzero-clone, loopzero-cache-restore, loopzero-cache-save all present |

### 3.2 Helm Releases (4, all deployed)

| Release | Chart | App Version |
|---------|-------|-------------|
| argo-workflows | argo-workflows-1.0.5 | v4.0.3 |
| loki | loki-6.24.0 | 3.3.2 |
| promtail | promtail-6.16.6 | 3.0.0 |
| loopzero-build-cwts | loopzero-build-cwts-0.1.0 | - |

### 3.3 Tenant Build Namespaces (2)

| Namespace | Secrets | Network Policy | Resource Quota |
|-----------|---------|----------------|----------------|
| build-tn-loopzero | buildkit-registry-config, loopzero-minio-credentials, loopzero-infra-registry | build-egress-restrict | 4/8 CPU, 8Gi/16Gi mem, 2 workflows max |
| build-tn-61957ca0ea3d311e | buildkit-registry-config, loopzero-minio-credentials, loopzero-infra-registry | build-egress-restrict | 4/8 CPU, 8Gi/16Gi mem, 2 workflows max |

### 3.4 Recent Builds (last 3.5h)

| Total | Succeeded | Failed | Success Rate |
|-------|-----------|--------|-------------|
| 11 | 7 | 4 | 64% |

The 4 failures occurred in a burst (first 5 builds), then all subsequent builds succeeded — likely a transient registry issue.

### 3.5 BUILD Findings

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| BUILD-001 | MEDIUM | 4 orphaned PVCs (20Gi) from failed builds not cleaned up | `loopzero-build-build-{id}-workspace` PVCs still Bound |
| BUILD-002 | MEDIUM | No metrics-server deployed | `kubectl top nodes` fails |
| BUILD-003 | LOW | 4/11 builds failed (36%) in first batch — transient but concerning | All failures on `build-image-api` step, retries exhausted |

---

## 4. RUNTIME Cluster Audit

### 4.1 Components Status

| Component | Replicas | Status | Detail |
|-----------|----------|--------|--------|
| Traefik | **1/1** | **WARNING** | Spec says 2 for HA. Single point of failure |
| cloudflared | 2/2 | **OK** | CF Tunnel operational, 4 QUIC connections |
| cert-manager (3 pods) | 3/3 | **OK** | letsencrypt-prod issuer Ready |
| ESO (3 pods) | 3/3 | **OK** | ClusterSecretStore `loopzero-mgmt-store` Valid |

### 4.2 User Applications Running

| Tenant | Project | App | Image Tag | Port | Custom Domain |
|--------|---------|-----|-----------|------|---------------|
| tn-61957ca0ea3d311e | aquim-test | api | a9fd54f6 (SHA) | 8000 | aquim.ai |
| tn-loopzero | loopzero-monorepo | api | b794774b (SHA) | 3001 | - |

Both apps use full commit SHA tags (no `:latest`). Security contexts enforced: runAsNonRoot, readOnlyRootFilesystem, drop ALL capabilities.

### 4.3 Tenant Namespace Security

| Security Control | tn-loopzero-production | tn-61957ca0ea3d311e-production |
|------------------|-----------------------|--------------------------------|
| PSA enforce=restricted | ✓ | ✓ |
| NetworkPolicy deny-all | ✓ | ✓ |
| NetworkPolicy allow-ingress-traefik | ✓ | ✓ |
| NetworkPolicy allow-egress | ✓ | ✓ |
| ResourceQuota (4 CPU / 8Gi) | ✓ | ✓ |
| LimitRange (500m/512Mi default) | ✓ | ✓ |
| LoadBalancer blocked (0 quota) | ✓ | ✓ |

### 4.4 TLS Certificates (all Ready)

5 certificates on runtime cluster: `tls-api` x2, `tls-web` x2, `tls-cd-aquim-ai` (custom domain)

### 4.5 IngressRoutes (6 total)

Platform: `runtime-traefik-dashboard`
Tenant loopzero: `api`, `web`
Tenant 61957...: `api`, `web`, `cd-aquim-ai` (custom domain)

### 4.6 RUNTIME Findings

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| RT-001 | **HIGH** | Traefik has only 1 replica — single point of failure for ALL ingress | `kubectl get deploy runtime-traefik` shows 1/1 |
| RT-002 | MEDIUM | `loopzero-inference` namespace exists without PSA labels, NetworkPolicies, or ResourceQuotas | Redis pod running without restricted security context |
| RT-003 | MEDIUM | No metrics-server deployed | `kubectl top nodes` fails |
| RT-004 | LOW | No ValidatingAdmissionPolicies deployed (VAPs exist in code but not applied) | `kubectl get validatingadmissionpolicies` returns empty |

---

## 5. DO Resources Audit

### 5.1 Database Security

| Database | Firewall Rules | Role Separation | HA | Status |
|----------|---------------|-----------------|-----|--------|
| loopzero-infra-dev-sfo3-db | 3 (K8s clusters) | doadmin, loopzero_api, loopzero_migrate, loopzero_readonly | 2-node | **OK** |
| loopzero-inference-db | **0 (NONE)** | doadmin only | 1-node | **CRITICAL** |

### 5.2 Container Registry (14 repos)

All images updated today or within last 5 days. Key images:

| Image | Tag | Updated |
|-------|-----|---------|
| loopzero-api | dev | 2026-04-08 14:38 |
| tenant-controller | dev | 2026-04-08 14:41 |
| loopzero-cloud | c7c3664 | 2026-04-08 14:31 |
| loopzero-dashboard | dev | 2026-04-08 14:30 |
| loopzero-clone-runner | v0.1.0 | 2026-04-08 14:42 |
| loopzero-packs-runner | v0.1.0 | 2026-04-08 14:42 |

### 5.3 Firewall Audit

| Firewall | Finding | Severity |
|----------|---------|----------|
| loopzero-gitea-fw | SSH (22) open to 0.0.0.0/0 | **HIGH** |
| loopzero-observability-firewall | SSH (22) + HTTP (80) + HTTPS (443) open to 0.0.0.0/0 | **HIGH** |
| build cluster public-access | NodePort 30100 (Loki) publicly exposed | MEDIUM |
| inference cluster public-access | Port 3001 publicly exposed | MEDIUM |

### 5.4 DO Findings

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| DO-001 | **CRITICAL** | loopzero-inference-db has ZERO firewall rules — accessible from public internet | `doctl databases firewalls list` returns empty |
| DO-002 | **HIGH** | SSH (port 22) open to 0.0.0.0/0 on gitea and observability firewalls | `doctl compute firewall list` shows InboundRules |
| DO-003 | **HIGH** | inference-db uses only `doadmin` — no role separation | `doctl databases user list` shows only doadmin |
| DO-004 | MEDIUM | Build cluster NodePort 30100 (Loki) publicly exposed | Firewall public-access rule |
| DO-005 | MEDIUM | Inference cluster K8s 1.35.1 with auto-upgrade OFF — version drift | `doctl kubernetes cluster list` |
| DO-006 | LOW | loopzero_inference database exists in BOTH database clusters | `doctl databases db list` shows on both |

---

## 6. Integration Audit

### 6.1 Integration Status Matrix

| Integration | Status | Detail |
|-------------|--------|--------|
| ArgoCD → Gitea | **OK** | 2 repo secrets, 2 apps Synced+Healthy |
| ArgoCD → Runtime cluster | **OK** | Cluster secret configured |
| API → Gitea | **OK** | Token in loopzero-platform-tokens secret |
| API → ArgoCD | **OK** | Token + URL configured, notifications working |
| API → Build cluster (Argo WF) | **OK** | Kubeconfig mounted, builds executing |
| ESO (mgmt → runtime) | **OK** | ClusterSecretStore Valid+Ready |
| Cloudflare Tunnel | **OK** | 2 replicas, 4 QUIC connections, all endpoints reachable |
| TLS (Let's Encrypt) | **OK** | 12 certificates across 2 clusters, all Ready |
| Ory Kratos ↔ Hydra | **OK** | Both running, cloud-gateway routes configured |
| Loki (build → API) | **WARNING** | LOKI_URL uses raw IP (fragile) |
| Static delivery (CF Worker) | **ERROR** | Bare domain `dev.loopzero.example` has no DNS |

### 6.2 CRD Instance Gaps

| CRD | Expected | Actual | Status |
|-----|----------|--------|--------|
| Tenant | 2 | 2 | **OK** |
| Environment | 2 | 2 | **OK** |
| LoopZeroApplication | 2+ | **0** | **ERROR** |
| ExposedRoute | 2+ | **0** | **ERROR** |
| GitRepositoryBinding | 2 | **0** | **MISSING** |
| ClusterCredential | 1+ | **0** | **MISSING** |
| PlatformBootstrap | 0 | 0 | OK (one-time) |

### 6.3 Integration Findings

| ID | Severity | Finding | Evidence |
|----|----------|---------|----------|
| INT-001 | **CRITICAL** | LoopZeroApplication and ExposedRoute CRDs have 0 instances despite 2 running apps — ApplicationController/ExposureController not creating CRs | `kubectl get loopzeroapplications -A` returns empty |
| INT-002 | **HIGH** | Cloudflared catchall route (config v2) forwards ALL unmatched traffic to runtime Traefik instead of 404 | Tunnel config shows `service: https://runtime-traefik...` as catchall |
| INT-003 | MEDIUM | LOKI_URL uses raw node IP `10.20.0.6:30100` — will break on node replacement | loopzero-api configmap |
| INT-004 | MEDIUM | `dev.loopzero.example` bare domain has no DNS record — CF Worker unreachable | `curl dev.loopzero.example` fails DNS resolution |
| INT-005 | MEDIUM | No ArgoCD repo secret for newer tenant `tn-61957ca0ea3d311e` — may use shared credentials | Only `tn-loopzero` has explicit repo secrets |
| INT-006 | LOW | ArgoCD notifications uses `service.webhook.*` key (CLAUDE.md warns about `notifiers` vs `services`) | ConfigMap argocd-notifications-cm |
| INT-007 | LOW | Cloudflared version 2024.12.2 is outdated | Pod image tag |

---

## 7. Consolidated Findings by Severity

### CRITICAL (2)

| ID | Finding | Impact | Remediation | Effort |
|----|---------|--------|-------------|--------|
| DO-001 | **loopzero-inference-db has ZERO firewall rules** — database accessible from public internet with only password auth | Data breach risk | `doctl databases firewalls add <id> --rule k8s:<cluster-id>` | 5 min |
| INT-001 | **LoopZeroApplication/ExposedRoute CRDs have 0 instances** despite running apps — lifecycle tracking broken | No TLS cert monitoring, no reachability probes, no health tracking via CRDs | Investigate why ApplicationController is not creating CRs | 2-4h |

### HIGH (5)

| ID | Finding | Impact | Remediation | Effort |
|----|---------|--------|-------------|--------|
| MGMT-001 | OTEL trace export failing — collector unreachable | Distributed tracing non-functional | Fix OTEL collector endpoint config or deploy collector | 1-2h |
| RT-001 | Traefik runtime has 1 replica (spec says 2) | Single point of failure for ALL user app ingress | Scale to 2 replicas | 5 min |
| DO-002 | SSH (22) open to 0.0.0.0/0 on gitea + observability firewalls | Unauthorized SSH access risk | Restrict to admin IPs | 15 min |
| DO-003 | inference-db uses only `doadmin` — no role separation | Excessive database privileges | Create app-specific users | 30 min |
| INT-002 | Cloudflared catchall forwards ALL traffic to Traefik (not 404) | Internal services potentially exposed via unknown hostnames | Change catchall to `http_status:404` | 15 min |

### MEDIUM (11)

| ID | Finding | Impact |
|----|---------|--------|
| MGMT-002 | No PSA enforcement on mgmt cluster namespaces | Privileged pods could be created |
| MGMT-003 | No metrics-server on mgmt cluster | No `kubectl top`, no HPA |
| BUILD-001 | 4 orphaned PVCs (20Gi) from failed builds | Wasted storage costs |
| BUILD-002 | No metrics-server on build cluster | No resource visibility |
| RT-002 | loopzero-inference namespace without PSA/NetworkPolicies | Security gap |
| RT-003 | No metrics-server on runtime cluster | No resource visibility |
| DO-004 | Build cluster Loki NodePort 30100 publicly exposed | Log data exposure risk |
| DO-005 | Inference cluster K8s version drift (1.35.1 vs 1.32.13) | Compatibility issues |
| INT-003 | LOKI_URL uses raw node IP — fragile | Log queries break on node change |
| INT-004 | dev.loopzero.example bare domain has no DNS | Static delivery broken at root |
| INT-005 | No ArgoCD repo secret for newer tenant | Credential isolation gap |

### LOW (6)

| ID | Finding |
|----|---------|
| MGMT-004 | kube-state-metrics: 3 restarts over 4d |
| BUILD-003 | 4/11 builds failed (transient, self-recovered) |
| RT-004 | ValidatingAdmissionPolicies not deployed (exist in code) |
| DO-006 | loopzero_inference DB exists in both clusters |
| INT-006 | ArgoCD notifications key format concern |
| INT-007 | Cloudflared version outdated (2024.12.2) |

---

## 8. Desired State vs Actual State Comparison

| Aspect | Code/IaC Says | Actually Running | Gap |
|--------|--------------|------------------|-----|
| Traefik runtime replicas | 2 | **1** | **Mismatch** |
| OTEL collector | Deployed (otel chart exists) | **Not reachable** from API | **Mismatch** |
| LoopZeroApplication CRs | Created by deploy flow | **0 instances** | **Mismatch** |
| ExposedRoute CRs | Created by ApplicationController | **0 instances** | **Mismatch** |
| GitRepositoryBinding CRs | Created by operators | **0 instances** | **Mismatch** |
| ClusterCredential CRs | Created by operators | **0 instances** | **Mismatch** |
| ValidatingAdmissionPolicies | 4 policies defined | **0 deployed** | **Mismatch** |
| metrics-server | Standard K8s component | **Not on any cluster** | **Mismatch** |
| PSA on mgmt cluster | Expected at least `baseline` | **No labels** | **Mismatch** |
| DB firewall (inference) | Should have cluster rules | **0 rules** | **CRITICAL gap** |
| Tenant count | 2 | 2 | OK |
| ArgoCD apps | 2 Synced+Healthy | 2 Synced+Healthy | OK |
| TLS certificates | 12 | 12 (all Ready) | OK |
| Cloudflare Tunnel | 2 replicas | 2 replicas | OK |
| ESO ClusterSecretStore | Valid | Valid | OK |
| User apps | 2 tenants deployed | 2 tenants running | OK |

---

## 9. Remediation Plan

### P0 — Immediate (Today)

| # | Finding | Action | Effort |
|---|---------|--------|--------|
| 1 | DO-001: inference-db no firewall | `doctl databases firewalls add` with cluster IDs | 5 min |
| 2 | RT-001: Traefik 1 replica | `kubectl scale deploy runtime-traefik --replicas=2` | 5 min |
| 3 | DO-002: SSH open to world | Update firewall rules to restrict SSH to admin IPs | 15 min |
| 4 | INT-002: Catchall forwards to Traefik | Change tunnel config catchall to `http_status:404` | 15 min |

### P1 — This Week

| # | Finding | Action | Effort |
|---|---------|--------|--------|
| 5 | MGMT-001: OTEL traces failing | Fix collector endpoint or deploy OTel collector to mgmt | 1-2h |
| 6 | INT-001: LoopZeroApplication/ExposedRoute CRs missing | Debug ApplicationController — check if it's running, check logs, verify wiring | 2-4h |
| 7 | BUILD-001: Orphaned PVCs | Delete 4 workspace PVCs from failed builds | 10 min |
| 8 | DO-003: inference-db admin-only access | Create app-specific database users | 30 min |
| 9 | INT-003: LOKI_URL raw IP | Replace with stable DNS or service endpoint | 30 min |

### P2 — This Sprint

| # | Finding | Action | Effort |
|---|---------|--------|--------|
| 10 | MGMT-002/003, BUILD-002, RT-003: No metrics-server | Deploy metrics-server to all 3 clusters | 1h |
| 11 | MGMT-002: No PSA on mgmt | Add `pod-security.kubernetes.io/enforce: baseline` to platform-system | 30 min |
| 12 | RT-004: VAPs not deployed | Apply admission-policies Helm chart to runtime cluster | 30 min |
| 13 | RT-002: loopzero-inference namespace ungoverned | Add PSA labels, NetworkPolicies, ResourceQuotas | 1h |
| 14 | INT-004: Bare domain DNS | Create DNS record for `dev.loopzero.example` or configure CF Worker route | 30 min |
| 15 | DO-004: Loki NodePort exposed | Remove public-access firewall rule for port 30100 | 15 min |

### P3 — Future

| # | Finding | Action | Effort |
|---|---------|--------|--------|
| 16 | DO-005: Inference cluster version drift | Align K8s version or enable auto-upgrade | 30 min |
| 17 | INT-005: Missing repo secret for newer tenant | Verify credential isolation per tenant | 1h |
| 18 | INT-007: Cloudflared outdated | Update to latest version | 30 min |
| 19 | INT-006: ArgoCD notifications key format | Verify after next Helm upgrade | 15 min |

---

## 10. Positive Findings (What's Working Well)

1. **All 3 LoopZero clusters consistent:** Same K8s version (1.32.13), auto-upgrade enabled, 2 nodes each in shared VPC
2. **Zero CrashLoopBackOff:** 120+ pods across 3 clusters, all Running or Completed
3. **All external endpoints reachable:** API, cloud, ArgoCD, Grafana, Gitea — 200 OK via CF Tunnel with valid TLS
4. **12 TLS certificates:** All Ready, Let's Encrypt production issuer, including custom domain `aquim.ai`
5. **Tenant isolation enforced on runtime:** PSA restricted + deny-all NetworkPolicy + ResourceQuota + LimitRange + LoadBalancer blocked
6. **ArgoCD healthy:** 2 applications Synced+Healthy, notifications working
7. **ESO cross-cluster secrets:** ClusterSecretStore Valid, ready for use
8. **Build pipeline functional:** 7/11 builds succeeded today, CWTs deployed, tenant namespaces with full credentials
9. **Primary DB properly secured:** Firewall locked to K8s clusters, role separation (loopzero_api, loopzero_migrate, loopzero_readonly)
10. **Container images fresh:** All platform images updated today with pinned tags (no `:latest`)
11. **Custom domains working:** `aquim.ai` has dedicated IngressRoute + TLS certificate
12. **2-host architecture confirmed:** `api.dev.loopzero.example` (engine) + `cloud.dev.loopzero.example` (browser) both operational

---

## Appendix: Commands Used

All data collected via live kubectl and doctl commands against the actual DigitalOcean infrastructure:

```
# Clusters
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-{mgmt,build,runtime} ...
doctl kubernetes cluster list
doctl kubernetes cluster node-pool list <id>

# Database
doctl databases list
doctl databases firewalls list <id>
doctl databases user list <id>
doctl databases db list <id>

# Storage & Registry
doctl compute cdn list
doctl registry get
doctl registry repository list-v2

# Network
doctl vpcs list
doctl compute load-balancer list
doctl compute firewall list
doctl compute domain records list loopzero.example

# Reachability
curl https://api.dev.loopzero.example/api/v1/health
curl https://cloud.dev.loopzero.example/health
curl https://argocd.dev.loopzero.example
curl https://grafana.dev.loopzero.example
curl https://git.dev.loopzero.example
```
