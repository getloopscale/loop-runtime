# Review — test hardening (3 systemic points)

**Date:** 2026-07-11
**Scope:** `git diff v0.65.5..HEAD` — 3 improvement points from the "can we improve something?" assessment
**Reviewer:** focused correctness agent (command-verified) + follow-up fixes
**Verdict:** READY_TO_MERGE

## Points addressed

1. **Systemic "implemented but not wired" (dead test-infra)** — comprehensive grep-based sweep of `infra/tests/livetest/*` found ~15 orphan exports across `chaos`/`cleanroom`/`contraprova`/`dogfood` (extends #228). Filed **issue #230** (wire/delete/allowlist — a deliberate product decision, not force-resolved). Production Go dead-code (pkg/api/operators) is **tooling-blocked** (`deadcode` go-version mismatch: base go 1.24, go.work requires 1.26.1) — noted on #230.
2. **E2E false-green** — `deploy_suite_test.go` skipped the whole suite (0 specs = SUCCESS) even with `LOOPZERO_E2E=1` + unreachable infra. Now `Expect`s API health + cluster reachability past the `LOOPZERO_E2E` gate → RED on broken infra. Verified: `LOOPZERO_E2E=1` without a cluster now FAILs (`Ran 0 of 156 Specs ... FAIL!`), was fake-green; hermetic (`LOOPZERO_E2E` unset) still skips cleanly. Also repaired the stale taskfile guard (`test:e2e`) that grepped the wrong suite name and never fired.
3. **Glue-boundary hardening** — IP rate-limiter: regression tests locking `groupIPKey(::ffff:a.b.c.d) == groupIPKey(a.b.c.d)` (rate-limit-bypass guard) + `parsePeerIP` negative cases (nil, not a shared bucket).

## Findings

Review verdict: NO BLOCKER/HIGH/MEDIUM. The critical risk (a legitimate `LOOPZERO_E2E=1`-without-cluster flow turning RED) was investigated and **does not exist** — all `LOOPZERO_E2E=1` callers of the deploy suite require a cluster+tilt; the one drill caller runs a different suite. Two LOW findings (stale taskfile guard, duplicate CHANGELOG header) were **fixed** (11dbc90d).

## Verification (all green)

- `go vet ./e2e/...` → 0; hermetic `go test ./e2e/` → ok (skips); `LOOPZERO_E2E=1` no-cluster → FAILs correctly (fix working).
- `go test ./api/internal/middleware/` → PASS (new tests + 5 subcases).
- `task --list` parses the repaired taskfile.

## Blast radius (intentional)

`task test:e2e` run WITHOUT a cluster now FAILs instead of reporting fake-green — this is the fix exposing that the task never actually ran the deploy suite in that context. Legitimate use (with a cluster + tilt) is unaffected.
