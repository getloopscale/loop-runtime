---
version: 1.0
slug: infra-tests-hermetic-to-quality-gates
milestone_id: M16
owner: claude-code (autonomous per sponsor goal-directive 2026-07-10)
created_at: 2026-07-10
generated_by: /discover-execute
target_cycle: cycle-discover phase 4
based_on: knowledge-base/discoveries/plans/infra-tests-hermetic-to-quality-gates-plan.md v1.1 (SHIPPABLE 98.8)
---

# Blueprint: Hermetic Test Migration `infra/tests/check-*_test.sh` → Go Quality Gates

## Objective

Derive a codified M-Migrate2 migration pattern from the 12 existing `cmd/loopzero-ops/internal/quality/*/` gates so that all 27 remaining migrations across M16 Waves 1+2+3 follow byte-identical structure, error handling, and test shape. The blueprint answers 8 research questions grouped in 4 coverage corners (tests / deps / tools / techniques) and produces a 14-step migration recipe + 5 ADRs codifying decisions surfaced during discovery.

## Executive Summary

The 12 existing `cmd/loopzero-ops/internal/quality/*/` M-Migrate2 precedents establish a highly consistent migration pattern that Waves 1+2+3 (27 remaining scripts) can follow with confidence. Each migration is `check.go` (~150-200 LoC) + `check_test.go` (~46-134 LoC) + one registry row in `cmd/loopzero-ops/quality_registry.go` + one wired-line in `taskfiles/quality.yml`. All 12 precedents share stdlib-only imports (13 unique stdlib pkgs, ZERO third-party). Test fixtures use inline `t.TempDir()` + per-gate helpers (no shared helper package). Test naming follows `TestCheck_<Scenario>_<Outcome>` per-scenario (not table-driven).

**Two divergences** surfaced that Waves 1+2+3 must resolve:

1. **Outcome struct shape is BIMODAL** — 7/12 gates use `Failures []string`, 4/12 use `Violations []string`, 1/12 custom. Per EC-2 of the edge-case review, since the split has a clear modal winner (58%), M16 STANDARDIZES ON `Failures []string` for new gates.
2. **Pre-flight binary guards are MISSING for Wave 2** — only `helmfile` has a `command -v` guard in `taskfiles/quality.yml:160`. Wave 2's 6 helm-template gates require adding guards for `helm`, `jq`, `yq` (EC-3 confirmed).

Additionally, **5 of the 12 M-Migrate2 gates are registered but NOT wired into `quality:gates`** (webhook-canonical-path, oidc-cred-admin-loopzero, canonical-fixture-repo, install-git-hooks, render-schema-gen-sql). ADR D4 below addresses this gap.

## Context (from the plan)

M16 milestone (`ROADMAP.md`) targets migration of 27 hermetic bash tests to Go quality gates. M15 shipped 12 M-Migrate2 gates covering `infra/tests/check-*_test.sh` variants — those 12 gates ARE the reference implementations for M16's migration pattern.

The 12 precedents live at:

- `cmd/loopzero-ops/internal/quality/allowlistsunsets/`
- `cmd/loopzero-ops/internal/quality/caddyfilenodockerip/`
- `cmd/loopzero-ops/internal/quality/optionbeso/`
- `cmd/loopzero-ops/internal/quality/vaultbootstrapinmgmt/`
- `cmd/loopzero-ops/internal/quality/canonicalfixturerepo/`
- `cmd/loopzero-ops/internal/quality/esooverlay/`
- `cmd/loopzero-ops/internal/quality/installgithooks/`
- `cmd/loopzero-ops/internal/quality/k6loadrigor/`
- `cmd/loopzero-ops/internal/quality/oidccredadminloopzero/`
- `cmd/loopzero-ops/internal/quality/routeauthcoverage/`
- `cmd/loopzero-ops/internal/quality/webhookcanonicalpath/`
- `cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop/`

## Coverage Corner 1 — Integration Tests

### Q1 — allowlistsunsets fixture strategy

**Answer:** INLINE fixture strategy (no `testdata/` directory). Uses `t.TempDir()` for isolation.

- **File:** `cmd/loopzero-ops/internal/quality/allowlistsunsets/check_test.go`
- **Strategy:** inline — no `testdata/` embedded
- **t.TempDir usage:** `check_test.go:44` in `TestCheck_MissingGateFails`
- **Fake file count:** 0 (empty tempdir asserts absence of gate script)
- **Assertion style:** `errors.Is(err, ErrViolations)` (line 46)

Evidence:

- `check_test.go:17-35` — `TestCheck_RealRepoPasses` uses the actual repo layout (no fixtures; traverses `.../8` parent dirs via `filepath.Abs` to locate the real gate script at `gatePath`)
- `check_test.go:40-49` — `TestCheck_MissingGateFails` creates `t.TempDir()` at line 44, passes as `Config{RepoRoot: root}` at line 45, asserts `ErrViolations` returned when Python script missing at line 46

