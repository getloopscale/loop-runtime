# Steady-state baseline — 2026-06-08 pre pre-release-test-rigor

> 4-layer baseline per `.claude/rules/steady-state-baseline.md` captured
> at start of `/implement` halt-loop for plan
> `.claude/knowledge-base/plans/2026-06-08-pre-release-test-rigor-plan.md`
> v1.2 (verdict SHIPPABLE_WITH_CAVEATS, final_score 70.0, lifted from
> 42.4 by `/plan-improve`).

## Why this baseline

This baseline is the bisect anchor for the pre-release test rigor
delivery. The plan ships 12 tasks (T0.1 → T6.1) adding 4 realistic
scenarios extending cleanroom + dogfood full + chaos-test + live-proof
suites — closing the test-coverage gap that let 5 LIVE-only bugs slip
past the previous gates in the dashboard-screens-review-fixes
sponsor-walkthrough. Any regression that surfaces post-merge will be
diff-bounded by this snapshot. The next baseline
(`2026-06-08-post-test-rigor-cleanroom.md` — T6.1 evidence file) will
capture the post-implementation state after the cleanroom-fresh
`task test:pre-release-rigor` run completes.

---

## Layer 1 — Code state

```
commit:               78099a73540ee1e2359fb94cedada2420accdf16
branch:               worktree-glowing-growing-babbage
relation:             264 commits ahead of origin/develop, 47 behind
prior commit:         9e0526cd chore(impl): bootstrap halt-loop for pre-release-test-rigor
this baseline commit: pending (T0.1 closes within the next commit)
working tree:         clean (the contract correction landed in 78099a73)
```

The previous milestone baseline is
`docs/operations/baselines/2026-06-08-pre-dashboard-screens-review-fixes.md`
(15-task dashboard remediation). Intermediate work in
`worktree-glowing-growing-babbage` delivered the 15-task dashboard
plan + 5 iter-20 LIVE bug fixes + sponsor walkthrough sign-off + the
new contract-consumers quality gate (`pkg/quality/contractconsumers/`).

---

## Layer 2 — Runtime state (dev-public; mgmt+build+workload+runtime-2 k3d clusters via Caddy + CF)

Probe time: 2026-06-08 ~12:00 UTC

```
kubectl get pods -n loopzero-system (mgmt):
  ✓ loopzero-api          2/2 Running (3h24m;   replicas=2 per HA config)
  ✓ loopzero-cloud        1/1 Running (24h)
  ✓ ory-kratos          present per helmfile L2; serves auth-dev.loopzero.example
  ✓ ory-hydra           present per helmfile L2; client_credentials JWT mint
  ✓ postgresql-0      2/2 Running (2d1h;    sidecar metrics)
  ✓ gitea             1/1 Running (2d;      GitOps store)
  ✓ minio             1/1 Running (2d1h;    artifact store)
  ✓ harbor (5 pods)   Running (2d;          auxiliary registry)
  ✓ opencost          2/2 Running (2d1h;    cost attribution)
  ✓ app-operator      1/1 Running (21h;     loopzero-app-operator binary)
  ✓ tenant-operator   1/1 Running (21h;     loopzero-tenant-operator binary)
  ✓ preview-operator  1/1 Running (21h;     loopzero-preview-operator binary)
  ✓ platform-operator 1/1 Running (26h;     loopzero-platform-operator binary)

helm list -A (mgmt context): 21 releases per helmfile L2
ArgoCD Apps: live (sync-cycle ~3min); residual *-production / *-staging
             apps from prior M2/J-10/J-23 e2e suites still tracked as
             Unknown/Healthy — pruning happens lazily per env CR delete
Tenant CRs:  tn-08fa2ecedc3c7e77 (admin tenant; starter plan; project "teste")
             tn-e2etest (test harness; starter plan)
Environment CRs: ~5 Active across both tenants
```

Edge: `*.preview-dev.loopzero.example` wildcard via CF DNS + Caddy DNS-01 LE
cert (per memory `reference_j23_batch_2026_06_07`). Public dashboard at
`app-dev.loopzero.example` + Hydra at `auth-dev.loopzero.example` + cloud surface
at `cloud-dev.loopzero.example`.

---

## Layer 3 — Config state

```
PG schema_migrations: max(version) = 28; dirty = false
  Last 2 applied:  28 (current), 10 (one prior snapshot row — schema_migrations
                   table preserves last 2 rows by migrate-library convention)
Plan caps schema (plan_limits): present, PK = (plan, metric)
                                hard_limit + soft_limit columns
Hydra OAuth clients: loopzero-cli + loopzero-dashboard + loopzerokit persistent per
                     dev-public helmfile env (per memory project_canonical_dev_user)
Kratos identity schema: v1.4 (J-04 recovery + J-05 device flow registered)
ConfigMap loopzero-cosign-config: keyed mode (cutover deadline 2026-08-T+90d per
                              ADR 2026-05-12 cosign-keyless-cutover-deadline.md)
governance-gate.local.md:    APPROVED scope for 2026-06-08-dashboard-screens-review-fixes
                              and contract-consumers gate. Pre-release-test-rigor
                              scope NOT YET APPROVED — Phase 1+ may need extension.
Stripe webhook secret:        loopzero-stripe-webhook-dev Secret NOT PRESENT in loopzero-system
                              namespace. Stripe webhook tests are NOT in scope for this
                              plan (Cenário 1 is GitHub webhook; Stripe is a separate
                              flow not covered by the 4 scenarios).
```

