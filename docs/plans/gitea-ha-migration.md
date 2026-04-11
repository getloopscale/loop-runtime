# Gitea HA Migration Plan

**Status:** ready to execute
**Owner:** Infrastructure
**Complexity:** 2/5 (no data migration, additive only)
**Downtime:** ~2 minutes (Gitea pod restart after config change)
**Rollback:** revert to base `values.yaml` and restart

## Why now

The Gitea singleton is a durability risk documented in `docs/roadmap/index.md`
as an escalation trigger at L4 (500 tenants = 1,000 repos). The current
config stores sessions in local memory, cache in local memory, queues in
local leveldb, and non-git storage on the local PVC. If the pod is
rescheduled to a different node, users lose sessions and the PVC may be
inaccessible until the old node releases it.

At L3 (100 tenants) we need Redis for sessions+cache+queues and S3 for
non-git storage as a **prerequisite** for scaling beyond the current
singleton. This is NOT about running multiple Gitea replicas — it is about
making the SINGLE replica crash-safe.

## Execution steps

### Phase 1: Deploy Redis (5 min)

```bash
# Install Bitnami Redis (standalone, no auth)
helm install redis oci://registry-1.docker.io/bitnamicharts/redis \
  -n platform-system \
  -f infra/helm/redis/values.yaml \
  --version 20.11.0

# Verify
kubectl -n platform-system get pods -l app.kubernetes.io/name=redis
# redis-master-0   1/1   Running
```

### Phase 2: Create Spaces bucket (2 min)

```bash
# Via doctl — or add to Pulumi
doctl spaces create gitea-storage --region sfo3

# Or reuse the existing storage bucket if preferred
```

### Phase 3: Create Spaces credentials secret (1 min)

```bash
kubectl -n platform-system create secret generic gitea-spaces-credentials \
  --from-literal=MINIO_ACCESS_KEY_ID=<SPACES_ACCESS_KEY_ID> \
  --from-literal=MINIO_SECRET_ACCESS_KEY=<SPACES_SECRET_ACCESS_KEY>
```

### Phase 4: Upgrade Gitea with HA values (2 min)

```bash
# Apply the HA overlay on top of the base values
helm upgrade gitea gitea-charts/gitea \
  -n platform-system \
  -f infra/helm/gitea/values.yaml \
  -f infra/helm/gitea/values-ha.yaml \
  --set gitea.config.storage.MINIO_ACCESS_KEY_ID=<from-secret> \
  --set gitea.config.storage.MINIO_SECRET_ACCESS_KEY=<from-secret>

# Watch rollout
kubectl -n platform-system rollout status deploy/gitea --timeout=120s
```

### Phase 5: Verify (2 min)

```bash
# 1. Health check
kubectl -n platform-system exec deploy/gitea -- \
  wget -qO- http://localhost:3000/api/healthz
# Should return 200

# 2. Redis connected (check gitea logs)
kubectl -n platform-system logs deploy/gitea | grep -i redis
# Should show "Started Redis session/cache/queue"

# 3. S3 storage connected
kubectl -n platform-system logs deploy/gitea | grep -i minio
# Should show "Storage: minio"

# 4. Push a test repo
# Use the existing L1 smoke test — it pushes a bundle to Gitea
```

## What changes for the existing repos

**Nothing.** Existing git repos stay on the PVC. Only NEW non-git content
(LFS objects, avatars, attachments) goes to S3. Git repositories themselves
stay on the PVC until we migrate to NFS or Gitea's experimental S3 git
backend (deferred to L4 trigger).

Sessions in Redis are ephemeral by design — existing sessions will be
re-created on the next request. There is no session migration needed.

## Pulumi integration

For the Pulumi owner — add these to `mgmt-bootstrap.ts`:

1. **Redis Helm Release:**
```typescript
new k8s.helm.v3.Release("redis", {
  chart: "oci://registry-1.docker.io/bitnamicharts/redis",
  version: "20.11.0",
  namespace: PLATFORM_NAMESPACE,
  values: {
    architecture: "standalone",
    auth: { enabled: false },
    master: {
      persistence: { enabled: true, size: "1Gi" },
      resources: { requests: { cpu: "50m", memory: "64Mi" }, limits: { cpu: "200m", memory: "128Mi" } },
    },
    replica: { replicaCount: 0 },
    metrics: { enabled: true },
  },
}, { provider, dependsOn: [ns] });
```

2. **Gitea values merge:**
```typescript
// In the existing Gitea Helm Release, merge the HA values:
values: {
  ...baseValues,
  gitea: {
    config: {
      ...baseValues.gitea.config,
      session: { PROVIDER: "redis", PROVIDER_CONFIG: "redis://:@redis-master.platform-system.svc.cluster.local:6379/0" },
      cache: { ADAPTER: "redis", HOST: "redis://:@redis-master.platform-system.svc.cluster.local:6379/1" },
      queue: { TYPE: "redis", CONN_STR: "redis://:@redis-master.platform-system.svc.cluster.local:6379/2" },
      global_lock: { SERVICE_TYPE: "redis", SERVICE_CONN_STR: "redis://:@redis-master.platform-system.svc.cluster.local:6379/5" },
      storage: {
        STORAGE_TYPE: "minio",
        MINIO_BUCKET: "gitea-storage",
        MINIO_ENDPOINT: config.spacesEndpoint,
        MINIO_ACCESS_KEY_ID: config.spacesAccessKeyId,
        MINIO_SECRET_ACCESS_KEY: config.spacesSecretAccessKey,
        MINIO_USE_SSL: "true",
      },
    },
  },
  persistence: { size: "10Gi" },
}
```

3. **DO Spaces bucket** (if not reusing existing):
```typescript
new digitalocean.SpacesBucket("gitea-storage", {
  name: "gitea-storage",
  region: config.region,
  acl: "private",
});
```

## Future (L4+ trigger)

When the L4 load test (500 tenants) fires the Gitea bottleneck trigger
(`loopzero_gitea_push_duration_seconds p99 > 2s` OR OOM), evaluate:

1. **Git repos on NFS (ReadWriteMany):** enables multi-replica Gitea. DO
   does not offer managed NFS — evaluate self-hosted NFS-Ganesha or
   switch to AWS EFS.
2. **Gitea S3 git storage (experimental):** eliminates shared disk entirely
   by storing pack files in S3. Potentially slower for large repos but
   eliminates the NFS dependency.
3. **Direct GitHub integration:** remove Gitea entirely and use GitHub as
   the source-of-truth. Requires rewriting `source/` and `gitops/`
   packages. Highest effort but eliminates the Gitea dependency.

Decision criteria for each option are pre-documented in
`docs/roadmap/index.md` § Gitea Bottleneck.

## References

- Gitea HA Guide (Enterprise, config identical for Community): docs shared in session
- Bitnami Redis chart: https://github.com/bitnami/charts/tree/main/bitnami/redis
- DO Spaces (S3-compatible): https://docs.digitalocean.com/products/spaces/
- Roadmap escalation trigger: `docs/roadmap/index.md` § Gitea Bottleneck
