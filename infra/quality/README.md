# Quality Gates — Engineer Reference

Plan: `git show 4f728eb7a:docs/plans/completed/2026-05-21-comprehensive-quality-gates-plan.md` v1.1
Operator handoff: recorded in the knowledge-base tree that no longer exists (`.claude/knowledge-base/` was removed; `.claude/` is gitignored).

## Quick start

```bash
# Local — measurement
task test:coverage          # run Go + CLI coverage, output /tmp/coverage-merged.lcov

# Local — gate dry-runs
bash infra/scripts/ci/coverage-go.sh        # measure Go coverage
bash infra/scripts/ci/test-inventory.sh     # build test inventory
bash infra/scripts/ci/check-pyramid.sh      # validate pyramid ratio
bash infra/scripts/ci/check-skip-link.sh    # find skip() without issue link
bash infra/scripts/ci/check-mock-budget.sh  # find files >5 mocks
```

## What each gate does

| Gate | Script | Blocks PR? | EC fixes |
|------|--------|------------|----------|
| Coverage diff | coverage-diff.sh | Yes (PR-blocking) | EC-7 (rename-only PASS) |
| Baseline regression | baseline-compare.sh | Yes | EC-2 (defer label) |
| PR template | template-compliance.sh | Yes | EC-5 (bot allowlist) |
| Bug-fix regression | check-regression-test.sh | Yes | EC-3 (PR title+body) |
| Test deletion audit | check-test-deletion.sh | Yes | EC-6 (rename detect) |
| Runtime SLO | check-runtime-slo.sh | Yes | — |
| Test pyramid | check-pyramid.sh | Yes | EC-12 (defer label) |
| Mock budget | check-mock-budget.sh | Advisory (legacy debt) | EC-9 (callback distinct) |
| Skip-link | check-skip-link.sh | Advisory (legacy debt) | EC-8 (all variants) |
| Flaky aggregator | flaky-aggregator.py | Async (cron) | EC-4 (single-writer) |
| Branch protection | branch-protection-update.sh | Operator-run | EC-1 (CRITICAL) |

## Escape valves (when gates legitimately block)

| Scenario | Label | What it does |
|----------|-------|--------------|
| Package split into 2 (chicken-egg) | `coverage-baseline-defer` | Skip baseline gate (7d TTL + auto-issue) |
| Sprint adds 50 E2E tests at once | `pyramid-defer` | Skip pyramid gate (one PR only + auto-issue) |
| Dependabot/Renovate PR | `dependencies` or `automated` | Skip template gate |
| Test deleted intentionally | `test-deletion-approved` | Allow test deletion |
| Slow integration test legitimate | `// slow_test: <issue-url>` comment | Exempt from runtime SLO |
| Permanent skip with docs reason | filename ends `_skip_test.go` | Exempt from skip-link gate |
| File legitimately needs >5 mocks | `// mock-budget: N — reason` comment | Raise budget for that file |

## When a gate fails

1. **Read the error message.** Each script prints WHY it failed + suggested fix.
2. **Fix at root, NOT via opt-out.** Opt-outs are escape valves for exceptional cases, not the default.
3. **If you're SURE it's a legitimate exception**, use the escape valve.
4. **If you're SURE the gate is buggy**, open a `quality-gates` label issue with reproducer.

## Adding a new gate

1. Write the gate in Go under `cmd/loopzero-ops/internal/quality/<name>/` and register it from its own adapter, `cmd/loopzero-ops/internal/qualitycli/<name>_gate.go` (one file per gate; its `init()` calls `register`, and a gate that is deliberately not wired says why in its `Wiring` field). New `.sh` files are refused by the `no-new-bash` gate; this step said `infra/scripts/ci/` until 2026-09-23.
2. Write its hermetic tests next to it (`check_test.go`); `task quality:hermetic` runs them.
3. Wire the gate into `task quality:gates` (`taskfiles/quality.yml`). This step named `.github/workflows/quality-gates.yml` until 2026-09-11; that workflow exists in no commit, and Actions was retired as the pipeline in 2026-05-23.
4. For the gate to face every pull request, add its `loopzero-ops quality <name>` step to the `public-checks` job of `.github/workflows/ci.yml`. No branch-protection entry is needed: the only required check is `CI gate`, which aggregates the CI jobs (`.github/branch-protection-config.yml`). `assert_delivery_gate_armed.py` reports which gates run on the pull request and which only run locally.

## Naming

- **Shell tests end in `_test.sh`** (kebab-case name, snake `_test` suffix: `dev-public-login-rate-limit_test.sh`). The suffix is deliberate, not an inconsistency: it mirrors Go's `_test.go`, and gates recognise test files by it (`billinglayers`, `divergencematrix` treat `_test.sh` exactly like `_test.go`). Renaming to `-test.sh` would silently turn those files into production scripts for every such gate.
- **Python scripts are snake_case** (`assert_delivery_gate_armed.py`), so any of them can be imported as a module instead of copied.

## Patterns reused across gates

- **Defer label**: PR label + non-empty reason → bypass gate + auto-issue
- **Bot allowlist**: PR author or label matches → bypass gate
- **Rename detection**: `git diff --find-renames=80%` for D+A pairs
- **Source priority**: PR title → body → commits (for parsing references)
- **EC-N references**: each amendment documented in plan v1.1 + linked here

## Helper API (helpers.go-style for quality scripts)

Currently each script is self-contained (KISS — no shared library yet). If
patterns emerge (e.g., 3+ scripts parsing PR body), extract to
`infra/scripts/ci/lib/`.

## Coverage of gaps from plan Coverage Matrix

23 of 23 gaps mapped (100%) per plan v1.1:
- 11 original gaps from §Context
- 12 edge cases (6 MUST FIX + 6 SHOULD TEST closed + 3 DOCUMENT as Risk Acknowledgements)

See plan §Coverage Matrix for the per-gap → task mapping.