The webhook endpoint (`POST /api/v1/webhooks/github` on
`cloud-dev.loopzero.example`) responds HTTP 401 on unsigned ping payload
(expected — HMAC-SHA256 signature required per
`reference_j09_live_e2e_complete_2026_06_06`).

---

## Layer 4 — LIVE functional probes (dev-public, 2026-06-08 ~12:00 UTC)

Each probe = command + observed result + invariant asserted. These
probes form the SLO floor: if any subsequent change breaks one, the
regression is bounded by this baseline + the next commit.

### Probe 1 — `/` (dashboard root)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://app-dev.loopzero.example/
→ 200
```
Invariant: dashboard SPA root loads OK.

### Probe 2 — `/projects` (project list page)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://app-dev.loopzero.example/projects
→ 200
```
Invariant: project list page reachable; T2.1 backend changes from prior
plan land in this view.

### Probe 3 — `/environments` (environments page — M2-aware)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://app-dev.loopzero.example/environments
→ 200
```
Invariant: M2-shipped UX with project picker + grouping + delete-with-?project=.

### Probe 4 — `/api/v1/projects` (engine API list)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://app-dev.loopzero.example/api/v1/projects
→ 200
```
Invariant: dashboard API proxy reachable; lists projects (the canonical
contract Cenário 4 will assert against).

### Probe 5 — `/health` (dashboard health)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 https://app-dev.loopzero.example/health
→ 200
```
Invariant: dashboard health endpoint OK.

### Probe 6 — `/api/v1/webhooks/github` (webhook endpoint reachable, signature check active)
```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 -X POST -d '{}' \
  -H "X-GitHub-Event: ping" \
  https://cloud-dev.loopzero.example/api/v1/webhooks/github
→ 401
```
Invariant: webhook endpoint serves but rejects unsigned payload (HMAC-SHA256
required). Cenário 1's chain test will sign payloads correctly.

---

## What changes between this baseline and the next (forward-looking)

After `/implement 2026-06-08-pre-release-test-rigor` completes,
the next baseline (`2026-06-08-post-test-rigor-cleanroom.md`) should show:

- `infra/tests/e2e/dev-public-webhook-chain_test.sh` exists (Cenário 1 LIVE)
- `infra/scripts/local/baseline-capture.sh` + `baseline-diff.sh` exist (Cenário 2 lib)
- `infra/scripts/local/seed-admin-loopzero-dev.sh` exists with 3-table tenant context (T0.2 + EC-3)
- `infra/scripts/local/connect-canonical-fixture.sh` exists (T2.1 fixture connect)
- `infra/scripts/local/live-proof-dev-public.sh` carries new `--check=api-sanity` branch
- `.claude/skills/chaos-test/SKILL.md` carries C11 + C12 entries
- `taskfiles/test.yml` carries `test:pre-release-rigor` entry
- T6.1 cleanroom-fresh evidence file confirms `task test:pre-release-rigor` exit 0
  with wall-clock < 120 min
- All new `*.sh` allowlisted in `.claude/rules/shell-scripts-allowlist.md` (per D5 of plan)

---

## Existing infrastructure this plan extends (Step 1 confirmed)

- 9 `dev-public-*_test.sh` LIVE tests already in `infra/tests/e2e/` —
  Cenário 1's webhook chain test becomes #10
- `infra/scripts/local/live-proof-dev-public.sh` already exists and
  ships `--check=oidc|stripe|resend|all` — T3.1 adds `--check=api-sanity`
- `taskfiles/test.yml` carries `parity:preflight`, `validate:matrix`,
  `pkg:soak:smoke`, etc. — T5.1 adds `test:pre-release-rigor` aggregator
- `.claude/skills/cleanroom/SKILL.md` exists — T1.2 wires baseline
  capture+diff into it as a phase pre-snapshot + post-diff hook
- `.claude/skills/chaos-test/SKILL.md` exists with 10 chaos cases (C01..C10) —
  T4.1 + T4.2 add C11 + C12

---

## Cross-link

- Plan: `.claude/knowledge-base/plans/2026-06-08-pre-release-test-rigor-plan.md`
- Edge cases: `.claude/knowledge-base/reviews/2026-06-08-pre-release-test-rigor-edge-cases.md`
- Deps audit: `.claude/knowledge-base/audits/2026-06-08-pre-release-test-rigor-deps-audit.md`
- Implementation contract: `.claude/knowledge-base/implementations/2026-06-08-pre-release-test-rigor-implementation.md`
- SEPA initial brief: `.claude/knowledge-base/implementations/2026-06-08-pre-release-test-rigor/sepa-iterations/initial-brief-response.md`
- Rule: `.claude/rules/steady-state-baseline.md`
- Previous baseline: `docs/operations/baselines/2026-06-08-pre-dashboard-screens-review-fixes.md`
- Sponsor mandate locking this plan ahead of priority chain: `CLAUDE.md § Dashboard Journeys — sponsor priority chain (locked 2026-06-08)`
- Memory: `feedback_test_rigor_before_priority_chain` (sponsor verbatim 2026-06-08)
