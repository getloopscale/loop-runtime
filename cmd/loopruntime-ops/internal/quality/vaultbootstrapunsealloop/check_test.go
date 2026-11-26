// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vaultbootstrapunsealloop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/qtestutil"
)

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "scripts", "local")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vault-bootstrap-dev.sh"), []byte(body), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

// canonical is a minimal fixture that satisfies every invariant. It mirrors
// the shape of the real vault-bootstrap-dev.sh Steps 2 + 3 canonical fix.
const canonical = `#!/usr/bin/env bash
# CANONICAL FIX 2026-06-15: bao rc-independent parse.
# Root cause: exit-code SEMANTICS + pipefail+bao rc=2 propagation triggered lowercase fallback.
# Per sponsor mandate 2026-06-15 "sem WORKAROUNDs". # english-only: fixture must carry the verbatim quote
set -uo pipefail

# --- Step 2: init check ---
BAO_STATUS_JSON=$(bao status -format=json 2>/dev/null || true)
INIT_STATUS=$(echo "$BAO_STATUS_JSON" | python3 -c "
import json,sys
try:
  s = json.loads(sys.stdin.read() or '{}')
  print(s.get('initialized', False))
except Exception:
  print(False)
" 2>/dev/null || echo "False")
if [[ "$INIT_STATUS" != "True" ]]; then
  bao operator init -key-shares=5 -key-threshold=3 -format=json > /tmp/init.json
fi

# --- Step 3: sealed check + unseal loop ---
  BAO_STATUS_JSON=$(bao status -format=json 2>/dev/null || true)
SEALED=$(echo "$BAO_STATUS_JSON" | python3 -c "
import json,sys
try:
  s = json.loads(sys.stdin.read() or '{}')
  print(s.get('sealed', True))
except Exception:
  print(True)
" 2>/dev/null || echo "True")
while [[ "$SEALED" == "True" ]]; do
  bao operator unseal $SHARD
done
`

func TestCheck_CanonicalPasses(t *testing.T) {
	root := writeFixture(t, canonical)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("expected PASS, got err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_MissingTargetFails(t *testing.T) {
	root := t.TempDir()
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations, got %v", err)
	}
}

func TestCheck_BuggyDirectPipeFails(t *testing.T) {
	// Injects the ORIGINAL buggy pattern on a non-comment line.
	buggy := canonical + "\n" + `INIT_STATUS=$(bao status -format=json 2>/dev/null | python3 -c "print('True')")` + "\n"
	root := writeFixture(t, buggy)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for buggy direct-pipe, got %v (failures=%v)", err, outcome.Failures)
	}
}

func TestCheck_LowercaseTrueFallbackFails(t *testing.T) {
	// Injects the `|| echo "true"` (lowercase) trap on a SEALED= assign path.
	buggy := canonical + "\n" + `SEALED=$(echo "$BAO_STATUS_JSON" | python3 -c "print('True')" || echo "true")` + "\n"
	root := writeFixture(t, buggy)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for lowercase 'true' fallback, got %v (failures=%v)", err, outcome.Failures)
	}
}

func TestCheck_LowercaseTrueInCommentIsAllowed(t *testing.T) {
	// A comment-line reference to `echo "true"` should NOT fail. The scan
	// skips lines whose trimmed prefix starts with `#`.
	withComment := canonical + "\n" + `# forbidden: SEALED=$(x || echo "true")` + "\n"
	root := writeFixture(t, withComment)
	outcome, err := violationOf(Check(Config{RepoRoot: root}))
	if err != nil {
		t.Fatalf("comment-line ref should NOT fail. err=%v failures=%v", err, outcome.Failures)
	}
}

func TestCheck_MissingAuditTrailFails(t *testing.T) {
	// Strip the "CANONICAL FIX 2026-06-15" audit marker.
	body := ""
	for _, line := range splitLines(canonical) {
		if contains(line, "CANONICAL FIX 2026-06-15") {
			continue
		}
		body += line + "\n"
	}
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations for missing CANONICAL FIX marker, got %v", err)
	}
}

func TestCheck_MissingCaptureCountFails(t *testing.T) {
	// Only 1 BAO_STATUS_JSON capture (Step 3 stripped).
	body := ""
	seen := 0
	for _, line := range splitLines(canonical) {
		if contains(line, "BAO_STATUS_JSON=$(bao status") {
			seen++
			if seen >= 2 {
				continue
			}
		}
		body += line + "\n"
	}
	root := writeFixture(t, body)
	_, err := violationOf(Check(Config{RepoRoot: root}))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("expected ErrViolations when only 1 capture present, got %v", err)
	}
}

func TestCheck_RealRepoScriptPasses(t *testing.T) {
	abs := qtestutil.RootFile(t, filepath.Join("infra", "scripts", "local", "vault-bootstrap-dev.sh"))
	outcome, checkErr := violationOf(Check(Config{RepoRoot: abs}))
	if checkErr != nil {
		t.Fatalf("real script must satisfy invariant. err=%v failures=%v", checkErr, outcome.Failures)
	}
}

// Small helpers to keep the test file self-contained.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
