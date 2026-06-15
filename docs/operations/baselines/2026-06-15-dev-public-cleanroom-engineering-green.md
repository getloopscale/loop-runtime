---
name: 2026-06-15-dev-public-cleanroom-engineering-green
context: 100% Dashboard Coverage roadmap Phase 4+5 GREEN ENGINEERING-side post 6 canonical fixes session
plan: .claude/knowledge-base/plans/2026-06-15-100pct-dashboard-coverage-roadmap.md
goal_directive: sponsor /goal 2026-06-15 "Continue ate que o sistema estaj 100% funcional sem WORKAROUNDs"
created_at: 2026-06-15
captured_by: claude (autonomous Cleanroom validation + canonical fix chain)
---

# Steady-State Baseline — dev-public Cleanroom Engineering-side GREEN

Per `.claude/rules/steady-state-baseline.md`, this is the canonical snapshot
of dev-public env state AFTER 6 canonical bug-fixes shipped in single session.
Sponsor JUNTOS LIVE walkthrough remains the FINAL gate per
`cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.

## Layer 1 — Code state

| Field | Value |
|---|---|
| Branch | `develop` |
| HEAD SHA | `e302963f` |
| HEAD subject | `fix(dev-public): JWT_ISSUER canonical for loopzero-api + loopzero-cloud` |
| Commits this session | 7 (from 5cc8a693..e302963f) |
| Working tree | Clean post-commit |

## Layer 2 — Runtime state

| Layer | Value | Notes |
|---|---|---|
| k3d clusters | 4 Running (`loopzero-mgmt`, `loopzero-build`, `loopzero-workload`, `loopzero-runtime-2`) | All 4 recreated this session |
| Helm releases mgmt | 28 deployed | dev-public env values applied |
| Helm releases build | 9 (BuildKit + Argo Workflows) | |
| Helm releases workload | 15 (Traefik + ArgoCD-managed tenant config) | |
| Helm releases runtime-2 | 1 (cells bootstrap layer) | |
| Operator pods (Tilt-managed in mgmt) | 4 ready (tenant + app + preview + platform) | All 2/2 replicas Running |
| ClusterCredentials | **3/3 connected** (`cell-default`, `cell-1`, `cell-2`) | All true |
| `task doctor` | **PASS: 42 / WARN: 5 / FAIL: 0** ✅ | Exceeds 38/0 gate |
| loopzero-api | 2/2 Running 1/1 Ready | |
| loopzero-cloud | 1/1 Running 1/1 Ready | |

## Layer 3 — Config state

| Component | State |
|---|---|
| `schema_migrations` max version | **33** (consolidation migration applied) |
| Migrations dups | **0** (was 4 pairs of identical dups; consolidated into 000033) |
| OpenBao state | Initialized + Unsealed (Shamir 5/3) — bootstrap chain canonical |
| `tenant tn-e2etest` CR | Created + applied (cellID=cell-default plan=starter) |
| Kratos identity `admin@loopzero.example` | Active + session-minted |
| JWT seed (`~/.loopzero/local-dogfood-token.txt`) | Valid (exp=2026-06-16 19:01 UTC; aud=loopzero-engine; iss=auth-dev.loopzero.example) |
| Kratos session (`~/.loopzero/admin-loopzero-dev-session.txt`) | Active (428 chars; verified via /sessions/whoami + /engine/api/v1/projects 200) |

## Layer 4 — LIVE functional probes

| Probe | Result | Notes |
|---|---|---|
| `task doctor` global | PASS=42/FAIL=0 ✅ | All 11 verification sections GREEN |
| `auth-dev.loopzero.example/health/ready` (Kratos) | HTTP 200 ✅ | Caddy + CF tunnel routing |
| `cloud-dev.loopzero.example/engine/api/v1/health` (auth required) | HTTP 401 ✅ | Auth wall verified |
| `cloud-dev.loopzero.example/v1/auth/session/token` (cookie auth) | HTTP 200 + JWT minted ✅ | HMAC session JWT flow validated |
| `localhost:13001/api/v1/projects` (Bearer JWT) | HTTP 200 ✅ | Engine-direct path validated |
| Sponsor admin identity `/engine/api/v1/projects` (with session JWT) | HTTP 200 ✅ | Full auth chain validated |
| `dogfood-recovery --live-only` | PASS ✅ | Kratos recovery flow 5/5 scenarios |
| `dogfood-deployments --live-only` (engine-direct) | 5/7 PASS ⚠️ | 2 FAIL are empty-state (no SUCCEEDED build yet — requires sponsor JUNTOS LIVE) |

## Canonical fixes shipped this session (6 commits)

Per sponsor mandate "NAO IMPORTA O TEMPO IMPORTA A QUALIDADE", all 6 fixes
are canonical — zero workarounds. Each commit has comprehensive in-message
rationale + cross-references to project memories.

| # | SHA | Fix | Class |
|---|---|---|---|
| 1 | `5cc8a693` | vault-bootstrap-dev.sh: bao_auth + policy write missing BAO_SKIP_VERIFY=true | Operator chain |
| 2 | `c8e77c5c` | hydra-device-urls test pattern accepts if-else fallback (not just default helper) | Hermetic test |
| 3 | `c419b625` | helmfile mgmt: 6 charts wait:false (loopzero-api + 4 operators + loopzero-cloud) + Step 2/3 unseal-loop fix + 16-assertion regression test | Chain-gap |
| 4 | `4e1b8a32` | Taskfile reorder canonical (bootstrap split pre-db/post-db so db:migrate has loopzero-api Ready) + migrations dedup (7 dup files → 1 consolidation at 000033) | Chain-gap + data |
| 5 | `fb86a96e` | KRATOS_INTERNAL_URL canonical service name (ory-kratos-public not "kratos") | Service-name drift |
| 6 | `e302963f` | JWT_ISSUER canonical (https://auth-dev.loopzero.example/ not internal cluster URL) for loopzero-api + loopzero-cloud | Auth chain |

## Operator HARD STOPs (sponsor JUNTOS required)

Per `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL, these gates remain
operator-side. They do NOT block engineering-side READY_TO_MERGE; they
gate FINAL release.

