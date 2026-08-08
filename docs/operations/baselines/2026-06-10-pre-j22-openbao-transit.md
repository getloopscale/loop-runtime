---
type: baseline
title: "Steady-State Baseline — Pre J-22 OpenBao Transit + ESO Sync"
---

# Steady-State Baseline — Pre J-22 OpenBao Transit + ESO Sync

**Date:** 2026-06-10
**Captured at:** 2026-06-10T06:56:28Z
**Trigger:** T0.2 of plan `2026-06-10-j22-openbao-transit-eso-sync` v1.2 (pre-implementation)
**Companion JSON:** [`2026-06-10-pre-j22-openbao-transit.json`](./2026-06-10-pre-j22-openbao-transit.json)

This baseline is the bisect anchor for the OpenBao Transit + ESO Sync chain that wires the J-22 user-secret write path through OpenBao transit encryption + declarative ESO sync to workload pod env. If a regression surfaces after implementation, diff [`2026-06-10-post-j22-openbao-transit.md`](./2026-06-10-post-j22-openbao-transit.md) against this snapshot to localize.

## Layer 1 — Code state

| Field | Value |
|---|---|
| Branch | `worktree-glowing-growing-babbage` |
| HEAD commit | `3a393d4fa51bff7fe01f69a785a650b2a8663d0e` (T0.1 ship-the-gate) |
| Commits ahead of develop | 352 |
| Commits behind develop | 47 |
| Working tree clean | false (stashed J-22 LIVE-caught TS fixes preserved as `stash@{0}`) |

**Recent governance + chore commits (pre-implementation chain):**

```
3a393d4f feat(openbao-transit): T0.1 OpenBao prereqs check script + hermetic test
658efd29 docs(openbao-transit): plan v1.2 + edge-case review + deps-audit governance artifacts
fc185900 chore(deps): create allowlist + add 3 stdlib CVEs sunset 2026-07-10
e411494b chore(deps): bump golang.org/x/sync to v0.21.0 (api/) — singleflight prep
52093a9d chore(deps): bump go directive to 1.26 in 8 modules + sync workspace
a38f23e2 chore(deps): bump golang.org/x/net to v0.55.0 for GO-2026-5025..5030
07e97fd2 chore(deps): bump terratest v0.46.15 → v1.0.0 to unblock modular genproto
99b79958 chore(deps): bump go-jose/v4 to v4.1.4 for GO-2026-4945
```

## Layer 2 — Runtime state

Captured from `k3d-loopzero-mgmt` cluster (kubectl context current).

| Resource | Count | Note |
|---|---:|---|
| Helm releases (mgmt cluster) | 20 | argocd, cert-manager, blackbox-exporter, ...; full list in JSON |
| Running pods | 60 | spread across loopzero-system, argocd, cert-manager, monitoring, vault-system, etc. |
| ArgoCD Apps | 4 | tenant-side gitops resources |
| Tenants (PG) | 3 | active tenants in dev environment |

**Key absences for this plan (= confirmed gaps T0.1 reported):**

- ❌ `pkg/vault/transit_client.go` — does NOT exist (T2.1 will create)
- ❌ `infra/helm/external-secrets/templates/clustersecretstore-openbao-transit.yaml` — does NOT exist (T2.4 will create)
- ❌ ESO `ClusterSecretStore` named `openbao-transit` — does NOT exist on workload cluster (T2.4 deploy will materialize)

**Key presences (Phase 1+2 already shipped per upstream openbao adoption):**

- ✓ `infra/helm/openbao-init/`, `openbao-snapshot-agent/`, `openbao-cert/` (3 charts)
- ✓ `infra/helmfile/vault.helmfile.yaml.gotmpl`
- ✓ `pkg/vault/{client.go, cached_client.go, database_client.go, k8s_engine_client.go, ...}` (Phase 1+2 SDK wrapper)

## Layer 3 — Config state

| Setting | Current value | Why captured |
|---|---|---|
| `cosign_mode` | `keyed` | ADR commitment to keyless v1.1 cutover; T2.x of this plan does not touch the cosign path but the value documents the supply-chain context at baseline. |
| `pg_max_migration` | `32` | Highest applied migration version. This plan does NOT add new migrations (transit ciphertext stays in existing `secrets.encrypted_value BYTEA` column). Confirms baseline schema. |
| `plan_limits_present` | `true` | Plan-limit enforcement middlewares wired (J-22 T1.1 confirmed). This plan's Bulk handler extension respects plan-cap. |
| `LOOPZERO_SECRET_ENCRYPTION_KEY` (loopzero-api env) | (unset — confirmed via loopzero-api pod env inspection) | This is the ROOT CAUSE the plan v1.2 absorbs via D6 ADR fail-loud refusal. Today's silent fallback path stores PG plaintext. |
| `OPENBAO_TRANSIT_ENABLED` (loopzero-api env) | (unset — confirmed via loopzero-api pod env inspection) | NEW env this plan introduces (T2.5). Currently absent because pre-implementation. |

## Layer 4 — LIVE functional probes

5 of 6 HTTPS probes returned 200 against the dev-public surface; 1 expected 401 (webhook requires HMAC sig — not a bug).

| Endpoint | Status | Interpretation |
|---|---:|---|
| `https://app-dev.loopzero.example/` | 200 | Dashboard root reachable |
| `https://app-dev.loopzero.example/api/v1/projects` | 200 | Engine `/projects` list endpoint serving |
| `https://app-dev.loopzero.example/api/v1/environments` | 200 | J-10 environments path functional |
| `https://app-dev.loopzero.example/health/deep` | 200 | API deep-health green |
| `https://cloud-dev.loopzero.example/api/v1/projects` | 200 | Cloud `/projects` proxy serving |
| `https://cloud-dev.loopzero.example/api/v1/webhooks/github` | 401 | Webhook endpoint serving but rejects unauthenticated probes per design (CHECK: HMAC required); not a regression |

**Plaintext-path confirmation (the canonical pre-fix state this plan eliminates):**

- POST `/api/v1/secrets/bulk` with `{secrets:[{name:"DATABASE_URL",value:"postgres://x",envs:["production"]}]}` → 200 (per J-22 v1.2.1 walkthrough commit `0cf1cc23`).
- PG row `SELECT encode(encrypted_value, 'escape') FROM secrets WHERE name='DATABASE_URL'` → `postgres://x` (13 bytes plaintext). This is the bug. Plan T2.3 fixes via transit.Encrypt route + D6 fail-loud refusal.
- `kubectl get externalsecret -A` → `No resources found` (T1.1 Gap 1: Bulk handler does NOT invoke k8sSync). Plan T1.1 fixes.

This baseline captures these gaps so the post-implementation diff CAN verify they were closed.

## Cross-references

- Rule: [`.claude/rules/steady-state-baseline.md`](../../../.claude/rules/steady-state-baseline.md)
- Plan: [`.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md`](../../../.claude/knowledge-base/plans/2026-06-10-j22-openbao-transit-eso-sync-plan.md) v1.2 § T0.2
- Companion JSON: [`2026-06-10-pre-j22-openbao-transit.json`](./2026-06-10-pre-j22-openbao-transit.json)
- Index: [`index.md`](./index.md)
- Post-fix baseline (will land after T6.1 closure): [`2026-06-10-post-j22-openbao-transit.md`](./2026-06-10-post-j22-openbao-transit.md)
