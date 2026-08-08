---
type: baseline
title: "Steady-State Baseline — post code-review remediation plan v1.2"
---

# Steady-State Baseline — post code-review remediation plan v1.2

**Date:** 2026-06-11
**Context:** Closing snapshot for plan
`2026-06-11-code-review-remediation-147-findings-plan.md` v1.2.
T6.3 acceptance criterion per `.claude/rules/steady-state-baseline.md`.

This baseline captures the engineering state after the multi-phase
remediation that closed 26 of the 33 plan tasks (8 deferred via
allowlist with explicit sunsets).

---

## Layer 1 — Code state

```
git rev-parse HEAD:        8ff0e066 feat(quality): T5.2+T5.2.a — code-review allowlist + CI sunset gate
git branch --show-current: worktree-glowing-growing-babbage
git status --porcelain:    clean (working tree empty)
git log --oneline main..HEAD | wc -l: ~30 plan-related commits
```

**Plan-attributed commits this session** (newest first):

| Task | Commit | Scope |
|---|---|---|
| T5.2 + T5.2.a | `8ff0e066` | Allowlist + CI sunset gate |
| T4.4 | `a9fe6941` | ProjectsPage CC=32→~15 |
| T4.3 | `28db4655` | RepositoryHandler.Connect CC=34→~16 |
| T4.2 | `11923a2a` | SecretHandler.Bulk CC=37→~14 |
| T4.1 | `2817ee9a` | Config.Validate CC=46→~10 |
| T3.6 | `beb8d3d5` | device.go pillar-(b) anti-regression |
| T3.5 | `d0630fc2` | reconcileDelete invariants |
| T3.4 | `cc6e1782` | tar zip-bomb caps |
| T3.3 | `7f0834dd` | SourceHandler 10-goroutine race |
| T3.2 + T3.3 | `17fcb046` | router-wiring + status RED contracts |
| T3.1 | `a744d6dc` | SecretFetch composite URL |
| T2.5 | `30616ebf` | pkg/naming.PreviewURL |
| T2.4 | `ee0f8687` | github API base env |
| T2.3.a + T2.3.b | `97f6e251` + `c1261fff` | PlanTier type + Create wire |
| T2.2 + T2.2.a | `8a88b8e8` | Loki M2 namespace OR |
| T2.1 | `d6be1943` | CLI dashboard URL env |
| T1.4 | `cca309ab` | useEnvironments hook |
| T1.3.a | `ce928fc9` | EnvRef Value Object types |
| T1.2 | `cba8bbe1` | DefaultPermanentEnv |
| T1.1.c | `9a3a8384` | Tiltfile ConfigMap+Secret render |
| (T1.1.a/b + T0.x earlier) | per implementation progress JSON | Tilt YAML migration + ADRs + baseline |

---

## Layer 2 — Runtime state

Not applicable for this remediation cycle: changes are pure code
and test-debt closures (no Helm release version bumps, no new
deployments, no CRD changes that affect runtime topology).

**Helm release baseline preserved:** the pre-remediation steady-state
[`docs/operations/baselines/2026-06-11-pre-code-review-remediation.md`](./2026-06-11-pre-code-review-remediation.md)
(commit referenced T0.1) captured runtime state; no drift expected.

---

## Layer 3 — Config state

**Tilt YAML drift CLOSED** (T1.1):
- `infra/tilt/api-deployment.yaml`: 53 inline `value:` env entries
  migrated to `envFrom: configMapRef + secretRef`
- `infra/tilt/cloud-deployment.yaml`: same pattern
- Tiltfile renders ConfigMap + Secret BEFORE Deployment apply
- Anti-regression hermetic test wired:
  `infra/tests/helm/tilt-secret-keys-classification_test.sh`

**Canonical product vocabulary CLOSED** (T1.2 + T1.4):
- `pkg/domain.DefaultPermanentEnv` is the single Go-side source of
  truth for the canonical permanent env (`"production"`)
- `cloud/dashboard/src/lib/domain.ts` mirrors it on the TS side
- Parity gate wired: `infra/tests/check-default-permanent-env-parity_test.sh`
- 6 hardcoded literals across api/ + cloud/ removed
- `cloud/dashboard/src/hooks/use-environments.ts` (NEW) closes the
  J-22↔J-10 silent break (hardcoded `['production', 'staging']` →
  dynamic env list from `/api/v1/environments`)

**Env-derived configuration CLOSED** (T2.1 + T2.4):
- `LOOPZERO_DASHBOARD_URL` overrides the CLI's AUTH_FAILED message URL
- `GITHUB_API_BASE_URL` overrides the GitHub API root (GHES support)