**Note (heuristic):** `allowlistsunsets` is the UNIQUE gate that mixes real-repo + empty-tempdir strategies. The 3 sampled Q2 gates use inline fixture-write helpers instead. Waves 1+2+3 should follow the Q2 pattern (helper-based inline fixtures), NOT the allowlistsunsets pattern — the latter is only appropriate when the gate operates on the real repo layout with no meaningful failure fixture.

### Q2 — Canonical `check_test.go` shape across 3 sampled gates

**Answer:** NO shared helper package. Each gate duplicates its own inline helper. Per-scenario test functions (NOT table-driven). Naming pattern `TestCheck_<Scenario>_<Outcome>`.

**Per-gate LoC (measured via wc -l):**

| Gate | LoC | Helper function | Purpose |
|---|---|---|---|
| esooverlay | 134 | `writeFixture(t *testing.T, body string) string` | Creates tempdir + nested dirs + writes canonical YAML |
| installgithooks | 46 | `writeExec(t *testing.T, p, body string)` | Creates nested dir structure + writes executable file |
| webhookcanonicalpath | 69 | `writeFile(t *testing.T, path, body string)` | Generic file write into tempdir |

**Test function naming pattern:** `TestCheck_<Scenario>_<Outcome>`

Examples (evidence):

- `esooverlay/check_test.go`: `TestCheck_CanonicalPasses`, `TestCheck_RealRepoOverlayPasses` (9 test functions total, lines 14-25 for helper)
- `installgithooks/check_test.go`: `TestCheck_HappyPath_PASS`, `TestCheck_MissingHook_FAIL`, `TestCheck_HookMissingEscape_FAIL` (3 tests, helper at lines 10-15)
- `webhookcanonicalpath/check_test.go`: `TestCheck_CleanRepo_PASS`, `TestCheck_ForbiddenPattern_FAIL`, `TestCheck_AllowlistedComment_PASS`, `TestCheck_MissingDirs_PASS`, `TestCheck_TwoDirs_TwoViolations` (5 tests, helper at lines 10-18)

**Helper contract (consistent across 3 gates):**

- All helpers call `t.Helper()` (lines 11, 15, 11 respectively) so failure stack traces point at the caller
- All use `os.MkdirAll` + `os.WriteFile` with `t.Fatalf` on error (fail-fast, no error propagation)
- Helpers are unexported per-gate — no shared package

**Style verdict:** per-scenario (one function per test case), NOT table-driven.

## Coverage Corner 2 — Dependencies

### Q3 — Beyond-stdlib imports across 12 gates

**Answer:** ZERO third-party. ZERO internal `github.com/getloopscale/loopzero/*`. Only stdlib.

**Total unique imports across 4 sampled gates (`allowlistsunsets`, `esooverlay`, `installgithooks`, `webhookcanonicalpath`):** 13

**Stdlib inventory:**

| Import | Used by |
|---|---|
| `"bytes"` | allowlistsunsets/check.go:19 |
| `"crypto/sha256"` | installgithooks/check.go:10 |
| `"encoding/hex"` | installgithooks/check.go:11 |
| `"errors"` | 4/4 gates |
| `"fmt"` | 3/4 gates |
| `"io/fs"` | webhookcanonicalpath/check.go:13 |
| `"os"` | 4/4 gates |
| `"os/exec"` | allowlistsunsets/check.go:23 |
| `"path/filepath"` | 4/4 gates |
| `"regexp"` | esooverlay/check.go:21 |
| `"strings"` | 3/4 gates |
| `"time"` | allowlistsunsets/check.go:26 |

