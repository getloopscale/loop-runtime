// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package prodimagedigests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: build a temp chart-like dir with files mapped via the
// (relative-path → content) map.
func writeTestTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCheck_HelperInclude_NoViolation(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopruntime-api/templates/deployment.yaml": `
spec:
  containers:
    - name: api
      image: {{ include "loopruntime-common.image" . }}
`,
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Errorf("helper include should NOT count as violation; got %v", out.Violations)
	}
	if !out.Pass {
		t.Error("warn-mode should Pass even with violations")
	}
}

func TestCheck_HardcodedTag_ReportedAsViolation(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopruntime-api/templates/sidecar.yaml": `
spec:
  containers:
    - name: vault-agent
      image: hashicorp/vault:1.18.0
`,
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 1 {
		t.Fatalf("want 1 violation, got %d: %v", len(out.Violations), out.Violations)
	}
	if !strings.Contains(out.Violations[0].String(), "hashicorp/vault:1.18.0") {
		t.Errorf("violation missing tag context: %q", out.Violations[0].String())
	}
	// `literal_tag` is enforced by default since
	// MEET-ASYNC-AMENDMENT-2026-08-17-digest-enforcement-has-two-populations: it
	// is the population the chart author can fix today, in one line, with a fix
	// that does not age.
	if out.Pass {
		t.Error("a literal :tag must fail the gate by default")
	}
}

func TestCheck_EnforceMode_ViolationFailsPass(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopruntime-api/templates/sidecar.yaml": `
spec:
  containers:
    - name: vault-agent
      image: hashicorp/vault:1.18.0
`,
	})
	out, err := Check(Config{Root: root, Enforce: EnforceAll()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Error("enforce-mode with violations should Fail")
	}
}

func TestCheck_DigestRef_NoViolation(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopruntime-api/templates/sidecar.yaml": `
spec:
  containers:
    - name: vault-agent
      image: hashicorp/vault@sha256:abc123def456789012345678901234567890abc123def456789012345678901
`,
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Errorf("digest ref should NOT be violation; got %v", out.Violations)
	}
}

func TestCheck_CommentedLine_Ignored(t *testing.T) {
	root := writeTestTree(t, map[string]string{
		"charts/loopruntime-api/templates/sidecar.yaml": `
spec:
  containers:
    # legacy: image: hashicorp/vault:1.17.0
    - name: vault-agent
      image: {{ include "loopruntime-common.image" . }}
`,
	})
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(out.Violations) != 0 {
		t.Errorf("commented line should be ignored; got %v", out.Violations)
	}
}

func TestCheck_MissingPath_SkippedSilently(t *testing.T) {
	// Use a tempdir that doesn't contain charts/ — Check should not error.
	root := t.TempDir()
	out, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check should not error on missing paths: %v", err)
	}
	if !out.Pass {
		t.Error("missing paths = nothing to violate, should Pass")
	}
	if out.ScannedFiles != 0 {
		t.Errorf("no files scanned, got %d", out.ScannedFiles)
	}
}

// TestCheck_LiveChartsRepo_MissingDigestStillOnlyWarns replaces a test that
// asserted "warn-mode should always Pass, regardless of how many violations exist
// today".
//
// That assertion encoded the contract this gate had for three months and which
// MEET-ASYNC-AMENDMENT-2026-08-17-digest-enforcement-has-two-populations
// supersedes: a single global mode. It is kept — narrowed — because the half it
// still protects is real and now easy to break by accident: promoting
// `missing_digest` to enforced before the image publisher emits the value would
// make the gate refuse seven findings whose fix does not exist.
func TestCheck_LiveChartsRepo_MissingDigestStillOnlyWarns(t *testing.T) {
	out, err := Check(Config{Root: repoRoot})
	if err != nil {
		t.Fatalf("Check on real repo: %v", err)
	}
	for _, v := range out.Enforced() {
		if v.Kind == KindMissingDigest {
			t.Fatalf("missing_digest must not block until the publisher emits image.digest: %s", v)
		}
	}
}
