---
type: baseline
title: "Baseline: Pre-Cleanroom 2026-06-24 — canonical fixture validation"
---

# Baseline: Pre-Cleanroom 2026-06-24 — canonical fixture validation

**Date:** 2026-06-24T00:07:30Z
**Branch:** develop
**SHA:** ca45e7b5bab696b71691b6768820e7e0f3aeddd1
**Context:** Pre-Cleanroom baseline for mandate chain execution against post-commit state
`1b4c4d15` (4 seed canonical fixes) + `ca45e7b5` (canonical fixture repo migration + hermetic guard).

## Layer 1 — Code state

```
SHA:    ca45e7b5bab696b71691b6768820e7e0f3aeddd1
Branch: develop
Recent: ca45e7b5 fix(canonical+guard): migrate fixture repo loopzero-repotest → loopzerokit-example + hermetic guard
1b4c4d15 fix(infra,seed): 4 LIVE-validated Cleanroom canonical fixes (chaos-mesh image + argocd-token HTTP + vault-addr HTTPS + Tenant CR loopzero.io/v1)
dd430b39 fix(infra)+fix(seed): Cleanroom canonical fixes — GHCR registry + kubectl context drift + Tenant CR schema
0f2a62e3 docs(discover+plan): Bun runtime-cost investigation — blueprint, T3 meeting (DEFERRED), Phase-0 benchmark plan
9ac903e5 docs(release): v0.28.0 RELEASED — M7 PREPARATION_COMPLETED post-merge artifacts
Status: 1 uncommitted file(s)
```

## Layer 2 — Runtime state (pre-destroy, captured for bisect anchor)

```
loopzero-api=2/2 ghcr.io/getloopscale/loopzero-api:develop
loopzero-cloud=1/1 ghcr.io/getloopscale/loopzero-cloud:develop
ArgoCD Apps (mgmt): 6
Workload pods (cell-default): 31
```

## Layer 3 — Config state (snapshot)

```
schema_migrations:       33 | f;;
loopzero-platform-topology: 22
project_repositories: 1
```

## Layer 4 — LIVE probes (pre-destroy)

(Captured during dogfood-all individual rerun 2026-06-23 22:30 UTC, post-commit `d990d1cd`:)
- status.url emitted: `https://tn-4a41ea2105c8b18b-teste-app-production.prod-dev.loopzero.example` → HTTP 200
- release `rel-3731517fed9f9ef0` ACTIVE; ArgoCD Synced/Healthy
- dogfood-all 13/13 wrappers PASS individually (JWT-refresh between runs)

## Purpose

Anchor for the destructive Cleanroom about to run. If post-Cleanroom state diverges from this baseline in an unexpected direction, bisect to the most recent commit between `d990d1cd` and `ca45e7b5` (4 commits total: d990d1cd, d112e31e, 1b4c4d15, ca45e7b5).