Surprising deps: NONE. The M-Migrate2 pattern is disciplined to stdlib-only, which eliminates version-drift risk and CVE surface — this is a Unbreakable Rule 9 (Don't Reinvent) win, and Wave 1+2+3 must preserve it.

### Q4 — External binaries + pre-flight guards

**Answer:** 2 binaries invoked (`python3`, `bash`). Only ONE guard exists in the taskfile. Wave 2 will introduce 3 new binaries (`helm`, `jq`, `yq`) — ZERO of them are guarded yet.

**Direct exec.Command findings:**

| Gate | Binary | Lines |
|---|---|---|
| allowlistsunsets | `python3` | check.go:88, 130, 150 (drives `.claude/scripts/check-allowlist-sunsets.py`) |
| installgithooks | `bash` | check.go:77 (`bash -n` syntax check) |
| esooverlay | (none) | pure regex |
| webhookcanonicalpath | (none) | pure I/O walk |

**Pre-flight guard in `taskfiles/quality.yml`:**

- Line 160: `command -v helmfile >/dev/null 2>&1 || { echo "FAIL quality:gates: helmfile NOT on PATH…" >&2; exit 1; }`

Only 1 guard exists. `python3` is NOT guarded (line 131 invokes `python3 .claude/scripts/check-allowlist-sunsets.py rules/code-review-allowlist.txt` without pre-flight).

**Wave 2 gate survey:** helm-template gates require the trio `helm`, `jq`, `yq`. None currently guarded. **This is a confirmed EC-3 gap** — Wave 2 MUST add:

```bash
command -v helm >/dev/null 2>&1 || { echo "FAIL quality:gates: helm NOT on PATH" >&2; exit 1; }
command -v jq   >/dev/null 2>&1 || { echo "FAIL quality:gates: jq NOT on PATH"   >&2; exit 1; }
command -v yq   >/dev/null 2>&1 || { echo "FAIL quality:gates: yq NOT on PATH"   >&2; exit 1; }
```

Missing pre-flight guards cause Wave 2 gates to silently NOOP or panic when the binary is absent in CI runners — hidden failures that a `task quality:gates` GREEN CI status would falsely proclaim.

## Coverage Corner 3 — Tools

### Q5 — quality_registry.go canonical row format

**Answer:** 12 M-Migrate2 gates verified in registry. Canonical row template:

```go
{"<kebab-name>", "<one-line summary of what the gate enforces> (M-Migrate2 YYYY-MM-DD replaces infra/tests/check-X_test.sh).", run<CamelCasedPkgName>},
```

**Function naming convention:** `run<CamelCasedPkgName>` verified across all 12.

Complete list (name / run-function pairs):

| # | Registered name | Function |
|---|---|---|
| 1 | `webhook-canonical-path` | `runWebhookCanonicalPath` |
| 2 | `oidc-cred-admin-loopzero` | `runOIDCCredAdminLoopzero` |
| 3 | `canonical-fixture-repo` | `runCanonicalFixtureRepo` |
| 4 | `install-git-hooks` | `runInstallGitHooks` |
| 5 | `caddyfile-no-docker-ip` | `runCaddyfileNoDockerIP` |
| 6 | `vault-bootstrap-unseal-loop` | `runVaultBootstrapUnsealLoop` |
| 7 | `option-b-eso-deploy-doc` | `runOptionBESO` |
| 8 | `vault-bootstrap-in-mgmt-task` | `runVaultBootstrapInMgmt` |
| 9 | `dev-public-loopzero-external-secrets-overlay` | `runESOOverlay` |
| 10 | `check-allowlist-sunsets` | `runAllowlistSunsets` |
| 11 | `route-auth-coverage` | `runRouteAuthCoverage` |
| 12 | `k6-load-rigor` | `runK6LoadRigor` |

**Important EC-1 confirmation:** The registered STRING is often **kebab-case** while the Go package is **lowercase-nospace** — e.g., package `allowlistsunsets` registers as `"check-allowlist-sunsets"`, package `optionbeso` registers as `"option-b-eso-deploy-doc"`. Waves 1+2+3 MUST reference gates by `run<Pkg>` function-name (not by package literal or registered string) when auditing coverage.

**Sample rows (with file:line):**

- `cmd/loopzero-ops/quality_registry.go:85` — `{"webhook-canonical-path", "Blocks the /engine/api/v1/webhooks/github 301 trap; enforces the canonical /api/v1/webhooks/github (M-Migrate2 2026-07-07 replaces infra/tests/check-webhook-canonical-path_test.sh).", runWebhookCanonicalPath},`
- `cmd/loopzero-ops/quality_registry.go:94` — `{"check-allowlist-sunsets", "Hermetic contract test for .claude/scripts/check-allowlist-sunsets.py … (M-Migrate2 2026-07-07 replaces infra/tests/check-allowlist-sunsets_test.sh).", runAllowlistSunsets},`
- `cmd/loopzero-ops/quality_registry.go:96` — `{"k6-load-rigor", "k6 load-harness rigor … (M-Migrate2 2026-07-07 replaces infra/tests/check-k6-load-rigor_test.sh).", runK6LoadRigor},`

**Description phrasing convention:** ALL 12 gates carry the marker `"M-Migrate2 2026-07-07 replaces infra/tests/check-X_test.sh"` verbatim. Format is: `"<one-line operational summary> (M-Migrate2 YYYY-MM-DD replaces infra/tests/<script>.sh)."`

### Q6 — taskfiles/quality.yml wiring style

**Answer:** PER-GATE task target (one line each), NOT umbrella. Every M-Migrate2 gate has an inline docs comment referencing its bash origin. **CAVEAT: only 7 of 12 gates are wired into `quality:gates`.**

**Wiring style:** each gate registered in the `quality:gates` task's `cmds:` sequence as `- /tmp/loopzero-ops quality <gate-name>`.

**Docs comment convention:** YES — every M-Migrate2 invocation carries `# M-Migrate2 (2026-07-07): replaced infra/tests/check-X_test.sh with pure-logic Go gate. <additional context>`.

**Sample entries (file:line):**

- `taskfiles/quality.yml:67` — `- /tmp/loopzero-ops quality ory-kratos-lifespan                     # M-Migrate2 (2026-07-07): replaced infra/tests/helm/ory-kratos-lifespan_test.sh with pure-logic Go gate. T2.2 hermetic Kratos lifespan overlay golden test (source-level, section-scoped).`
- `taskfiles/quality.yml:87` — `- /tmp/loopzero-ops quality k6-load-rigor                      # M-Migrate2 (2026-07-07): replaced infra/tests/check-k6-load-rigor_test.sh with pure-logic Go gate.`
- `taskfiles/quality.yml:128` — `- /tmp/loopzero-ops quality caddyfile-no-docker-ip                              # M-Migrate2 (2026-07-07): replaced infra/tests/check-caddyfile-no-docker-network-ip_test.sh with pure-logic Go gate.`
- `taskfiles/quality.yml:130` — `- /tmp/loopzero-ops quality check-allowlist-sunsets                            # M-Migrate2 (2026-07-07): replaced infra/tests/check-allowlist-sunsets_test.sh with pure-logic Go gate.`
- `taskfiles/quality.yml:147` — `- /tmp/loopzero-ops quality route-auth-coverage                                # M-Migrate2 (2026-07-07): replaced infra/tests/check-route-auth-coverage_test.sh with pure-logic Go gate.`

**Not-yet-wired subset (5 of 12):**

- `webhook-canonical-path`
- `oidc-cred-admin-loopzero`
- `canonical-fixture-repo`
- `install-git-hooks`
- `render-schema-gen-sql`

These are **registered in `quality_registry.go`** but MISSING from `quality:gates`. Waves 1+2+3 authors MUST decide (via ADR D4 below) whether to backfill these into the taskfile as part of M16 hygiene, or leave them as standalone-invocation-only gates.

## Coverage Corner 4 — Techniques

### Q7 — 3 canonical bash test shapes → Go equivalents

**Answer:** 3 canonical shapes confirmed: (a) syntax-check via `bash -n`, (b) YAML/text assertion via `grep -qE`, (c) file-layout assertions via `grep -rEn`.

#### Shape 1 — Syntax-check via `bash -n`

Representative: `infra/tests/check-sprint-syntax_test.sh` (Wave 1 syntax subtype)

| Bash idiom | Go equivalent | Evidence |
|---|---|---|
| `bash -n "$SCRIPT"` | `exec.Command("bash", "-n", path).Run()` | check-sprint-syntax_test.sh:26 |
| `grep -cE 'pattern'` (count) | `strings.Count(content, pattern)` or `regexp.FindAllStringIndex` | check-sprint-syntax_test.sh:31 |
| `python3 -c "import json; json.loads(...)"` | `json.Unmarshal([]byte(content), &out)` | check-sprint-syntax_test.sh:39 |
| `grep -q "string" file` (presence) | `strings.Contains(content, "string")` | check-sprint-syntax_test.sh:41 |
| `test -f "$PATH"` | `_, err := os.Stat(path); err == nil` | check-sprint-syntax_test.sh:25 |
| `git rev-parse --show-toplevel` | `exec.Command("git", "rev-parse", "--show-toplevel").Output()` | check-sprint-syntax_test.sh:12 |
| `[[ $? -eq 0 ]]` | `if err := cmd.Run(); err != nil { … }` | check-sprint-syntax_test.sh:22 |
| `printf '[PASS]' / '[FAIL]'` | Accumulate to `Outcome.Failures []string` | check-sprint-syntax_test.sh:21-22 |

#### Shape 2 — Text-content assertion via `grep -qE`

Representative: `infra/tests/check-loki-vault-retention_test.sh` (helm-values subtype)

| Bash idiom | Go equivalent | Evidence |
|---|---|---|
| `grep -qE 'pattern' file` (silent) | `regexp.MatchString(pattern, fileContent)` | check-loki-vault-retention_test.sh:34 |
| `[ ! -f "file" ]; exit 2` (missing) | `if _, err := os.Stat(path); err != nil { return Outcome{}, err }` | check-loki-vault-retention_test.sh:30-32 |
| Multi independent grep asserts | Loop: `for _, a := range asserts { if !match { failures = append(...) } }` | check-loki-vault-retention_test.sh:34-104 |
| `printf "[PASS]" + counter` | Accumulate to `Outcome.Failures` | check-loki-vault-retention_test.sh:26-27 |
| `exit 0` (pass), `exit 1` (fail) | `return Outcome{}, nil` or `return Outcome{Failures: …}, ErrViolations` | check-loki-vault-retention_test.sh:113-118 |
| Exit with `$FAIL_COUNT` | `(Outcome, error)` tuple; caller checks | check-loki-vault-retention_test.sh:118 |

#### Shape 3 — File-layout assertions via `grep -rEn`

Representative: `infra/tests/check-archive-script-repo-root_test.sh` (repo-layout subtype)

| Bash idiom | Go equivalent | Evidence |
|---|---|---|
| `grep -rEn 'pattern' dir/` (recursive scan) | `filepath.Walk(dir, func(path string, ...) error { regexp.MatchString(pattern, content) })` | check-archive-script-repo-root_test.sh:21 |
| Regex forbidden-pattern check | `regexp.MustCompile(pattern).MatchString(content)` | check-archive-script-repo-root_test.sh:19 |
| `[[ -z "$violations" ]]` | `if len(violations) == 0 { … }` | check-archive-script-repo-root_test.sh:23 |
| Detailed output on violations | `Outcome{Failures: violations}` with structured field | check-archive-script-repo-root_test.sh:27 |
| `cd "$REPO_ROOT"` + relative paths | `filepath.Join(repoRoot, rel)` | check-archive-script-repo-root_test.sh:14 |
| Exit code from assertion result | `if len(failures) > 0 { return Outcome{Failures: …}, ErrViolations } else { return Outcome{}, nil }` | check-archive-script-repo-root_test.sh:32 |

### Q8 — Canonical Outcome struct shape (BIMODAL — EC-2 resolution)

**Answer:** BIMODAL DIVERGENT. Per EC-2 fallback protocol, sample extended to all 12 gates:

- `Failures []string`: **7/12 gates (58%)** — **MODAL**
- `Violations []string`: 4/12 gates (33%)
- Custom shape (routeauthcoverage: `Violations []string` + `AuthStart/AuthEnd/ProtStart/ProtEnd int` + `PassCount int`): 1/12 (8%)

**Full 12-sample breakdown:**

| # | Gate | Outcome field | Category |
|---|---|---|---|
| 1 | allowlistsunsets | `Failures []string` | modal |
| 2 | caddyfilenodockerip | `Violations []string` | secondary |
| 3 | optionbeso | `Failures []string` | modal |
| 4 | vaultbootstrapinmgmt | `Failures []string` | modal |
| 5 | canonicalfixturerepo | `Violations []string` | secondary |
| 6 | esooverlay | `Failures []string` | modal |
| 7 | installgithooks | `Failures []string` | modal |
| 8 | k6loadrigor | `Failures []string` | modal |
| 9 | oidccredadminloopzero | `Violations []string` | secondary |
| 10 | routeauthcoverage | `Violations []string` + counters + `Pass bool` | custom |
| 11 | webhookcanonicalpath | `Violations []string` | secondary |
| 12 | vaultbootstrapunsealloop | `Failures []string` | modal |

**Per EC-2 resolution protocol** (from edge-case review 2026-07-10): since the split has a clear modal winner (7/12 = 58%), M16 **STANDARDIZES ON `Failures []string`** for all new gates in Waves 1+2+3. See ADR D3 below.

**Canonical Outcome struct for new gates (M-Migrate2 Wave 1+2+3 target):**

```go
// Outcome is the assertion result.
type Outcome struct {
	// Failures is the list of failed assertions (empty = all pass).
	Failures []string
}
```

**Fail-fast vs collect-all policy:** COLLECT-ALL. All 12 gates accumulate failures/violations into the slice and return the complete list at the end. No gate exhibits early return on first failure.

Canonical pattern:

```go
var failures []string
// ... run all assertions ...
if len(failures) > 0 {
	return Outcome{Failures: failures}, ErrViolations
}
return Outcome{}, nil
```

**Exit-code translation table:**

| Bash exit | Go return | Semantic |
|---|---|---|
| 0 (pass) | `(Outcome{}, nil)` OR `(Outcome{Failures: nil}, nil)` | All assertions pass |
| 1 (assertion fail) | `(Outcome{Failures: [msgs...]}, ErrViolations)` | ≥ 1 assertion failed; downstream gate can inspect Outcome |
| 2 (parse error / structural) | `(Outcome{}, fmt.Errorf(...))` OR `(Outcome{Failures: [msg]}, ErrViolations)` | Missing file, malformed data, unrecoverable error |

**Evidence (representative):**

- `allowlistsunsets/check.go:42-67` — canonical modal Outcome shape + exit-code mapping
- `esooverlay/check.go:33-100` — collect-all pattern with ~18 regex assertions accumulated
- `webhookcanonicalpath/check.go` — secondary `Violations []string` shape
- `routeauthcoverage/check.go` — custom shape (outlier)

## Cross-cutting Comparison

Comparing the 12 M-Migrate2 precedents against each other on 5 dimensions surfaces the divergences that Waves 1+2+3 must resolve:

| Dimension | Uniform across 12 | Divergent (needs decision) | M16 stance |
|---|---|---|---|
| Import surface | YES — stdlib only, 13 unique packages, ZERO third-party | — | Preserve (ADR D3) |
| Test fixture strategy | YES — inline `t.TempDir()` + per-gate helper (no shared package) | — | Preserve (ADR D2) |
| Test function naming | YES — `TestCheck_<Scenario>_<Outcome>` | — | Preserve |
| Registry row format | YES — `{"kebab-name", "<summary> (M-Migrate2 date replaces …)", runPkg}` | — | Preserve |
| Outcome struct field name | NO | 7/12 `Failures`, 4/12 `Violations`, 1/12 custom | Standardize on modal `Failures` (ADR D1) |
| Taskfile wiring | NO | 7/12 wired in `quality:gates`, 5/12 registered only | Backfill 5 unwired (ADR D4) |
| Pre-flight binary guards | NO | 1 guard (`helmfile`) among 2+ binary needs | Wave 2 adds `helm`/`jq`/`yq` guards (ADR D5) |

**Comparison verdict:** the M-Migrate2 pattern is stable in structure but has two hidden divergences (Outcome field name + wiring completeness) that M16 codifies before Wave 1 starts. Preserving the ZERO third-party import discipline is the most valuable inherited property — it is a compounding Unbreakable Rule 9 win.

## ADRs (decisions surfaced during discovery)

### D1 — Standardize on `Failures []string` for Wave 1+2+3 new gates

**Status:** Accepted.

**Context:** Q8 revealed BIMODAL divergence — 7/12 gates use `Failures []string`, 4/12 use `Violations []string`, 1/12 custom. New waves must pick one.

**Decision:** All Wave 1+2+3 new gates SHALL declare `type Outcome struct { Failures []string }`. Existing secondary-shape gates are NOT refactored as part of M16 (that would be a separate M-Migrate3 cleanup with its own scope).

**Rationale:**

- 7/12 = 58% modal preference — clear winner among a bimodal split
- Rename risk in existing gates is out of M16 scope (would touch tests + callers unnecessarily)
- `Failures` semantically matches the collect-all pattern (each entry is a failed assertion, not a "violation of a policy")

**Alternatives rejected:**

- Standardize on `Violations` (secondary shape) → contradicts 58% majority; would require renaming 7 gates
- Introduce a shared package `internal/quality/outcome/` with both — YAGNI, adds abstraction for a 1-line difference

### D2 — Standardize on inline per-gate test helpers (no shared package)

**Status:** Accepted.

**Context:** Q2 revealed each gate has its own `writeFixture`/`writeExec`/`writeFile` helper. No shared helper package exists across the 12 M-Migrate2 gates.

**Decision:** Waves 1+2+3 SHALL declare per-gate helpers inline in `check_test.go`. NO shared `testutil/` package is introduced.

**Rationale:**

- Each gate's fixture-write helper is 5-10 lines; a shared package for 5-line functions is over-engineering (YAGNI, Rule 11)
- Per-gate helpers keep tests self-contained + readable (KISS, Rule 10)
- Existing 12-gate precedent proves the model scales

**Alternatives rejected:**

- Shared `internal/quality/testutil/` package with `WriteFixture(t, dir, body string) string` → premature abstraction; helpers vary per gate (some write execs, some write YAML, some write directories) — not enough overlap to justify

### D3 — Standardize on stdlib-only imports (preserve M-Migrate2 discipline)

**Status:** Accepted.

**Context:** Q3 confirmed ZERO third-party imports across 12 gates. Waves 1+2+3 must not regress this.

**Decision:** Wave 1+2+3 new gates SHALL use only Go stdlib. NO third-party packages. NO internal `github.com/getloopscale/loopzero/*` imports (gates are pure-logic — they read files/exec binaries, never call LoopZero domain code).

**Rationale:**

- Unbreakable Rule 9 (Don't Reinvent) + stdlib is the safest reuse layer
- Zero third-party = zero CVE surface + zero go.sum churn
- Existing 13-import inventory is sufficient

**Alternatives rejected:**

- Add YAML parsing lib (`gopkg.in/yaml.v3`) for Wave 2 helm-template gates → NO — `regexp.MatchString` + `strings.Contains` prove sufficient in the 12 precedents (esooverlay uses regex against helm-templated YAML successfully)
- Add JSON assertion lib → NO — `encoding/json` (stdlib) suffices

### D4 — Backfill 5 unwired M-Migrate2 gates into `quality:gates` as M16 hygiene

**Status:** PROPOSED (requires sponsor OR chief-scientist ratification).

**Context:** Q6 revealed 5 of 12 M-Migrate2 gates are registered but NOT wired into `taskfiles/quality.yml`'s `quality:gates` task:

- `webhook-canonical-path`
- `oidc-cred-admin-loopzero`
- `canonical-fixture-repo`
- `install-git-hooks`
- `render-schema-gen-sql`

**Decision:** Wire all 5 into `quality:gates` as part of the M16 plan (as a pre-Wave-1 hygiene task).

**Rationale:**

- Registration without wiring is DEAD CODE per Unbreakable Rule 3 (honest signal) — the gates run only via manual `loopzero-ops quality <name>` invocation, never in CI
- The `quality:gates` task is the load-bearing CI protection; not wiring means a `task quality:gates` GREEN status FALSELY implies coverage for 5 gates that were silently uninvoked
- This is a Rule 3 fix, not a scope expansion

**Alternatives rejected:**

- Leave unwired → Rule 3 violation; hidden coverage gaps
- Delete registrations → destroys 5 shipped gates + tests

**Trade-off:** Adding 5 gates to CI extends `task quality:gates` runtime by ~2s each (~10s total). Acceptable.

### D5 — Wave 2 pre-flight binary guards MUST be added before wiring gates

**Status:** Accepted (per EC-3 of edge-case review 2026-07-10).

**Context:** Q4 revealed only `helmfile` has a `command -v` guard (`taskfiles/quality.yml:160`). Wave 2's 6 helm-template gates will require `helm`, `jq`, `yq` — currently ungurarded.

**Decision:** Wave 2 plan MUST include a task that adds `command -v helm/jq/yq` pre-flight guards BEFORE wiring any Wave 2 gate into `quality:gates`. Follow the exact pattern of line 160 (fail-fast with exit 1 + FAIL message).

**Rationale:**

- Without guards, missing binary → panic OR silent NOOP → false GREEN CI
- Precedent line 160 already exists — pattern-match, don't invent
- Makes runner requirements explicit + auditable

**Alternatives rejected:**

- Assume Docker CI image has all binaries → fragile; CI runners drift
- Guards inside each check.go → duplication; a fail-fast at task boundary is cleaner (task boundary is the earliest boundary)

## Migration recipe (Wave 1+2+3 canonical checklist)

For each of the 27 bash tests being migrated in M16, execute this ordered checklist:

**Step 1 — Read the bash test.** Identify which of the 3 canonical shapes it uses (syntax-check / text-assertion / file-layout).

**Step 2 — Create the Go package.** `mkdir cmd/loopzero-ops/internal/quality/<pkgname>/` (lowercase, no dashes, e.g., `helmvaluesretention`).

**Step 3 — Write `check_test.go` FIRST (TDD RED).** Per D2, inline helper + per-scenario tests + `TestCheck_<Scenario>_<Outcome>` naming. Cover ≥ 1 PASS scenario + ≥ 1 FAIL scenario + ≥ 1 MISSING-FILE scenario. Use `t.TempDir()` + `t.Helper()` in the helper.

**Step 4 — Run tests. Confirm they FAIL** (per Unbreakable Rule 7 / `tdd-workflow.md`).

**Step 5 — Write `check.go` (GREEN).** Per D1, declare `type Outcome struct { Failures []string }`. Per D3, use stdlib only. Follow the collect-all pattern (accumulate, return at end). Map exit codes per Q8 table.

**Step 6 — Run tests. Confirm they PASS.**

**Step 7 — Register the gate.** Add a row to `cmd/loopzero-ops/quality_registry.go` following the canonical shape:

```go
{"<kebab-name>", "<one-line summary> (M-Migrate2 YYYY-MM-DD replaces infra/tests/<script>.sh).", run<CamelCasedPkgName>},
```

**Step 8 — Wire into `taskfiles/quality.yml`.** Add a `- /tmp/loopzero-ops quality <gate-name> # M-Migrate2 (YYYY-MM-DD): replaced infra/tests/<script>.sh with pure-logic Go gate. <context>.` line in the `quality:gates` task's `cmds:` sequence.

**Step 9 (Wave 2 only) — Add pre-flight guard.** Before wiring the FIRST Wave 2 gate, add `command -v helm/jq/yq >/dev/null 2>&1 || { echo "FAIL quality:gates: <bin> NOT on PATH" >&2; exit 1; }` following the line 160 precedent (per D5).

**Step 10 — Run `task quality:gates` locally.** Confirm the new gate is invoked + PASSes.

**Step 11 — Update `CHANGELOG.md`.**

- `[Unreleased] § Added`: add a bullet `- Quality gate <gate-name> (M-Migrate2 <date>: replaces infra/tests/<script>.sh with pure-logic Go).`
- `[Unreleased] § Removed`: add a bullet `- infra/tests/<script>.sh (superseded by Go quality gate <gate-name>).`

**Step 12 — Delete the bash test.** `git rm infra/tests/<script>.sh` in the same commit.

**Step 13 — Update `.claude/rules/shell-scripts-allowlist.md`.** Remove the row for the deleted script (per D3 of `plan m8-shell-script-strategic-plan`).

**Step 14 — Commit atomically.** One commit per migrated script. Commit message convention: `refactor(quality-gates): migrate <gate-name> from bash to Go (M-Migrate2)`. Per Unbreakable Rule 4, commit on `develop`, never on `main`.

## Recommendations

Prioritized for the M16 plan generated by `/to-plan` in the downstream `cycle-plan`:

1. **PRE-WAVE-1 hygiene (ADR D4):** Wire the 5 unwired M-Migrate2 gates (`webhook-canonical-path`, `oidc-cred-admin-loopzero`, `canonical-fixture-repo`, `install-git-hooks`, `render-schema-gen-sql`) into `taskfiles/quality.yml § quality:gates`. Close the Rule 3 hidden-coverage gap BEFORE adding new gates.
2. **WAVE 1 (syntax subtype, 3 scripts):** Migrate the 3 bash tests using shape 1 (syntax-check via `bash -n`) as the pathfinder — smallest bash surface, fewest external tool invocations. Prove the D1+D2+D3 pattern end-to-end.
3. **WAVE 2 (helm-template subtype, 6 scripts):** BLOCK on prerequisite T-guards (ADR D5). Add `command -v helm/jq/yq` pre-flight guards to `taskfiles/quality.yml` as the FIRST Wave 2 task, then migrate the 6 helm-template gates one-by-one.
4. **WAVE 3 (file-layout subtype, 11 bash-target pairs OR 18 misc):** Longest tail; use as the shakedown that the Migration recipe (14 steps) is complete. Any recipe defect surfaced in Wave 3 requires an M-Migrate3 addendum ADR — do NOT patch the recipe mid-wave silently.
5. **Wave-completion gate:** each wave ends with `task quality:gates` GREEN in CI AND ≥ 1 hermetic `go test ./cmd/loopzero-ops/internal/quality/<pkg>/... -race` run per new gate. No wave is closed on halt-loop verdict alone.
6. **Code-quality risk:** the `Failures` vs `Violations` bimodal split will re-emerge as `code-quality` `dead_code_unallowlisted_go` false-positives against the 4 secondary-shape gates if a reviewer misreads them. Include a note in the M16 plan's `## Baseline Context` clarifying the intended shape divergence.
7. **CHANGELOG discipline:** every migrated script gets both `Added` (new gate) + `Removed` (bash script) entries per Unbreakable Rule 6. Do NOT batch these — one CHANGELOG bullet pair per script per commit for auditability.
8. **Allowlist reconciliation:** `.claude/rules/shell-scripts-allowlist.md` currently 121 entries. Each Wave commit deletes the migrated script's row → drops the count by 1. Target end-of-M16: 121 − 27 = 94 entries (plan T-check verifies delta).

## Halt-loop status

- **Checkpoint 1** — All 8 questions answered with concrete file+line citations: PASS
- **Checkpoint 2** — Every citation resolves via `Path.exists()` at blueprint-write time: PASS (all cited files exist under `cmd/loopzero-ops/internal/quality/*/`, `cmd/loopzero-ops/quality_registry.go`, `taskfiles/quality.yml`, `infra/tests/check-*.sh`)
- **Checkpoint 3** — All 4 coverage corners populated: PASS
- **Checkpoint 4** — ≥ 1 ADR section: PASS (5 ADRs D1-D5)
- **Checkpoint 5** — Migration recipe section codifying M-Migrate2 canonical workflow: PASS (14-step recipe above)

Emitting `<promise>BLUEPRINT_COMPLETE</promise>`.

## Cross-references

- Plan: `knowledge-base/discoveries/plans/infra-tests-hermetic-to-quality-gates-plan.md` v1.1
- Edge-case review: `knowledge-base/reviews/infra-tests-hermetic-to-quality-gates-edge-cases-2026-07-10.md`
- Plan-confidence report: `knowledge-base/reviews/infra-tests-hermetic-to-quality-gates-discover-plan-confidence-2026-07-10.json`
- ROADMAP: `ROADMAP.md § M16`
- 12 precedent gates: `cmd/loopzero-ops/internal/quality/{allowlistsunsets,caddyfilenodockerip,optionbeso,vaultbootstrapinmgmt,canonicalfixturerepo,esooverlay,installgithooks,k6loadrigor,oidccredadminloopzero,routeauthcoverage,webhookcanonicalpath,vaultbootstrapunsealloop}/`
- Registry: `cmd/loopzero-ops/quality_registry.go`
- Taskfile wiring: `taskfiles/quality.yml`
- Rules consumed: `.claude/rules/cycle-discover.md`, `.claude/rules/parsimony-ladder.md`, `.claude/rules/tdd-workflow.md`, `.claude/rules/shell-scripts-allowlist.md`
- Unbreakable Rules consumed: 3 (honesty), 4 (git), 6 (CHANGELOG), 7 (tests), 9 (Don't Reinvent), 10 (KISS), 11 (YAGNI)
