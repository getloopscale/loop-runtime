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

// enforcedNow is any instant after the T+60d gate opens.
func enforcedNow() time.Time { return CutoverDay.Add(EnforcementDelay + 24*time.Hour) }

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
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
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

// The store file is present and carries no AES marker: T10.3 done, as the
// gate is meant to observe it.
//
// This test used to assert PASS for an EMPTY tmp dir — no api/ tree, no
// store file, nothing — under the comment "No file written → secret_wrapper.go
// absent → desired post-T10.3 state". That is the defect the gate had, pinned
// as an expectation: absent and clean were the same answer, so a mistyped
// `-root` reported the tree healthy. The state T10.3 actually produces is a
// store that still exists and no longer reaches for AES, which is what this
// writes. Zero-found now has its own test below, and it asserts the opposite.
func TestCheck_PostEnforcement_StorePresentWithoutAES_PASS(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package store
func encryptForStore() string { return "vault:v1:ct" }
`)
	writeGo(t, tmp, "api/internal/server/server.go", `package server
func Run() {}
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS when the store carries no AES marker, hits: %+v", out.Hits)
	}
	if out.FilesScanned != 2 {
		t.Errorf("expected both non-test files scanned, got %d", out.FilesScanned)
	}
}

// Zero candidate files means the scan read nothing, and a scan that read
// nothing cannot certify anything. Before B-162 this was the gate's cleanest
// PASS.
func TestCheck_PostEnforcement_NothingScanned_FAILS(t *testing.T) {
	tmp := t.TempDir() // no api/ tree at all

	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("a scan that found 0 files reported PASS; an empty tree is indistinguishable from a wrong -root")
	}
	if out.FilesFound != 0 {
		t.Errorf("expected FilesFound=0, got %d", out.FilesFound)
	}
	if len(out.Hits) != 1 || !strings.Contains(out.Hits[0].Reason, "scanned 0 Go files") {
		t.Errorf("expected one zero-scan violation, got %+v", out.Hits)
	}
}

// The evasion the T10.3 ADR named and rejected on judgment: leave the store
// file clean and move the AES implementation one file sideways. The gate
// scanned a single hardcoded path, so this passed.
func TestCheck_PostEnforcement_AESRelocatedToSiblingFile_FAILS(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package store
func encryptForStore() string { return "vault:v1:ct" }
`)
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper_aes.go", `package store

import (
	"crypto/aes"
	"crypto/cipher"
)

func legacy(k []byte) {
	b, _ := aes.NewCipher(k)
	g, _ := cipher.NewGCM(b)
	_ = g
}
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("AES moved to a sibling file passed the gate; relocating the code must not defeat it")
	}
	for _, h := range out.Hits {
		if !strings.HasSuffix(h.File, "secret_wrapper_aes.go") {
			t.Errorf("unexpected hit outside the relocated file: %+v", h)
		}
	}
}

// AES anywhere else under api/ counts too — the invariant is about the code
// that compiles into the running binary, not about one directory.
func TestCheck_PostEnforcement_AESElsewhereUnderAPI_FAILS(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", "package store\n")
	writeGo(t, tmp, "api/internal/routes/secrets/crypto.go", `package secrets
import "crypto/aes"
func x(k []byte) { _, _ = aes.NewCipher(k) }
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL for an AES import elsewhere under api/")
	}
}

// Test files are excluded on purpose: they never compile into a running
// binary, and the legacy AES fixtures are what B-162 DoD bullet 3 leans on —
// the read path must stay provable while pre-T10.3 rows can still exist.
func TestCheck_TestFilesAreNotScanned(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", "package store\n")
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper_legacy_aes_fixture_test.go", `package store
import "crypto/aes"
func fixture(k []byte) { _, _ = aes.NewCipher(k) }
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("a legacy-AES test fixture must not fail the gate, hits: %+v", out.Hits)
	}
	if out.FilesScanned != 1 {
		t.Errorf("expected only the non-test file scanned, got %d", out.FilesScanned)
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
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
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
		NowOverride:    enforcedNow(),
		AllowlistPaths: []string{"api/internal/db/store/secret_wrapper.go"},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for allowlisted path")
	}
	// An allowlisted file was still FOUND: the zero-scan floor must not fire
	// just because the operator excused the only file in the tree.
	if out.FilesFound != 1 || out.FilesScanned != 0 {
		t.Errorf("expected found=1 scanned=0, got found=%d scanned=%d", out.FilesFound, out.FilesScanned)
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

// The scan size travels with the verdict. A reader who cannot see how much
// was read cannot tell a clean tree from an unread one.
func TestFormatOutcome_ReportsHowMuchWasScanned(t *testing.T) {
	s := FormatOutcome(Outcome{Pass: true, Enforced: true, FilesFound: 374, FilesScanned: 374})
	if !strings.Contains(s, "374 of 374") {
		t.Errorf("expected the scanned/found counts in the output, got: %s", s)
	}
}
