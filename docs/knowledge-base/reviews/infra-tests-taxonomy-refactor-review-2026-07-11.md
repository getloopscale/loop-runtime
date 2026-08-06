---
type: review
title: infra/tests taxonomy refactor
---

# Review — infra/tests taxonomy refactor

**Date:** 2026-07-11
**Scope:** `git diff c4ed0a2b~1..HEAD` (infra/tests taxonomy refactor)
**Reviewers:** 4 parallel specialist agents (correctness/cross-validation, test-integrity, CI-wiring, architecture)
**Verdict:** READY_TO_MERGE

## Summary

Behavior-neutral structural refactor of `infra/tests`: 15 `check-*.sh` → `secrets/`+`platform/`;
60 helm env-parity files → 1 table-driven (`helm/envparity/`); `load`+`k6`+`capacity` → `performance/`;
5 config-drift tests → `config/`; `helpers_test.go` (810 LOC) split by domain; 12 `dev-public-*.sh` → `e2e/live/`;
fixture cache bloat removed (389 MB → 332 KB).

Net −1706 LOC across 171 files. All git mv (history preserved).

## Findings (all resolved before merge)

| Sev | Finding | Resolution |
|---|---|---|
| BLOCKER | `runner/run-k6.sh:21` cd `$SCRIPT_DIR/../k6` (moved to performance/k6) — broke 11 `test:load:*` tasks | `../performance/k6` (commit 1ee376ed) |
| BLOCKER | `performance/{load,capacity}` moved depth 3→4; `../e2e/fixtures` + `../../helm`/`../../helmfile` not deepened; `parse_fixture_test` is hermetic+ungated → broke `go test` | `../../e2e/fixtures` + `../../../helm|helmfile` (1ee376ed); parse_fixture PASS |
| HIGH | `e2e/live/dev-public-secrets-zero-leak:3` cd `../../..` resolved `infra/` not repo-root | `../../../..` (1ee376ed) |
| HIGH | `e2e/live/{webhook-chain,deploy-live-url}` seed-script path relative to `$0`, depth off by one | `+1 ..` (543c6206) |
| MEDIUM | `config/*.go` doc `// Package load` + stale run path; `capacity/*.go` run-command paths | fixed (a-prior commit) |
| LOW | `check-migrate-legacy-namespaces` orphan (target script missing pre-existing; not a gate) | follow-up: wire or delete |
| LOW/deferred | chaos/dogfood consolidation, dev-public shell-lib, `..`-count → `git rev-parse` anchor | out of scope; follow-up tickets |

## Verification (post-fix)

- `task quality:gates` → **exit 0** (59 PASS / 4 SKIP — toolchain-absent trivy/trufflehog/shellcheck + ory-kratos helmfile-WIP, all pre-existing).
- `go build ./...` (infra/tests) → 0; `go vet ./e2e/...` → 0; `go vet -tags helm_render ./helm/...` → 0.
- `go test ./config/...` → ok; `go test ./performance/load/ -run TestParseFixture` → ok (the previously-broken hermetic tests).
- helm envparity `TestEnvParity` → 60/60 subtests PASS; test coverage 100% preserved (independent count).
- 6 touched Go quality gates → tests GREEN; `no-new-bash` → PASS (all renamed paths allowlisted).

## Why this review mattered

The 2 BLOCKER + 1 HIGH regressions were invisible to `go build`/`go vet` and `task quality:gates`
because the affected paths live in **env-gated** tests (`LOOPZERO_REAL_DEPLOY` / `LOOPZERO_K3D_INTEGRATION` /
`--skip-live`) and one hermetic-but-uncovered test. Only the review exercised those code paths.
This is the review gate doing exactly its job.

## Follow-ups (non-blocking)

1. `check-migrate-legacy-namespaces_test.sh` — orphan (target `migrate-legacy-namespaces.sh` absent repo-wide): wire to a gate or delete.
2. Re-anchor `check-*.sh` + moved Go tests to `git rev-parse --show-toplevel` instead of counting `..`.
3. Consolidate `chaos/` + `livetest/chaos/` and the 3 `dogfood/` locations (deferred — runtime/Ginkgo coupling + Cleanroom LIVE mandate).
4. Extract `lib/shell-test-helpers.sh` for `e2e/live/*.sh` (deferred — M-Migrate2 owns dev-public shell).
