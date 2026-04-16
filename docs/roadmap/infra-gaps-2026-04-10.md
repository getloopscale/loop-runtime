# Pulumi Infra Gaps — discovered during roadmap 100k iteration

**Date:** 2026-04-10
**Source:** Iterations 4-12 of the roadmap 100k sessions — every manual
fix I had to apply in-cluster that the next `pulumi up` would NOT
re-apply automatically. These are the reasons the previous cluster
rebuild was not zero-intervention.

## Priority 1 — Bootstrap blockers (cluster rebuild FAILS without these)

### GAP-1: `loopzero-infra-registry` Secret is not created by Pulumi

**Symptom on rebuild:**
```
loopzero-api-*           0/1   Init:ImagePullBackOff
loopzero-cloud-*         0/1   ImagePullBackOff
loopzero-dashboard-*     0/1   ImagePullBackOff
tenant-controller-*  0/1   ImagePullBackOff
```
All Deployments reference `imagePullSecrets: [{ name: "loopzero-infra-registry" }]`
via their ServiceAccounts, but the Secret itself is not a Pulumi resource.
The code comment at `mgmt-bootstrap.ts:1517` says "doctl kubernetes cluster
registry add configures the default SA" — but that command does NOT
create the Secret in the `platform-system` or `argo` namespaces where
our ServiceAccounts live.

**Manual fix I applied in iter 4:**
```bash
doctl registry kubernetes-manifest --namespace platform-system \
  --name loopzero-infra-registry | kubectl apply -f -
doctl registry kubernetes-manifest --namespace argo \
  --name loopzero-infra-registry | kubectl --context=build apply -f -
```

**Proposed Pulumi fix:**

Create the Secret explicitly in every namespace that references it.
The content can be built from `config.digitaloceanToken`:

```typescript
// mgmt-bootstrap.ts — near registrySecretName
const dockerConfig = config.digitaloceanToken.apply(token => {
  const auth = Buffer.from(`${token}:${token}`).toString("base64");
  return JSON.stringify({
    auths: {
      "registry.digitalocean.com": { auth },
    },
  });
});

new k8s.core.v1.Secret(
  "loopzero-infra-registry-platform",
  {
    metadata: { name: "loopzero-infra-registry", namespace: PLATFORM_NAMESPACE },
    type: "kubernetes.io/dockerconfigjson",
    stringData: {
      ".dockerconfigjson": dockerConfig,
    },
  },
  { provider, dependsOn: [ns] },
);
```

Apply the same pattern in `build-bootstrap.ts` for the `argo` namespace.
(`buildkit-registry-config` already exists — that one is fine.)

**Files touched:**
- `infra/pulumi/src/components/mgmt-bootstrap.ts` — add secret before `apiSA` at line ~1521
- `infra/pulumi/src/components/build-bootstrap.ts` — add secret before `buildSA` at line ~98

---

### GAP-2: Runtime cluster tenant namespaces have no registry secret

**Symptom:**
When a tenant's app pod starts in `loopzero-<tenant>-<env>` namespace on the
runtime cluster, it fails with `ImagePullBackOff` because:
1. The `loopzero-app` ServiceAccount copies `imagePullSecrets` from the
   namespace's `default` SA (`environment_controller.go:321-327`).
2. But the `default` SA in a fresh tenant namespace has NO pull secrets
   because `doctl kubernetes cluster registry add` only runs once at
   cluster creation time, affecting the kube-system default, not
   newly-created namespaces.

**Manual fix I applied in iter 4 for every tenant ns:**
```bash
doctl registry kubernetes-manifest --namespace loopzero-<tenant>-<env> \
  --name loopzero-infra-registry | kubectl --context=runtime apply -f -
kubectl --context=runtime -n loopzero-<tenant>-<env> patch sa loopzero-app \
  --type=json -p='[{"op":"add","path":"/imagePullSecrets",
  "value":[{"name":"loopzero-infra-registry"}]}]'
```

**Proposed fix (two options):**

**Option A — environment-controller creates the secret itself:**

Modify `operators/internal/controller/environment_controller.go` so
`reconcileTenantNamespace` creates the registry secret in the tenant
namespace before creating the SA. The credential source is a Secret
the controller has read access to in its own namespace.

