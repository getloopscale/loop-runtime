# ADR 0005: Cross-cluster networking strategy

## Status

In progress -- current implementation uses hardcoded Docker network IPs

## Context

LoopZero uses 3 k3d clusters (mgmt, build, workload) that need to communicate:

- **Build cluster** Argo Workflows must reach the **mgmt cluster** API for workflow callbacks and Gitea for git operations.
- **Mgmt cluster** ArgoCD must reach the **workload cluster** API server for sync.
- **Mgmt cluster** Prometheus must scrape metrics from the **build cluster** (Argo Workflows) and **workload cluster** (Traefik).

## Current Implementation

Cross-cluster networking relies on Docker network bridging and hardcoded IPs:

1. `k3d cluster create` places each cluster on its own Docker network (`k3d-loopzero-mgmt`, `k3d-loopzero-build`, `k3d-loopzero-workload`).
2. A setup script connects cluster load balancer containers to peer networks via `docker network connect`.
3. `infra/scripts/update-cross-cluster-ips.sh` detects the dynamically assigned Docker IPs of each cluster's load balancer on peer networks and patches them into:
   - `infra/tilt/api-deployment.yaml` (env vars: `GITEA_CROSS_CLUSTER_URL`, `API_CROSS_CLUSTER_URL`, `WORKLOAD_INGRESS_URL`)
   - `infra/helm/monitoring/values.yaml` (Prometheus scrape targets)
4. These IPs change every time clusters are recreated.

### Problems

- **Fragile:** IPs change on every `k3d cluster delete` / `k3d cluster create` cycle. Forgetting to run the update script causes silent connectivity failures.
- **Sed-based patching:** `update-cross-cluster-ips.sh` uses `sed` to modify YAML files, which can break on special characters or unexpected formatting.
- **Not idempotent in the general case:** if the YAML structure changes, the sed patterns may silently fail to match.
- **No DNS:** services reference IPs directly, preventing standard Kubernetes DNS-based discovery.

## Planned Improvement

Replace hardcoded IPs with DNS-based service discovery using ExternalName services and CoreDNS configuration.

### Step 1: ExternalName Services

Create ExternalName services in each cluster that resolve to the Docker DNS name of the target cluster's load balancer:

```yaml
# In build cluster: resolve mgmt API
apiVersion: v1
kind: Service
metadata:
  name: mgmt-api
  namespace: loopzero-cross
spec:
  type: ExternalName
  externalName: k3d-loopzero-mgmt-serverlb
```

### Step 2: CoreDNS Configuration

Configure CoreDNS in each cluster to forward `loopzero-cross.svc.cluster.local` queries to Docker's embedded DNS (127.0.0.11) so that Docker container names resolve inside the cluster:

```
loopzero-cross.svc.cluster.local:53 {
    forward . 127.0.0.11
    cache 30
}
```

### Step 3: Update Deployment Manifests

Replace hardcoded IP references with DNS names:

| Current | Target |
|---------|--------|
| `http://172.18.0.X:30000` (Gitea) | `http://mgmt-api.loopzero-cross.svc.cluster.local:30000` |
| `http://172.18.0.X:30001` (API) | `http://mgmt-api.loopzero-cross.svc.cluster.local:30001` |
| `http://172.18.0.X:80` (Workload) | `http://workload-ingress.loopzero-cross.svc.cluster.local:80` |

### Step 4: Remove IP Patching Script

Once DNS-based discovery is validated:

1. Remove `infra/scripts/update-cross-cluster-ips.sh`.
2. Remove the `update-cross-cluster-ips` task from `Taskfile.yml`.
3. Update `setup-cross-cluster-networking.sh` to create ExternalName services and CoreDNS config instead.

### Migration Path

1. Implement ExternalName services alongside current IP-based approach (both work simultaneously).
2. Validate DNS resolution from within build and workload cluster pods.
3. Switch deployment manifests to use DNS names.
4. Remove IP patching infrastructure.

## Consequences

- **Stability:** DNS names are stable across cluster recreations. No more IP drift.
- **Simplicity:** no sed-based file patching. Manifests contain fixed DNS names, not dynamic IPs.
- **Idempotency:** creating ExternalName services is idempotent (`kubectl apply`).
- **Complexity trade-off:** initial CoreDNS customization is more complex than sed, but the result is more robust and standard.
- **Docker dependency:** ExternalName resolution relies on Docker's embedded DNS, which is always available in k3d environments.
- **Production path:** in production, cross-cluster networking would use proper service mesh, VPC peering, or multi-cluster DNS (e.g., Submariner, Cilium ClusterMesh). This ADR covers the local k3d development environment only.