**Canonical plan tier types CLOSED** (T2.3):
- `pkg/domain.PlanTier` + `ResolvePlanTier` per ADR D4
- Closes H7 CRITICAL silent downgrade in Create handler
- Legacy `growth` → `pro` alias path

**Canonical preview URL formula CLOSED** (T2.5):
- `pkg/naming.PreviewURL` + `PreviewSubdomain` + `PreviewEnvName`
- Single source of truth for preview URL composition

---

## Layer 4 — LIVE functional probes

Probe execution requires Cleanroom TOTAL + UP + LIVE TEST per
`.claude/rules/cleanroom-up-live-test-mandate.md`. This baseline is
captured against a hot-path tree (operator-validated commits already
landed); a full Layer 4 probe pass MUST run as part of T6.2 sponsor
LIVE walkthrough BEFORE this baseline counts as canonical-complete.

**Layer 4 deferred to T6.2 sponsor LIVE walkthrough.**

Engineering-side validation that DID run:

- All `go test ./...` packages: GREEN (api, pkg, cloud, operators)
- Dashboard `npx vitest run` + `tsc --noEmit`: GREEN
- Hermetic infrastructure tests (allowlist sunsets, parity gates,
  Tilt secret classification): 7/7 + 7/7 + 7/7 PASS
- Property-based JWT signature flip + phase2gate boundary tests:
  GREEN under `-count=10`

---

## Plan completion ledger (honest, per Unbreakable Rule 3)

**Committed (26 of 33 plan tasks):**

| Phase | Tasks | Status |
|---|---|---|
| Phase 0 | T0.1–T0.8 | ✅ ADRs + baseline + Go bump + dependencies |
| Phase 1 | T1.1.a/b/c + T1.2 + T1.3.a + T1.4 | ✅ Tilt YAML + domain canonicalization |
| Phase 2 | T2.1–T2.5 + T2.2.a + T2.3.a/b | ✅ Drift fixes |
| Phase 3 | T3.1–T3.6 | ✅ Test debt closed (HappyPath + EC07 + tenant + source race + zip-bomb + reconcileDelete + pillar-b) |
| Phase 4 | T4.1–T4.4 | ✅ Complexity hot-spots T4.1+T4.2+T4.3+T4.4 |
| Phase 5 | T5.2 + T5.2.a | ✅ Allowlist + CI sunset gate |

**Deferred via allowlist** (rules/code-review-allowlist.txt, sunset 2026-09-09):

- **T1.3.b–f** — EnvRef migration across 34 call sites (type-side
  shipped in T1.3.a; per-package call-site migration is mechanical
  + per-PR-reviewable; deferred to keep this PR scope reviewable)
- **T4.5** — project-connect.tsx god-component (already heavily
  decomposed via 6 hooks; further extraction surfaces diminishing
  returns; revisit if CC ≥ 30 in next /loop-code-review)
- **T5.1** — 25 LOW maturity findings batched into follow-up
  grooming sweep (none security-impacting per per-finding triage)

**Operator HARD STOPs** (require cleanroom + sponsor LIVE):

- **T6.1** — Re-run `/loop-code-review` on same scope; assert
  ≤30 findings, zero CRITICAL, ≤5 HIGH. Requires `/loop-code-review`
  skill invocation against post-remediation tree.
- **T6.2** — Cleanroom TOTAL + UP + sponsor LIVE walkthrough on
  J-22 + J-10 surfaces. HARD STOP per
  `.claude/rules/cleanroom-up-live-test-mandate.md`.

---

## Bisect anchor utility

Future regression: bisect against this baseline + the previous
pre-remediation baseline
[`docs/operations/baselines/2026-06-11-pre-code-review-remediation.md`](./2026-06-11-pre-code-review-remediation.md).
The delta is ~30 commits across the 26 completed plan tasks above.

## Cross-references

- Plan: `.claude/knowledge-base/plans/2026-06-11-code-review-remediation-147-findings-plan.md`
- Pre-baseline: [`docs/operations/baselines/2026-06-11-pre-code-review-remediation.md`](./2026-06-11-pre-code-review-remediation.md)
- Allowlist: `rules/code-review-allowlist.txt`
- CI sunset gate: `.claude/scripts/check-allowlist-sunsets.py`
- Baseline canonical rule: `.claude/rules/steady-state-baseline.md`
- Cleanroom + LIVE mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
- ADRs locked: `decisions/2026-06-11-{tilt-yaml,pkg-domain-env-ref,pkg-domain-default-permanent-env,plan-tier-canonical-resolution,wiring-triad-pillar-b-test-contract,hot-path-complexity-decomposition}.md`
