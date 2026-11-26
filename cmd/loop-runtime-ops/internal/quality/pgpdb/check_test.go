// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package pgpdb

import (
	"os"
	"path/filepath"
	"testing"
)

func writeValues(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "values.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestCheck_CreateFalse_Pass(t *testing.T) {
	p := writeValues(t, "primary:\n  pdb:\n    create: false\n")
	out, err := Check(p)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("primary.pdb.create=false must PASS: %+v", out)
	}
}

func TestCheck_CreateTrue_Fails(t *testing.T) {
	p := writeValues(t, "primary:\n  pdb:\n    create: true\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("primary.pdb.create=true must FAIL (B2 regression): %+v", out)
	}
}

func TestCheck_PdbMissing_Fails(t *testing.T) {
	p := writeValues(t, "primary:\n  resources: {}\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("primary.pdb missing must FAIL: %+v", out)
	}
}

func TestCheck_PdbPresentCreateAbsent_Fails(t *testing.T) {
	p := writeValues(t, "primary:\n  pdb:\n    minAvailable: 1\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("pdb present but create absent must FAIL (not explicitly false): %+v", out)
	}
}

func TestCheck_PrimaryMissing_Fails(t *testing.T) {
	p := writeValues(t, "image:\n  tag: latest\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("primary missing → pdb missing → must FAIL: %+v", out)
	}
}

func TestCheck_FileMissing_Errors(t *testing.T) {
	_, err := Check(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil {
		t.Fatal("missing values.yaml must error")
	}
}
