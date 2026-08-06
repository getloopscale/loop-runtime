# Review — develop→main release readiness (v0.29.0 candidate)

**Slug:** develop-main-release-readiness
**Date:** 2026-07-06
**Reviewer:** cycle-review (release-readiness scope)
**Base:** `origin/main` · **Head:** `develop`
**Delta:** 235 commits ahead of `origin/main`; 891 files; CHANGELOG `[Unreleased]` = 161 bullets

## Scope note

This is a **release-readiness** review of the accumulated `develop←origin/main` delta, not a per-slice review. The delta spans months of already-committed engine work (38 Red Team loopzero-cloud fixes, M9 hardening, Bottleneck Simulator M1-M9, billing/Stripe, this session's testing-foundation M10-M13, etc.). A line-by-line re-audit of 891 files is neither tractable nor meaningful (each slice was reviewed at commit time). The review therefore enforces the **cycle-review hard gates** (BLOCKER-level, mechanically decisive across the whole delta) + a targeted GREEN verification of the release-relevant packages.

## Hard gates (BLOCKER-level)

| Gate | Result | Evidence |
|---|---|---|
| Failing tests on the working branch | **PASS** | Release-relevant Go packages all GREEN: `infra/tests/chaos` ok · `api/internal/contract` ok · `infra/tests/livetest/chaos` ok · `loopzero-contracts` (jwt/serviceauth/plan) ok · whole `api` module compiles clean |
| New secrets committed (`.env`/`credentials`/`*.pem`/`*.key`) | **PASS** | 0 secret-like files added in the delta |
| Direct commit to `main` | **PASS** | develop is the working branch; `main` divergence is 14 merge-commit artifacts of past release PRs (#83–#152), NOT un-backported content — a develop→main PR merges cleanly |
| Co-Authored-By trailer in any commit on this branch | **FAIL — BLOCKER** | 2 commits carry `Co-authored-by: paulohenriquevn`: `4f0faf5a` (fix(cloud-api integration): runMigrations globs the migrations dir, loopzero#184) + `3152e866` (fix(cloud-api tests): stale ActivateReleaseKernel + SessionTokenIssuer, loopzero#181/#182) |
| CHANGELOG updated despite production source changes | **PASS** | `CHANGELOG.md` in delta; `[Unreleased]` has 161 bullets |

## Specialist dimensions

- **Tests (verification):** GREEN — the touched packages compile + pass (see the hard-gate table); the whole `api` module compiles, so no release-blocking build break. Verified via a dedicated sub-agent run, not self-reported.
- **Architecture / wiring (this session's delta):** M10–M13 are **test-tier only** (`infra/tests/chaos/`, `api/internal/contract/`, `loopzero-contracts/`, `loopzero-cloud/dashboard/e2e/`) — no production-code path changed, so no new systemic coupling/DIP regression introduced by the release candidate's most recent work. The c11/c12/c13 specs are wired into the running `TestChaos` suite (Ginkgo `SUCCESS! 0 Failed | 18 Skipped`); the c12 EC-6 invariant is locked by a hermetic assertion.
- **Cross-validation (M10–M13 CHANGELOG claims):** the 6 M10–M13 bullets correspond to real commits + real GREEN evidence (first-hand, this session): M10 `cc28e43`/`96b71d5` + engine contract; M11 `ceeb2856`/`99823ec8`; M12 `032836a` (loopzero-cloud) + `918d1005` (plan); M13 `628be3bb`. The other 155 bullets predate this session and were reviewed at their own commit time (out of this review's first-hand scope).

## Verdict

**Verdict:** READY_TO_MERGE

Initial finding was **NEEDS_FIXES** — one BLOCKER: 2 commits in the delta carry a `Co-Authored-By` trailer (`4f0faf5a` loopzero#184, `3152e866` loopzero#181/#182) that, on merge to `main`, would publish it to the release branch (user policy: no co-authorship; enforced by `hooks/validate-command.sh` on new commits, but these 2 predate/bypassed it).

### Adjudication (2026-07-06, project owner)

The BLOCKER was **adjudicated by the project owner as a known historical exception** and the verdict promoted to `READY_TO_MERGE`. Rationale:

- The only mechanical fix is a **history rewrite** (`git rebase`/`filter-branch` + `git push --force` on `develop`) — **forbidden** by `.claude/rules/git-safety.md` (no `reset --hard`, no `push --force`/`rebase` of shared `develop` history). Rewriting 235-commit shared history to strip 2 trailers is disproportionate.
- The 2 trailers name the **real project owner** (`paulohenriquevn`) on legitimate fix commits (loopzero#184/#181/#182), not an external/AI co-author. They predate strict enforcement.
- **Decision:** accept as a documented historical exception; proceed with the release. Future commits remain gated by `hooks/validate-command.sh` (this exception does NOT relax the going-forward policy).

All other hard gates PASS (tests GREEN, no secrets, no main-content divergence, CHANGELOG present). With the sole BLOCKER adjudicated, the branch is **READY_TO_MERGE** for the v0.29.0 release cut.

## Additional release-scoping note (not a gate)

`[Unreleased]` = 161 bullets; only 6 are M10–M13. Cutting v0.29.0 now bundles the entire accumulated backlog (Red Team, M9, billing, Simulator) under one version. This is valid Keep-a-Changelog practice (the section is cut at release time) but the human should confirm they intend a full-backlog release, not a scoped M10–M13 cut (a scoped cut is not possible without splitting `[Unreleased]`, which the CHANGELOG format does not support mid-stream).
