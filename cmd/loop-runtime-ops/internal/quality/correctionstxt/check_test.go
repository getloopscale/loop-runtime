// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package correctionstxt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCorr(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "CORRECTIONS.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

const goodHeader = "# CORRECTIONS war journal\n# see finding-2026-05-13-corrections-txt-audit\n\n"

func TestCheck_AllItemsAnnotated_Pass(t *testing.T) {
	p := writeCorr(t, goodHeader+
		"### Item A\nsome text\n**STATUS 2026-05-13:** closed\n\n"+
		"### Item B\nmore text\n**STATUS 2026-05-14:** closed\n")
	out, err := Check(p)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass {
		t.Fatalf("all items annotated must PASS: %+v", out)
	}
}

func TestCheck_ItemMissingStatus_Fails(t *testing.T) {
	p := writeCorr(t, goodHeader+
		"### Item A\n**STATUS 2026-05-13:** closed\n\n"+
		"### Item B\nno status here\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("item without STATUS must FAIL: %+v", out)
	}
	if !strings.Contains(strings.Join(out.Violations, "\n"), "Item B") {
		t.Fatalf("violation must name Item B: %v", out.Violations)
	}
}

func TestCheck_HeaderMissingFindingRef_Fails(t *testing.T) {
	p := writeCorr(t, "# CORRECTIONS war journal\n\n"+
		"### Item A\n**STATUS 2026-05-13:** closed\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("header without the audit-finding reference must FAIL: %+v", out)
	}
}

func TestCheck_DefinitiveFixWithoutStatus_Fails(t *testing.T) {
	p := writeCorr(t, goodHeader+
		"### Item A\nDefinitive fix: rewrite the thing\nstill open\n")
	out, _ := Check(p)
	if out.Pass {
		t.Fatalf("a 'Definitive fix' block without STATUS must FAIL: %+v", out)
	}
}

func TestCheck_DefinitiveFixWithStatus_Pass(t *testing.T) {
	p := writeCorr(t, goodHeader+
		"### Item A\nDefinitive fix: rewrite the thing\n**STATUS 2026-05-13:** done\n")
	out, _ := Check(p)
	if !out.Pass {
		t.Fatalf("Definitive fix WITH STATUS must PASS: %+v", out)
	}
}

func TestCheck_FileMissing_Errors(t *testing.T) {
	_, err := Check(filepath.Join(t.TempDir(), "nope.txt"))
	if err == nil {
		t.Fatal("missing CORRECTIONS.txt must error")
	}
}
