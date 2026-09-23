// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// TestDetectsMissingScriptTarget — a *_test.sh whose SCRIPT= points at a repo
// path that does not exist is the exact relocation-rot class (drill-* moved to
// dr/* without repointing the test). It MUST be flagged.
func TestDetectsMissingScriptTarget(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/tests/scripts/foo_test.sh": "#!/bin/bash\nSCRIPT=\"$ROOT/infra/scripts/gone.sh\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL (missing target), got Pass")
	}
	if len(out.Violations) != 1 {
		t.Errorf("expected 1 violation, got %d: %v", len(out.Violations), out.Violations)
	}
	if len(out.Violations) > 0 && !strings.Contains(out.Violations[0], "gone.sh") {
		t.Errorf("violation should name gone.sh, got %q", out.Violations[0])
	}
}

// TestPassesWhenTargetExists — SCRIPT= pointing at an existing repo file is OK.
func TestPassesWhenTargetExists(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/scripts/dr/real.sh":       "#!/bin/bash\nexit 0\n",
		"infra/tests/scripts/ok_test.sh": "#!/bin/bash\nSCRIPT=\"$ROOT/infra/scripts/dr/real.sh\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS, got violations: %v", out.Violations)
	}
}

// TestSkipsTmpdirFixturePaths — paths under $tmpdir/$WORK are test-created stubs,
// NOT repo targets; they must NOT be flagged even though the repo file is absent.
func TestSkipsTmpdirFixturePaths(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/tests/scripts/stub_test.sh": "#!/bin/bash\nSCRIPT=\"$tmpdir/infra/scripts/stub.sh\"\nLIB=\"$WORK/infra/scripts/lib/x.sh\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("tmpdir/WORK fixture paths must be skipped, got: %v", out.Violations)
	}
}

// TestSkipsUnresolvableVarPaths — a value with a non-root variable prefix (no
// repo-relative literal) cannot be resolved and must be skipped, not flagged.
func TestSkipsUnresolvableVarPaths(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/tests/scripts/var_test.sh": "#!/bin/bash\nSCRIPT=\"$SOMEDIR/$thing.sh\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("unresolvable var path must be skipped, got: %v", out.Violations)
	}
}

// TestHandlesDefaultValueSyntax — SCRIPT_UNDER_TEST="${X:-infra/scripts/real.sh}".
func TestHandlesDefaultValueSyntax(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/scripts/quality/real.sh":   "#!/bin/bash\nexit 0\n",
		"infra/tests/scripts/def_test.sh": "#!/bin/bash\nSCRIPT_UNDER_TEST=\"${SCRIPT_UNDER_TEST:-infra/scripts/quality/real.sh}\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("default-value syntax with existing target must PASS, got: %v", out.Violations)
	}
}

// TestDetectsMissingDefaultValueTarget — same default syntax but target missing.
func TestDetectsMissingDefaultValueTarget(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/tests/scripts/def2_test.sh": "#!/bin/bash\nSCRIPT_UNDER_TEST=\"${X:-infra/scripts/quality/gone.sh}\"\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("expected FAIL on missing default-value target")
	}
}

// TestSkipsExcludedDirs — a broken test under node_modules/worktrees is pruned.
func TestSkipsExcludedDirs(t *testing.T) {
	root := fixture(t, map[string]string{
		"node_modules/x/bad_test.sh":          "#!/bin/bash\nSCRIPT=\"$ROOT/infra/scripts/gone.sh\"\n",
		".claude/worktrees/w/infra/y_test.sh": "#!/bin/bash\nSCRIPT=\"$ROOT/infra/scripts/gone.sh\"\n",
		"infra/tests/ok_test.sh":              "#!/bin/bash\necho ok\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("excluded dirs must be pruned, got: %v", out.Violations)
	}
}

// TestEmptyRootDefaultsToDot — Config{} scans "." and, over a directory with no
// test script, reports that it read nothing rather than a clean tree.
func TestEmptyRootDefaultsToDot(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := Check(Config{})

	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v, want ErrNothingScanned", err)
	}
}

// TestUnreadableTestScriptIsAnError — a test script the gate cannot read is not
// a script with no targets.
func TestUnreadableTestScriptIsAnError(t *testing.T) {
	root := fixture(t, map[string]string{"infra/tests/ok_test.sh": "#!/bin/bash\n"})
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "infra/tests/gone_test.sh")); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{Root: root}); err == nil {
		t.Fatal("an unreadable *_test.sh must be an error")
	}
}

// TestOnlyScansTestScripts — a non-test .sh with a SCRIPT= line is NOT scanned
// (the gate's scope is *_test.sh target integrity).
func TestOnlyScansTestScripts(t *testing.T) {
	root := fixture(t, map[string]string{
		"infra/scripts/helper.sh": "#!/bin/bash\nSCRIPT=\"$ROOT/infra/scripts/gone.sh\"\n",
		"infra/tests/ok_test.sh":  "#!/bin/bash\necho ok\n",
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("non-test .sh must not be scanned, got: %v", out.Violations)
	}
}