```go
// Before creating loopzero-app SA:
registrySrc := &corev1.Secret{}
if err := r.Client.Get(ctx, client.ObjectKey{
  Namespace: "platform-system", Name: "loopzero-infra-registry",
}, registrySrc); err == nil {
  pullSecret := &corev1.Secret{
    ObjectMeta: metav1.ObjectMeta{
      Name:      "loopzero-infra-registry",
      Namespace: nsName,
    },
    Type: corev1.SecretTypeDockerConfigJson,
    Data: registrySrc.Data,
  }
  _, err := controllerutil.CreateOrUpdate(ctx, r.RuntimeClient, pullSecret, ...)
}

// Then copy into the SA:
appSA.ImagePullSecrets = []corev1.LocalObjectReference{
  {Name: "loopzero-infra-registry"},
}
```

The controller already has access to the runtime cluster via
`r.RuntimeClient`. The only new thing it needs is the registry secret
in its own (mgmt) namespace — which GAP-1 fix provides.

**Option B — Pulumi pre-creates a ClusterSecretStore + ExternalSecret
template:**

More complex, involves ESO. Defer unless Option A has blockers.

**Recommended:** Option A — single controller change, no new infrastructure.

**Files touched:**
- `operators/internal/controller/environment_controller.go` — new function `reconcileRegistrySecret`, called before `reconcileServiceAccounts`
- Add RBAC rule for the controller to read the source Secret

---

### GAP-3: `loopzero-cf-credentials` not created when CF is partially configured

**Symptom:**
```
loopzero-api-*   0/1   CreateContainerConfigError
  Warning  Failed   Error: secret "loopzero-cf-credentials" not found
```

**Root cause:** `mgmt-bootstrap.ts:1433` guards creation with
`if (config.cfApiToken && config.cfAccountId && cf?.kvNamespaceId)`.
When CF is configured for custom domains but the static-delivery KV
namespace has not been created yet (first deploy, dev stack without
static frontends), the Secret is never created — but the Deployment
still references it because the code at line 1914 does NOT gate the
`envFrom`.

**Manual fix I applied:**
```bash
kubectl create secret generic loopzero-cf-credentials \
  --from-literal=CF_API_TOKEN=<token> \
  --from-literal=CF_ACCOUNT_ID=<account> \
  -n platform-system
```

**Proposed Pulumi fix:**

Always create the secret. Populate when CF is configured, leave empty
otherwise. An empty secret with `envFrom.secretRef.optional: true`
satisfies Kubernetes without setting any env vars.

```typescript
// Unconditionally create the stub — replaces the existing if block.
const cfCredentials = new k8s.core.v1.Secret(
  "loopzero-cf-credentials",
  {
    metadata: { name: "loopzero-cf-credentials", namespace: PLATFORM_NAMESPACE },
    stringData: {
      CF_API_TOKEN: config.cfApiToken || "",
      CF_ACCOUNT_ID: config.cfAccountId || "",
      CF_KV_NAMESPACE_ID: (cf?.kvNamespaceId || ""),
    },
  },
  { provider, dependsOn: [ns] },
);
```

The `mgmt-bootstrap.ts` line 1914 already uses `optional: true` — so an
empty secret is safe. The only thing that matters is that the secret
RESOURCE exists.

**Files touched:**
- `infra/pulumi/src/components/mgmt-bootstrap.ts` — remove the `if` guard at 1433, always create.

---

## Priority 2 — Degradation under load

### GAP-4: `DB_MAX_CONNS` not configured in loopzero-api-config

**Current state:** `config.go:173` defaults `DB_MAX_CONNS=25`. Two API
pods + one controller = 3 processes × 25 = **75 pool ceiling**. DO
managed PG `db-s-1vcpu-2gb` (the dev stack) has `max_connections=50`.
Under burst load the API will see `FATAL: too many connections for
role doadmin` before its own pool thinks it is saturated.

**Evidence:** Documented in `docs/capacity/cell-budget.md` § PG
connections as the hardest constraint today.

**Proposed fix:**
```typescript
// mgmt-bootstrap.ts — loopzero-api-config ConfigMap around line 1580
data: {
  // ... existing keys ...
  DB_MAX_CONNS: "12",   // 3 processes × 12 = 36 < 40 safe line
  DB_MIN_CONNS: "3",
}
```

