// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package noaesremaining

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeGo(t *testing.T, tmp, rel, content string) {
	t.Helper()
	full := filepath.Join(tmp, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_EmptyRoot_Errors(t *testing.T) {
	_, err := Check(Config{RepoRoot: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheck_PreEnforcement_AESPresent_WARNonly(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package db
import "crypto/aes"
func x() { _, _ = aes.NewCipher(nil) }
`)
	out, err := Check(Config{
		RepoRoot:    tmp,
		NowOverride: CutoverDay.Add(10 * 24 * time.Hour), // T+10d (pre-cutover)
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS in pre-enforcement window, got: hits=%d", len(out.Hits))
	}
	if out.Enforced {
		t.Error("expected Enforced=false in pre-T+60d window")
	}
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "WARN-only") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected WARN-only message in pre-enforcement, got: %v", out.Warnings)
	}
}

func TestCheck_PostEnforcement_AESPresent_FAIL(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package db
import "crypto/aes"
func x() { _, _ = aes.NewCipher(nil) }
`)
	out, err := Check(Config{
		RepoRoot:    tmp,
		NowOverride: CutoverDay.Add(EnforcementDelay + 24*time.Hour),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL in post-enforcement with AES present")
	}
	if !out.Enforced {
		t.Error("expected Enforced=true post-T+60d")
	}
}

func TestCheck_PostEnforcement_AESDeleted_PASS(t *testing.T) {
	tmp := t.TempDir()
	// No file written → secret_wrapper.go absent → desired post-T10.3 state.
	out, err := Check(Config{
		RepoRoot:    tmp,
		NowOverride: CutoverDay.Add(EnforcementDelay + 24*time.Hour),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS when AES code deleted (T10.3 satisfied)")
	}
}

func TestCheck_CommentedAES_Ignored(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package db
// import "crypto/aes" — removed in T10.3
/*
   aes.NewCipher used to live here.
*/
func x() {}
`)
	out, err := Check(Config{
		RepoRoot:    tmp,
		NowOverride: CutoverDay.Add(EnforcementDelay + 24*time.Hour),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS — comments/block comments should be ignored, hits: %+v", out.Hits)
	}
}

func TestCheck_Allowlist(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `import "crypto/aes"`)
	out, err := Check(Config{
		RepoRoot:       tmp,
		NowOverride:    CutoverDay.Add(EnforcementDelay + 24*time.Hour),
		AllowlistPaths: []string{"api/internal/db/store/secret_wrapper.go"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for allowlisted path")
	}
}

func TestFormatOutcome_PreEnforcementMessage(t *testing.T) {
	out := Outcome{
		Pass:            true,
		Enforced:        false,
		EnforcementTime: CutoverDay.Add(EnforcementDelay),
		Now:             CutoverDay,
		Warnings:        []string{"countdown"},
	}
	s := FormatOutcome(out)
	if !strings.Contains(s, "WARN-only") {
		t.Errorf("expected WARN-only label, got: %s", s)
	}
}
