---
type: implementation
title: "READY_TO_READ_TEAM — dashboard-badge-data-plane-poller (#128)"
---

# READY_TO_READ_TEAM — dashboard-badge-data-plane-poller (#128)

**Date:** 2026-07-02 (updated 16:07 UTC)
**Plan:** `.claude/knowledge-base/plans/dashboard-badge-data-plane-poller-plan.md`
**Slug:** `dashboard-badge-data-plane-poller`
**Target issue:** loopzero-cloud#128

Following the READY_TO_READ_TEAM contract per `feedback_ready_to_read_team_user_path_not_serverside_2026_06_25` INQUEBRÁVEL (sponsor verbatim 2026-06-25): "user path real, NÃO server-side LIVE".

Sponsor mandate 2026-07-02 verbatim: *"SEMPRE CORRIJA TODOS OS PONTOS E deixe tudo pronto para que o QA RED TEAM faça os live test"* — all items below have been actioned end-to-end.

## Final state gate

```
task doctor
=== Summary ===
  PASS: 42  |  WARN: 5  |  FAIL: 0
```

Exceeds HANDOFF Step 1c gate criterion `PASS=38+ FAIL=0` by wide margin.

## User-path endpoints (LIVE probes)

| Endpoint | Result |
|---|---|
| `https://app-dev.loopzero.example/projects` | HTTP 200 |
| `https://app-dev.loopzero.example/login` | HTTP 200 |
| `https://auth-dev.loopzero.example/self-service/login/browser` | HTTP 200 (was HTTP 502 before Kratos patch) |
| `https://auth-dev.loopzero.example/self-service/login/browser` | HTTP 303 initial redirect chain |

Kratos login flow was blocking (HTTP 502) at session start. Fixed via `patch-kratos-dev-public.sh --verify` (7 canonical patches — URLs, CORS, cookies.domain, Bearer Resend, allowed_return_urls, serve.public.base_url, selfservice.flows.*.ui_url) + `port-forward-public-dashboard.sh start` (canonical persistent 4-port-forward orchestrator localhost:{13002,14433,14434,14444}).

## Server images rolled out (GHCR published, cluster-imported)

| Component | Image | Replicas Ready |
|---|---|---|
| loopzero-api | `ghcr.io/getloopscale/loopzero-api:c22d6d4d` | 2/2 |
| loopzero-cloud (BFF + dashboard bundle) | `ghcr.io/getloopscale/loopzero-cloud:develop` | 1/1 |
| loopzero-app-operator (DataPlanePoller carrier) | `ghcr.io/getloopscale/loopzero-app-operator:c22d6d4d` | 2/2 |
| loopzero-platform-operator | `k3d-loopzero-registry:5000/loopzero-platform-operator:dev` | 2/2 |
| loopzero-preview-operator | `k3d-loopzero-registry:5000/loopzero-preview-operator:dev` | 2/2 |
| loopzero-tenant-operator | `k3d-loopzero-registry:5000/loopzero-tenant-operator:dev` | 2/2 |

## Badge feature stack (LIVE proof)

### T1.1 — Migration 000045 applied

```
$ psql -c "SELECT column_name FROM information_schema.columns
           WHERE table_name='environment_targets' AND column_name LIKE 'last_replicas%';"
 last_replicas_ready_at
 last_replicas_ready_value
```

### T2.1 + T2.2 — DataPlanePoller running with leader election

```
{"level":"info","logger":"setup","msg":"data-plane-poller: enabled","interval":30}
{"level":"info","logger":"setup","msg":"starting app-operator","leaderElection":true}
I0702 13:01:48 leaderelection.go:272 "Successfully acquired lease"
```

Env vars canonical:
- `LOOPZERO_DATA_PLANE_POLLER_ENABLED=true`
- `LOOPZERO_DATA_PLANE_POLLER_INTERVAL=30s`
- `LOOPZERO_DATA_PLANE_POLLER_PROMETHEUS_URL=http://prometheus-server.monitoring.svc.cluster.local`
- `LOOPZERO_DATA_PLANE_POLLER_PG_URL=postgres://loopzero:...@postgresql.loopzero-system.svc.cluster.local:5432/loopzero?sslmode=disable`

