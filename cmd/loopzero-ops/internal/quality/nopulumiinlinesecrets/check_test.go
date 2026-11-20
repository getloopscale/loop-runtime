// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package nopulumiinlinesecrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStack(t *testing.T, tmp, name, content string) {
	t.Helper()
	dir := filepath.Join(tmp, "infra/pulumi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_EmptyRoot_Errors(t *testing.T) {
	_, err := Check(Config{RepoRoot: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheck_NoPulumiTree_Pass(t *testing.T) {
	tmp := t.TempDir()
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS")
	}
}

func TestCheck_InlineSecretInStack_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeStack(t, tmp, "Pulumi.dev.yaml", `config:
  loopzero:db-url:
    secure: v1:AAAAAAA==:
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL for inline secret")
	}
	if len(out.Hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(out.Hits))
	}
}

func TestCheck_NonSecretYAMLEntry_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeStack(t, tmp, "Pulumi.dev.yaml", `config:
  loopzero:base-domain: dev.getloopscale.com
  loopzero:region: sfo3
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS, got hits: %+v", out.Hits)
	}
}

func TestCheck_OnlyPulumiFilesScanned(t *testing.T) {
	tmp := t.TempDir()
	// non-Pulumi file with the marker — should be ignored.
	writeStack(t, tmp, "README.yaml", `secure: v1:foo`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS — non-Pulumi.* files ignored")
	}
}

func TestCheck_MultipleStacks(t *testing.T) {
	tmp := t.TempDir()
	writeStack(t, tmp, "Pulumi.dev.yaml", `config:
  k:
    secure: v1:AAA
`)
	writeStack(t, tmp, "Pulumi.prod.yaml", `config:
  k:
    secure: v1:BBB
`)
	out, err := Check(Config{RepoRoot: tmp})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out.Hits) != 2 {
		t.Errorf("expected 2 hits across stacks, got %d", len(out.Hits))
	}
}

func TestCheck_Allowlist(t *testing.T) {
	tmp := t.TempDir()
	writeStack(t, tmp, "Pulumi.legacy.yaml", `config:
  k:
    secure: v1:keepme
`)
	out, err := Check(Config{
		RepoRoot:       tmp,
		AllowlistPaths: []string{"infra/pulumi/Pulumi.legacy.yaml"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS, got: %+v", out.Hits)
	}
}

func TestFormatOutcome_Pass(t *testing.T) {
	if !strings.Contains(FormatOutcome(Outcome{Pass: true}), "PASS") {
		t.Error("expected PASS in output")
	}
}

func TestFormatOutcome_FailIncludesKey(t *testing.T) {
	s := FormatOutcome(Outcome{
		Pass: false,
		Hits: []Hit{{File: "f.yaml", Line: 5, Key: "db-url", Reason: "R", Suggestion: "S"}},
	})
	if !strings.Contains(s, "db-url") {
		t.Errorf("expected key in output: %s", s)
	}
}
