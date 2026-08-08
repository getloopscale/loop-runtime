---
type: baseline
title: "Steady-State Baseline — J-22 Layer 2 ESO Sync LIVE GREEN end-to-end"
---

# Steady-State Baseline — J-22 Layer 2 ESO Sync LIVE GREEN end-to-end

**Date:** 2026-06-10 (20:23 UTC)
**Context:** Closure of the J-22 OpenBao Transit + ESO Sync chain — Layer 2 (ESO webhook → loopzero-api broker → OpenBao transit decrypt → K8s Secret materialization) LIVE-validated end-to-end during sponsor walkthrough on plan `2026-06-10-j22-openbao-transit-eso-sync v1.2`.
**Captured by:** Claude (sponsor LIVE-presence)
**Mandate cross-ref:** `.claude/rules/steady-state-baseline.md` + `.claude/rules/cleanroom-up-live-test-mandate.md` (Cleanroom TOTAL preceded this baseline by ~5h)

---

## Why this baseline matters

Closes the LIVE walkthrough that started ~17:00 UTC after the canonical Cleanroom + UP + LIVE TEST chain (mandate of 2026-06-10). During the walkthrough we LIVE-caught and shipped **9 distinct Layer 1+2 bugs** in cascade:

| # | Bug class | Fix |
|---|---|---|
| 1 | M2 canonical namespace drift (M1 2-seg vs M2 3-seg) in `secret_k8s_manager.go` | `loopzero-<tenant>-<project>-<env>` Sprintf |
| 2 | OpenBao transit key path uses `/` — engine rejects | hyphen prefix `loopzero-tenants-<id>` + helper |
| 3 | Policy `transit/keys/...` `update` capability missing for PUT semantics | Add `update` to AppRole policy |
| 4 | OpenBao 403 (not 404) for missing key with non-root token — enumeration prevention | EC-06 self-heal triggers on `404 OR 403` |
| 5 | ExternalSecret target name trailing `-` when app is empty | `targetSecretName()` helper omits empty segment |
| 6 | ExternalSecret referenced legacy `loopzero-mgmt-store` CSS that didn't exist | Repointed to `openbao-transit`, then to canonical `loopzero-broker` |
| 7 | 9 unshipped pieces for webhook CSS backend (SecretFetchHandler unrouted, no middleware, no CSS template, no SA) | Full wiring + template + Bearer-via-Secret middleware + LIVE provisioning |
| 8 | Cross-cluster DNS (`loopzero-api.svc.cluster.local` unresolvable from workload ESO) | URL via `k3d-loopzero-mgmt-server-0:30001` NodePort |
| 9 | ESO URL-encodes `/` inside `remoteRef.key` → 404 at chi router | Split into `key` + `property` with two URL substitutions |

Every fix was committed alive into the working tree; this baseline is the bisect anchor for any regression that surfaces in the next 24h.

---

## Layer 1 — Code state

```
git rev-parse HEAD          → 097e9c501f9a9abe1acab45a52a34996ffcea99e
branch                      → worktree-glowing-growing-babbage
commits ahead of develop    → 0 (HEAD = origin/develop)
working tree                → 23 modified + 4 untracked (Layer 1+2 fixes uncommitted; commit pending)
```

Modified files (LIVE engineering not yet committed):

```
M api/cmd/main.go
M api/cmd/setup_secret_fetch.go                       (NEW)
M api/internal/domain/types.go
M api/internal/k8s/secret_k8s_manager.go
M api/internal/middleware/cross_cluster_token_review.go
M api/internal/routes/secrets.go
M api/internal/routes/secrets_test.go
M api/internal/server/router.go
M api/internal/server/server.go
M cloud/dashboard/src/components/secrets/add-secret-sheet.test.tsx
M cloud/dashboard/src/hooks/mutations/use-secrets-mutations.test.tsx
M cloud/dashboard/src/hooks/mutations/use-secrets-mutations.ts
M cloud/dashboard/src/hooks/use-secrets.test.tsx
M cloud/dashboard/src/hooks/use-secrets.ts
M cloud/dashboard/src/pages/secrets.test.tsx
M cloud/dashboard/src/pages/secrets.tsx
M docs/operations/openbao-transit-cutover-runbook.md
M go.work.sum
M infra/helm/loopzero-external-secrets/values.yaml
M infra/helm/loopzero-external-secrets/templates/clustersecretstore-loopzero-broker.yaml  (NEW)
M infra/tilt/api-deployment.yaml
M pkg/vault/transit_client.go
```

---

## Layer 2 — Runtime state

