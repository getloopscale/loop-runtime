// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package clusterimagepolicymode

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T, valuesBody string, adrFiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if valuesBody != "<absent>" {
		p := filepath.Join(root, DefaultValuesFile)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(valuesBody), 0o644); err != nil {
			t.Fatalf("write values: %v", err)
		}
	}
	for rel, body := range adrFiles {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write adr: %v", err)
		}
	}
	return root
}

const warnValues = "clusterImagePolicy:\n  mode: warn\n  registries:\n    - foo\n"
const enforceValues = "clusterImagePolicy:\n  mode: enforce\n  registries:\n    - foo\n"

func TestModeWarnPasses(t *testing.T) {
	root := setup(t, warnValues, nil)
	o := Check(Config{Root: root})
	if !o.Pass || o.Mode != "warn" {
		t.Errorf("warn should pass, got %+v", o)
	}
}

func TestModeEnforceWithoutSignOffFails(t *testing.T) {
	root := setup(t, enforceValues, map[string]string{
		"docs/decisions.md": "# Decisions\n\nSome other decision.\n",
	})
	o := Check(Config{Root: root})
	if o.Pass {
		t.Errorf("enforce without sign-off must fail, got %+v", o)
	}
}

func TestModeEnforceWithoutADecisionsDocFails(t *testing.T) {
	root := setup(t, enforceValues, nil) // no docs/decisions.md at all
	o := Check(Config{Root: root})
	if o.Pass {
		t.Error("enforce with no sign-off dirs must fail")
	}
}

func TestQuotedModeStripped(t *testing.T) {
	root := setup(t, "clusterImagePolicy:\n  mode: \"warn\"\n", nil)
	o := Check(Config{Root: root})
	if !o.Pass || o.Mode != "warn" {
		t.Errorf("quoted warn should strip + pass, got %+v", o)
	}
}

func TestModeOutsideBlockIgnored(t *testing.T) {
	// A `mode:` under a DIFFERENT top-level key must not be read.
	root := setup(t, "clusterImagePolicy:\n  registries:\n    - foo\nother:\n  mode: enforce\n", nil)
	o := Check(Config{Root: root})
	if !o.Pass || o.Mode != "" {
		t.Errorf("mode outside the clusterImagePolicy block must be ignored, got %+v", o)
	}
}

func TestModeFieldAbsentPasses(t *testing.T) {
	root := setup(t, "clusterImagePolicy:\n  registries:\n    - foo\n", nil)
	o := Check(Config{Root: root})
	if !o.Pass || o.Mode != "" {
		t.Errorf("absent mode should pass, got %+v", o)
	}
}

// The values file is the gate's subject; when it cannot be read the mode was
// never checked, and that is not a pass.
func TestMissingValuesFileFails(t *testing.T) {
	root := setup(t, "<absent>", nil)
	if Check(Config{Root: root}).Pass {
		t.Error("an unreadable values.yaml must not pass")
	}
}

// A sign-off written under the gitignored .claude tree reaches nobody who
// clones, so the gate no longer accepts one there.
func TestSignOffUnderGitignoredKitTreeIsNotAccepted(t *testing.T) {
	root := setup(t, enforceValues, map[string]string{
		".claude/knowledge-base/decisions/0050-deny.md": "Decision: flip find_infra_008 cutover.\n",
	})
	if Check(Config{Root: root}).Pass {
		t.Error("a sign-off under .claude/ must not satisfy the gate")
	}
}

func TestUnknownModeFails(t *testing.T) {
	root := setup(t, "clusterImagePolicy:\n  mode: audit\n", nil)
	o := Check(Config{Root: root})
	if o.Pass || o.Mode != "audit" {
		t.Errorf("unknown mode must fail, got %+v", o)
	}
}
