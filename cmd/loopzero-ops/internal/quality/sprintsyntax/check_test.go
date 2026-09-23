// Hermetic tests for the sprintsyntax gate.
//
// Follows M-Migrate2 test-shape conventions from blueprint Q1+Q2:
//   - inline per-scenario tests (no shared testutil package)
//   - t.TempDir() + writeFile/writeExec helpers with t.Helper()
//   - TestCheck_<Scenario>_<Outcome> naming
//   - table-driven only for parameterized micro-checks (json shape)
//
// Migrated from bash test infra/tests/check-sprint-syntax_test.sh
// (M-Migrate2 2026-07-10 M16 T2.1). The 11 bash assertions map to these
// Go tests plus a happy-path check against the real repo.
package sprintsyntax

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// writeFile creates parent dirs + writes body at path. Fails the test on error.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writefile %s: %v", path, err)
	}
}

// writeExec is the same as writeFile but sets 0o755.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdirall %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writeexec %s: %v", path, err)
	}
}

// canonicalHelper reproduces the shape of infra/scripts/local/check_sprint.py
// (a compilable Python script that emits the expected JSON envelope).
const canonicalHelper = `#!/usr/bin/env python3
"""check_sprint.py helper — emits JSON report from CS_* env + CS_RESULTS_FILE."""
import json, os, sys
results = []
path = os.environ.get("CS_RESULTS_FILE", "")
if path:
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line: continue
            parts = line.split(":", 2)
            if len(parts) == 3:
                results.append(parts)
report = {
    "pass":    int(os.environ.get("CS_PASS", "0")),
    "warn":    int(os.environ.get("CS_WARN", "0")),
    "fail":    int(os.environ.get("CS_FAIL", "0")),
    "skip":    int(os.environ.get("CS_SKIP", "0")),
    "elapsed": int(os.environ.get("CS_ELAPSED", "0")),
    "results": results,
}
sys.stdout.write(json.dumps(report))
`

// canonicalScript reproduces the shape of infra/scripts/local/check-sprint.sh
// (a compilable bash script with NO python3 heredoc).
const canonicalScript = `#!/usr/bin/env bash
# check-sprint.sh — de-costume compliant: heredoc-free.
set -euo pipefail
python3 "$(dirname "$0")/check_sprint.py"
`

// canonicalAllowlist has a table row that includes check-sprint.sh.
const canonicalAllowlist = `| infra/scripts/local/check-sprint.sh | 42 | canonical |
| some/other.sh | 10 | grandfathered |
`

// heredocScript is a script that violates T3.2 by embedding a python3 heredoc.
const heredocScript = `#!/usr/bin/env bash
set -euo pipefail
python3 <<PY
print("hi")
PY
`

func TestCheck_HappyPath_PASS(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scriptRel), canonicalScript)
	writeExec(t, filepath.Join(root, helperRel), canonicalHelper)
	writeFile(t, filepath.Join(root, allowlistRel), canonicalAllowlist)

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected nil err, got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures, got %v", outcome.Failures)
	}
}

func TestCheck_MissingScript_FAIL(t *testing.T) {
	root := t.TempDir()
	// no script, no helper, no allowlist — expect ≥ 3 failures
	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
	if len(outcome.Failures) < 3 {
		t.Fatalf("expected ≥ 3 failures for empty tempdir, got %v", outcome.Failures)
	}
}

func TestCheck_BashSyntaxError_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scriptRel), "#!/usr/bin/env bash\nif then fi\n")
	writeExec(t, filepath.Join(root, helperRel), canonicalHelper)
	writeFile(t, filepath.Join(root, allowlistRel), canonicalAllowlist)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for bash syntax error, got %v", err)
	}
	if !containsSubstring(outcome.Failures, "bash -n") {
		t.Fatalf("expected 'bash -n' failure, got %v", outcome.Failures)
	}
}

func TestCheck_HeredocViolation_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scriptRel), heredocScript)
	writeExec(t, filepath.Join(root, helperRel), canonicalHelper)
	writeFile(t, filepath.Join(root, allowlistRel), canonicalAllowlist)

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for heredoc violation, got %v", err)
	}
	if !containsSubstring(outcome.Failures, "python3 heredoc") {
		t.Fatalf("expected 'python3 heredoc' failure, got %v", outcome.Failures)
	}
}

func TestCheck_ScriptNotAllowlisted_FAIL(t *testing.T) {
	root := t.TempDir()
	writeExec(t, filepath.Join(root, scriptRel), canonicalScript)
	writeExec(t, filepath.Join(root, helperRel), canonicalHelper)
	writeFile(t, filepath.Join(root, allowlistRel), "| some/other.sh | 10 | x |\n")

	outcome, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing allowlist row, got %v", err)
	}
	if !containsSubstring(outcome.Failures, "allowlisted") {
		t.Fatalf("expected 'allowlisted' failure, got %v", outcome.Failures)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	// Traverse from this test file up to the repo root by locating ROADMAP.md.
	// go.mod lives at cmd/loopzero-ops (submodule) — ROADMAP.md is the repo-root anchor.
	root := qtestutil.Root(t)

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("expected real repo PASS, got %v; failures=%v", err, outcome.Failures)
	}
	if len(outcome.Failures) != 0 {
		t.Fatalf("expected 0 failures against real repo, got %v", outcome.Failures)
	}
}

// containsSubstring reports whether any element of failures contains substr.
func containsSubstring(failures []string, substr string) bool {
	for _, f := range failures {
		if len(f) >= len(substr) && containsAt(f, substr) {
			return true
		}
	}
	return false
}

func containsAt(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