### Helm releases (mgmt cluster, dev-public)

| Release | Namespace | Chart | App ver |
|---|---|---|---|
| argocd | argocd | argo-cd-7.3.11 | v2.11.7 |
| **external-secrets** | external-secrets | external-secrets-0.14.3 | v0.14.3 |
| gitea | loopzero-system | gitea-10.6.0 | 1.22.3 |
| harbor | loopzero-system | harbor-1.16.2 | 2.12.2 |
| minio | loopzero-system | minio-5.4.0 | RELEASE.2024-12-18 |
| **openbao** | vault-system | openbao-0.28.3 | v2.5.4 |
| openbao-cert | vault-system | openbao-cert-0.1.0 | 1.0.0 |

### Helm releases (workload cluster)

| Release | Namespace | Chart | App ver |
|---|---|---|---|
| **external-secrets** | external-secrets | external-secrets-0.14.3 | v0.14.3 (REVISION=3) |

### loopzero-api LIVE image

```
k3d-loopzero-registry:5000/loopzero-api:tilt-253639d6335ef726
```

Two replicas Running across mgmt servers. Image carries the canonical
`tenantKeyName()` helper, the bearer broker middleware, the EC-06 self-heal
trigger fix (403 || 404), the M2 3-seg namespace formula, the canonical
ExternalSecret CR shape (key + property split), and the `loopzero-broker` CSS
reference.

### ClusterSecretStore `loopzero-broker`

| Cluster | Ready | Message |
|---|---|---|
| mgmt | True | store validated |
| workload | True | store validated |

### Tenant CR (sponsor canonical)

```
Tenant: tn-0ca75f9f32a0193d (Active starter)
Project: teste (proj-5acf981c)
Environment: production
Namespace: loopzero-tn-0ca75f9f32a0193d-teste-production (M2 3-seg canonical, workload cluster)
```

---

## Layer 3 — Config state

### DB migrations (PG)

```
version=32 dirty=f   (latest M2 plan_caps migration applied clean)
version=10 dirty=f   (legacy artifact, harmless)
```

### Secret resourceVersions (rotation markers)

| Secret | Namespace | Cluster | resourceVersion |
|---|---|---|---|
| loopzero-broker-token | external-secrets | mgmt | 24045 |
| loopzero-broker-token | external-secrets | workload | 45119 |
| loopzero-broker-token | loopzero-system | mgmt (loopzero-api envFrom) | 22345 |
| loopzero-openbao-approle | loopzero-system | mgmt (loopzero-api envFrom) | (earlier) |

### OpenBao policy `loopzero-api-approle` (LIVE-read via root token)

```hcl
path "transit/encrypt/loopzero-tenants-*" { capabilities = ["update"] }
path "transit/decrypt/loopzero-tenants-*" { capabilities = ["update"] }
path "transit/keys/loopzero-tenants-*"    { capabilities = ["create", "read", "update"] }
```

Hyphen prefix per LIVE-caught fix #2 + `update` capability per LIVE-caught fix #3.

### OpenBao transit keys (canonical naming)

```
bootstrap                                  (system)
flatname                                   (LIVE probe artifact)
foo                                        (LIVE probe artifact)
master                                     (system)
loopzero-tenants-approletest3                  (LIVE probe artifact)
loopzero-tenants-doesnotexist                  (LIVE probe artifact — root encrypt auto-creates)
loopzero-tenants-rootest                       (LIVE probe artifact)
loopzero-tenants-tn-0ca75f9f32a0193d           (SPONSOR — canonical tenant key, used by TESTE + TESTEDDDD)
```

The sponsor's tenant key uses the hyphen-prefix convention end-to-end.

---

## Layer 4 — LIVE functional probes (the SLO floor)

### Probe 1 — PG encryption-at-rest (vault:v1: prefix)

```
TESTE      vault:v1:OK
TESTEDDDD  vault:v1:OK
```

Zero plaintext-leak in PG.

### Probe 2 — K8s Secret materialized in tenant ns (ESO sync)

```
user-secrets-tn-0ca75f9f32a0193d-teste-production   Opaque   1   6m37s
```

ExternalSecret CR for TESTEDDDD reconciled → ESO created the materialized K8s Secret canonical.

### Probe 3 — broker endpoint round-trip (encrypt → decrypt → plaintext_b64)

```
POST /internal/v1/secret-fetch/tn-0ca75f9f32a0193d/TESTEDDDD
→ HTTP 200, time=20ms
→ {"tenant_id":"tn-0ca75f9f32a0193d","secret_name":"TESTEDDDD","plaintext_b64":"c2Rmc2FkZmFzZGY="}
```