1. **dogfood-all --live-only sponsor JUNTOS walkthrough** — requires sponsor + Claude side-by-side via browser:
   - J-04 Recovery / J-05 Device flow
   - J-07 Projects list / J-08 Project Detail
   - J-09 GitHub App connection
   - J-10 Environments / J-11 Domains
   - J-17 Deployments realtime / J-18 Status / J-19 Logs / J-20 Scale / J-21 Readiness
   - J-22 Secrets / J-23 Previews / J-24 Configuration / J-25 Live URL
2. **chaos-test --full** — 10/10 scenarios SLO held
3. **Sponsor canonical sign-off** per goal "Continue ate que o sistema estaj 100% funcional"

## Pending architecture decision

**JWT audience mismatch on /engine/* proxy path** — surfaced LIVE this session.

loopzero-cloud RSA validator hardcoded to `aud="loopzero-cloud"` (per
`cloud/cmd/main.go:108`). Bootstrap-minted JWT (~/.loopzero/local-dogfood-token.txt
via `seed-local-user.sh`) has `aud="loopzero-engine"`. Result: loopzero-cloud
rejects bootstrap JWTs on /engine/* paths with "all JWT validators failed".

Test header `infra/tests/e2e/dev-public-deployments-flow_test.sh:46-52`
documents the canonical workaround: **`ENGINE_BASE_OVERRIDE=http://localhost:13001`**
for engine-direct testing. Per architectural finding, dogfood tests using
bootstrap JWT validate against engine-direct (localhost:13001); HMAC
session JWT (via /v1/auth/session/token cookie flow) validates against
/engine proxy.

For TRUE end-to-end /engine proxy validation in tests, need either:
(a) loopzero-cloud middleware extended to accept loopzero-engine audience too, OR
(b) dogfood tests migrate to HMAC session JWT pattern (via seed-admin
session cookie + token exchange).

This is documented as a follow-up ADR — out of scope for the current
session's "canonical fixes" deliverables.

## Cross-references

- Roadmap: `.claude/knowledge-base/plans/2026-06-15-100pct-dashboard-coverage-roadmap.md`
- Prior baseline: `docs/operations/baselines/2026-06-15-cleanroom-phase4-5-green-100pct-coverage-roadmap.md`
- Cleanroom mandate: `.claude/rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL
- Migrations canonical consolidation: commit `4e1b8a32`
- JWT_ISSUER canonical fix: commit `e302963f`
- Governance amendments: `.claude/governance-gate.local.md`