For production (`Pulumi.prod.yaml`, `db-s-2vcpu-4gb` with
`max_connections=197`), override via config:
```yaml
loopzero-infra:dbMaxConns: "40"  # 3 × 40 = 120 < 160 safe line
```

**Files touched:**
- `infra/pulumi/src/components/mgmt-bootstrap.ts` — ConfigMap data block
- `infra/pulumi/src/config/stack.ts` — add `dbMaxConns` config field
- `infra/pulumi/Pulumi.dev.yaml` — `loopzero-infra:dbMaxConns: "12"`
- `infra/pulumi/Pulumi.prod.yaml` — `loopzero-infra:dbMaxConns: "40"`

---

### GAP-5: Gitea token scopes missing `read:organization`

**Current:** `["write:repository","write:organization","read:user"]`

**Problem:** Some Gitea API paths (e.g. `GET /api/v1/orgs/loopzero`,
`GET /api/v1/orgs/search`) require `read:organization` scope. I hit
this twice during the session when debugging repo-creds and ensureRepo.

**Proposed fix:** Add `"read:organization"` to the scopes array at
`mgmt-bootstrap.ts:1008`.

```typescript
-d '{"name":"loopzero-platform","scopes":["write:repository","write:organization","read:organization","read:user"]}'
```

Trivial 1-line change.

---

## Priority 3 — Nice-to-have / defense in depth

### GAP-6: Bootstrap `/internal/bootstrap` call has no retry

At `mgmt-bootstrap.ts:1133` the bootstrap Job calls the API's
`/internal/bootstrap` endpoint once. If the API pod is Running but
not yet serving (JWT validator loading, migration replay), the call
fails silently and the Job exits clean.

**Proposed fix:** Wrap in a retry loop with backoff, 30 attempts × 5s.

---

### GAP-7: `PlacementLockID` registry

I added `0x706C636D746C636B` to `api/internal/placement/engine.go` as
the advisory lock ID for the placement TOCTOU fix. LoopZero already uses
advisory locks in other places (environment lock family). We should
maintain a registry to prevent collisions.

**Proposed fix:** Create `docs/technical-reference/advisory-lock-ids.md`
listing every advisory lock ID used in the codebase with its purpose.
Zero Pulumi change, but the list should be kept up to date.

---

## Summary

| Gap | Priority | Files | Lines |
|---|---|---|---|
| 1 — loopzero-infra-registry secret | 🔴 P1 | mgmt-bootstrap.ts, build-bootstrap.ts | ~40 |
| 2 — Tenant ns pull secret | 🔴 P1 | operators/internal/controller/environment_controller.go | ~30 |
| 3 — loopzero-cf-credentials stub | 🔴 P1 | mgmt-bootstrap.ts | ~5 |
| 4 — DB_MAX_CONNS config | 🟡 P2 | mgmt-bootstrap.ts + stack.ts + Pulumi.*.yaml | ~15 |
| 5 — Gitea read:organization scope | 🟡 P2 | mgmt-bootstrap.ts | 1 |
| 6 — Bootstrap retry loop | 🟢 P3 | mgmt-bootstrap.ts | ~15 |
| 7 — Advisory lock registry | 🟢 P3 | docs only | ~40 |

**Impact if all P1 are implemented:** next cluster rebuild is zero-
intervention. Saves hours of manual debugging per rebuild.

**Impact if all P1+P2 are implemented:** dev stack can run L3 (100
tenants) without hitting the `too many connections` wall. Gitea API
scope issues disappear.

## Validation checklist

After implementing each gap:

- [ ] `pulumi preview` shows only additive changes (no destructions)
- [ ] `pulumi up` on a fresh cluster completes without manual
      intervention
- [ ] `kubectl -n platform-system get pods` shows all ready within 5 min
- [ ] `task test:load:l1` passes 10/10 without any secrets being
      patched by hand
- [ ] `task test:chaos:api-leader-kill` passes (proves GAP-1 doesn't
      break leader election)

## Not in scope

- The ArgoCD token generation flow at `mgmt-bootstrap.ts:1098` is
  already correct — it generates a real `apiKey` JWT for the
  `loopzero-api` account and stores it in `loopzero-platform-tokens`. The
  issue I hit this session was that the Job didn't run due to GAP-1
  blocking pod startup. Fix GAP-1 and the token flow works.

- `cell.ts` for Item 05 has its own TODO list in the roadmap item
  doc. Not covered here.