`c2Rmc2FkZmFzZGY=` decodes to `sdfsadfasdf` — matches sponsor input exactly.

### Probe 4 — audit_log broker reads (audit trail proof)

```
secret.read.broker (count=28)
secret.set         (count=9)
```

Every broker fetch is audit-logged with actor=`broker:eso-webhook`. Zero
opaque accesses.

### Probe 5 — ExternalSecret CR status

```
es--teste       loopzero-broker  SecretSyncedError  False   (legacy shape; pre-canonical-fix)
es--testedddd   loopzero-broker  SecretSynced       True    (canonical key+property shape)
```

`es--teste` was created before the canonical key/property split fix shipped;
the next sponsor POST creates fresh CRs with the canonical shape.

### Probe 6 — controller liveness

```
[external-secrets workload]
  external-secrets-6cddc7bb75-mcgpm                   Running
  external-secrets-cert-controller-64f8978956-mmjpr   Running
  external-secrets-webhook-868d7b5756-2qkf2           Running

[loopzero-api mgmt]
  loopzero-api-59cff96d-7cl6g                             Running
  loopzero-api-59cff96d-qvfh7                             Running
```

All controllers Running; no restarts during the LIVE window.

---

## Outcome — what is LIVE-validated post this baseline

| Layer | State |
|---|---|
| Producer (dashboard → cloud → engine /api/v1/secrets/bulk) | ✅ GREEN |
| Encryption (OpenBao transit auto-create + Encrypt) | ✅ GREEN — hyphen-prefix keys + EC-06 self-heal |
| Persistence (PG encrypted_value with `vault:v1:` prefix) | ✅ GREEN — zero plaintext leak |
| Emission (ExternalSecret CR with canonical key + property) | ✅ GREEN |
| Broker endpoint (`/internal/v1/secret-fetch/{tenant}/{name}`) | ✅ GREEN — Bearer auth + tenant↔cell binding + transit decrypt |
| ESO sync (webhook CSS → broker → plaintext_b64 → K8s Secret) | ✅ GREEN — `SecretSynced True` end-to-end |
| Audit trail (`secret.read.broker` action by `broker:eso-webhook`) | ✅ GREEN — 28 reads logged |

## Outcome — what remains open

| Item | Status |
|---|---|
| Commit Layer 1 + Layer 2 fixes (23 files) | OPEN |
| `es--teste` CR refresh on canonical shape | OPEN (next sponsor POST replaces) |
| Sponsor LIVE walkthrough confirmation `"FUNCIONOU CANONICAL!"` | OPEN |
| T4.1 LIVE 6/6 zero-leak suite | OPEN |
| `<promise>IMPLEMENTATION_COMPLETE</promise>` | WITHHELD per Unbreakable Rule 3 — promise only after sponsor verbatim + T4.1 |
| Workload helm release of ESO into runtime helmfile (canonical, not ad-hoc) | OPEN follow-up (currently ad-hoc helm install) |
| Cross-cluster URL switch from Docker hostname to canonical k3d ingress | OPEN follow-up (`k3d-loopzero-mgmt-server-0:30001` is correct for dev-public but not portable) |

---

## Related

- Plan: `.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md` v1.2
- Runbook: [`docs/operations/openbao-transit-cutover-runbook.md`](../openbao-transit-cutover-runbook.md)
- Inquebrável Cleanroom mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
- Previous baselines on the same day:
  - [`2026-06-10-iter-27-j22-quality-gates-evidence.md`](./2026-06-10-iter-27-j22-quality-gates-evidence.md)
  - [`2026-06-10-iter-33-j22-session-final-evidence.md`](./2026-06-10-iter-33-j22-session-final-evidence.md)
  - [`2026-06-10-pre-j22-openbao-transit.md`](./2026-06-10-pre-j22-openbao-transit.md) (pre-Cleanroom Sun 06:56 UTC)
  - [`2026-06-10-post-j22-openbao-transit.md`](./2026-06-10-post-j22-openbao-transit.md) (post-rehearsal 08:21 UTC)
- Sponsor mandate verbatim: *"Voce deve terminar de implementar, precisamos fechar nosso sistema de Scrects [...] Senao esta fucnionando nao esta implementado"* + *"Por que voce esta preocupado com o tempo?"*

## Follow-up tasks (already registered)

- #102 — Cleanroom mandate Layer 4 LIVE seed gap absorption
- #103 — Anti-pattern: git stash with LIVE fixes
- #104 — 403 vs 404 OpenBao enumeration prevention pattern (memory)
