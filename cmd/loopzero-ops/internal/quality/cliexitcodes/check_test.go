// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cliexitcodes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_ContractCodes_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(0)\nprocess.exit(3)\nprocess.exit(5)\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("exit codes 0-5 must PASS: %+v", out)
	}
}

func TestCheck_OutOfContract_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.exit(6)\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass || len(out.Violations) != 1 {
		t.Fatalf("process.exit(6) must FAIL: %+v", out)
	}
	if !strings.Contains(out.Violations[0], "6") {
		t.Fatalf("violation must name the code: %v", out.Violations)
	}
}

func TestCheck_SignalHandler_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "process.on('SIGINT', () => {\n  cleanup()\n  process.exit(130)\n})\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("exit(130) inside a SIG handler must PASS: %+v", out)
	}
}

func TestCheck_SignalCodeOutsideHandler_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.ts", "function f() {\n  doStuff()\n  somethingElse()\n  process.exit(143)\n}\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Fatalf("exit(143) NOT in a SIG handler must FAIL: %+v", out)
	}
}

func TestCheck_TestFilesIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "cli/src/a.test.ts", "process.exit(99)\n")
	write(t, root, "cli/src/__tests__/b.ts", "process.exit(98)\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass {
		t.Fatalf("test files must be ignored: %+v", out)
	}
}

func TestCheck_NoCliSrc_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass || !out.Skipped {
		t.Fatalf("absent cli/src must PASS (skipped): %+v", out)
	}
}