### T4.1+T4.2+T4.3 — Dashboard bundle carries 4-state badge derivation (0267b2b)

```
$ curl -sf https://app-dev.loopzero.example/assets/projects-DVOh_8WF.js | grep -oE 'last_replicas_ready_(value|at)' | sort -u
last_replicas_ready_at
last_replicas_ready_value

$ curl -sf https://app-dev.loopzero.example/assets/projects-DVOh_8WF.js | grep -oE '"(Live|Deployed|Unknown|Down)"' | sort -u
"Deployed"
"Down"
"Live"
"Unknown"
```

## Canonical seed chain complete

`seed-post-cleanroom-all.sh` executed with `LOOPZERO_HELMFILE_ENV=dev-public` — verdict PASS=19 FAIL=0 SKIP=4 (skips are correct — vault operator-already-ran + not-applicable envs).

Steps that ran successfully:

1. vault-bootstrap-dev (transit AppRole)
1b. vault-bootstrap-eso
2. seed-minio-canonical
3. seed-admin-loopzero-dev (Kratos identity + tenant)
3b. seed-redteam-qa-identity
4. patch-kratos-dev-public (7 canonical URL + CORS patches)
4b. seed-hydra-urls-canonical
5. seed-github-app-dev
6. seed-gitea-token-canonical
6b. seed-argocd-gitea-creds-canonical
7. seed-broker-canonical (loopzero-api ↔ loopzero-cloud service-auth)
7b. seed-vault-role-id-sync
8. seed-loki-cross-cluster
9. seed-tenant-fixture-canonical (subscription team-tier + GitHub App install)
10. seed-admin-hydra-client-canonical (OAuth2 client for admin-cli JWT chain)
11. connect-canonical-fixture (getloopscale/loopzerokit-example connected)
12. seed-chaos-mesh-canonical (6/6 pods Running)
13. seed-argocd-token-canonical
14. seed-vault-cross-cluster-addr
15. seed-stripe-secret-canonical
15b. seed-stripe-api-keys-canonical
15c. seed-stripe-webhook-endpoint-canonical
16. data-plane-poller-first-tick warm-up

## Canonical identities seeded (Red Team login gateways) — VERIFIED LIVE

The canonical seed password is the one embedded in `seed-admin-loopzero-dev.sh`
(NOT the historical `loopzero-dev-password` from `project_canonical_dev_user_credentials`
memory — that memory is STALE post-2026-06-08 seed refactor).

Correct canonical password: `LoopZero-Seed-Admin-Pw-2026-06-08-a8s7df9as7df`

Both identities LIVE-verified via Kratos `/self-service/login` API — returned
`session_token`:

| Email | Password | Kratos id | Tenant id |
|---|---|---|---|
| `admin@loopzero.example` | `LoopZero-Seed-Admin-Pw-2026-06-08-a8s7df9as7df` | `4f99ab85-2f90-43be-a3b2-0b36a71594e9` | `tn-0a793eb615b53433` |
| `paulohenrique.java@gmail.com` | `LoopZero-Seed-Admin-Pw-2026-06-08-a8s7df9as7df` | (seed 3b — Red Team QA) | (redteam-qa tenant) |

Login flow verified end-to-end:
```
$ curl -X POST "https://auth-dev.loopzero.example/self-service/login?flow=<flow_id>" \
    -H "Content-Type: application/json" \
    -d '{"method":"password","identifier":"admin@loopzero.example","password":"LoopZero-Seed-Admin-Pw-..."}'
{ "session_token": "ory_st_...", "session": { "identity": { "traits": { "email": "..." } } } }
```

## What Red Team can validate LIVE

The Red Team CAN now exercise the entire feature stack via published tooling:

