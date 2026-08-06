# Review — infra/tests taxonomy follow-ups

**Date:** 2026-07-11
**Scope:** `git diff be94cb26..HEAD` (follow-ups to the infra/tests taxonomy refactor)
**Reviewer:** focused correctness/wiring agent (command-verified)
**Verdict:** READY_TO_MERGE

## Scope

Three follow-ups from the taxonomy review, plus a doc cleanup:

1. Deleted orphan `check-migrate-legacy-namespaces_test.sh` (target script removed by M-Migrate3; dead test wired to no gate) + empty `platform/` dir + allowlist entry.
2. Consolidated dogfood utilities (`preflight.sh` + `traffic.js` + fixtures + README) under `infra/tests/livetest/dogfood/` — 2 top-level dirs removed; lockstep allowlist + `observability.yml` + `scriptsaudit` gate.
3. Re-anchored 14 `secrets/check-*.sh` + `preflight.sh` from `..`-counting to `git -C <script-dir> rev-parse --show-toplevel` — move-proof + CWD-independent.
4. Repathed stale doc/comment refs in the moved dogfood files.

## Findings

No BLOCKER / HIGH / MEDIUM. LOW findings (stale string refs) were pre-existing and have been fixed (commit e8280a5c).

The two failure modes a prior green-gates run masked — broken relative paths and orphan refs after moves/deletes — were **actively disproven**:
- `git -C rev-parse` is provably CWD-independent and resolves identically to the old logic (group-1 → `<root>/infra`, group-2 → `<root>`).
- `no-new-bash` + `quality:gates` both pass with the moves/deletes correctly reflected in the allowlist.

## Verification (all green, command-verified)

- 14/14 `secrets/check-*.sh` PASS standalone (re-anchor preserved REPO_ROOT semantics).
- `git -C rev-parse` resolves correctly from an arbitrary CWD (`/tmp`) — robust.
- `scriptsaudit` Go gate test → ok (classify `livetest/dogfood/` added; synthetic fixture retained).
- `observability.yml` → new `traffic.js` path; monitoring configs reference by NAME (metric/uid `loopzero-dogfood-slo-gate`), not the path — unaffected.
- git rename detection: preflight R099, traffic.js R100, orphan test D.
- `loopzero-ops quality no-new-bash --base-ref be94cb26` → PASS (exit 0).
- `task quality:gates` → exit 0 (59 PASS / 4 SKIP toolchain-absent, pre-existing).

## livetest/chaos (investigated, kept — not "really orphan")

Its exported helpers have zero consumers, but it is self-tested and wired into the chaos hermetic aggregate (`chaos:v2:hermetic`), and was intentionally built (M10 T4.1) for future chaos LIVE Tempo assertions. Classified "implemented but not wired" — a deliberate wire-or-remove decision for the team, NOT a mechanical dead-code cleanup like the check-migrate orphan (whose target was deleted). Kept.