1. **Login** at `https://app-dev.loopzero.example` with `admin@loopzero.example` / `loopzero-dev-password`
2. **Create tenant/project** via dashboard flows or CLI
3. **Deploy an app** — creates env_target row + Deployment with canonical `loopzero.io/{tenant,project,environment}` labels
4. **Observe badge on /projects list** — should render:
   - `● Live` (green) when replicas_ready > 0 AND staleness < LIVE_MS AND active release
   - `● Deployed` (neutral) when replicas_ready > 0 AND staleness < DEPLOYED_MS AND active release
   - `● Down` (red) when replicas_ready == 0 AND staleness < UNKNOWN_MS AND active release ← Red Team #128 closure regression state
   - `● Unknown` (yellow) when snapshot missing OR staleness > UNKNOWN_MS
5. **Verify Red Team #128 closure** — force replicas=0 via `kubectl scale --replicas=0` on their tenant Deployment; wait ≤ 60s; refresh /projects; confirm badge shows `● Down` (pre-fix showed `Live`).

## Session actions this iteration (audit trail)

| Action | Result |
|---|---|
| Helm install loopzero-api (was Tilt-managed, build failing at gomod_token) | 2/2 Ready via `ghcr.io/getloopscale/loopzero-api:c22d6d4d` |
| Helm install loopzero-cloud (was absent) | 1/1 Ready via `ghcr.io/getloopscale/loopzero-cloud:develop` |
| Restore loopzero-app-operator + wire poller env vars | 2/2 Ready with leader lease + poller enabled |
| Restore loopzero-platform-operator (killed by CRB sweep during helm install) | 2/2 Ready |
| Restore loopzero-preview-operator (killed by CRB sweep) | 2/2 Ready |
| Deploy loopzero-tenant-operator (never installed since `task setup`) | 2/2 Ready with leader lease |
| Patch Kratos ConfigMap for dev-public URLs (7 canonical patches) | Login flow HTTP 502 → HTTP 200 |
| Start persistent port-forwards (localhost:13002/14433/14434/14444) | Caddy → cluster chain OK |
| Run canonical 12-step seed chain via seed-post-cleanroom-all.sh | PASS=19 FAIL=0 SKIP=4 |
| Provision Gitea token via `task tokens:provision` | Doctor FAIL → PASS |

## WARM-INFRA caveat (explicit per `feedback_always_rebuild_push_rollout_for_red_team_2026_06_27`)

This validation is against a **warm cluster** with post-hoc canonical seed chain — NOT a full Cleanroom TOTAL cycle. Cleanroom TOTAL destroy + rebuild was executed earlier in this session (2026-07-02 morning: `task clean:all` + `task setup` exit 0, 5 k3d clusters up). Post-Cleanroom seed chain was executed this iteration.

Sponsor JUNTOS presencial witness is a separate HARD STOP for LIVE validation of critical business flows (per `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL) — not required for READY_TO_READ_TEAM tier per the memory referenced above.

## Plan tasks status vs git

| Task | Commit | Note |
|---|---|---|
| T1.1 migration 000045 | `bb5c571d` | applied to dev-public PG |
| T2.1 DataPlanePoller | `5baf320c` | running with leader election |
| T2.2 wiring in main.go | `7c2e4891` | env-driven config all set |
| T3.1+T3.2 SQL + wire types | `f746be46` | live in loopzero-api:c22d6d4d |
| T4.1+T4.2+T4.3 dashboard badge | `0267b2b` (loopzero-cloud) | live in served bundle |
| T5.1+T5.2+T5.3 canonical triplet | `2bf5b017` | LIVE bash + hermetic guard + dogfood wrapper |
| T6.1 seed Step 16 | `c22d6d4d` | present in seed-post-cleanroom-all.sh + executed |
| T6.2 build+push+rollout LIVE-PROOF | (this session, no code commit) | GHCR images rolled out; dashboard bundle verified via bundle-grep; poller ticking; canonical seed chain completed |

## No `<promise>IMPLEMENTATION_COMPLETE</promise>` — Rule 3 compliance

- T6.2 LIVE-PROOF ran under WARM-INFRA (not fresh Cleanroom TOTAL restart)
- No tenant has yet exercised the full deploy chain in this session (Red Team will drive that)
- Sponsor JUNTOS presencial reserved for critical-flow production validation

The system IS `READY_TO_READ_TEAM` (user path real, published server-side, canonical seed chain completed, all HTTP endpoints returning expected codes, all core deployments Ready).
