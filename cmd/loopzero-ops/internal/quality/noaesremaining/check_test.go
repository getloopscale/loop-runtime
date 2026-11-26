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

// seedNonAPIRoots writes one innocuous non-test file into every scanRoot other
// than api/, and returns the paths it wrote.
//
// It exists because the zero-found floor is per-root (B-195): a fixture that
// builds only an api/ tree is a tree with two empty roots, and the gate is
// supposed to say so. Without this, every test below that means to assert
// something about MARKER scanning would instead be asserting the floor --
// silently, and in the FAIL-expecting tests, vacuously: they would go on
// passing with the AES fixture deleted. Seeding keeps each test's subject its
// own. The returned paths let callers keep exact counts and, for the allowlist
// test, excuse the seeds along with the file it is really about.
//
// The loop reads scanRoots rather than hardcoding names, so widening the roots
// again cannot leave these fixtures quietly one root short.
// assertAllMarkerHits pins a FAIL to the file that is supposed to have caused
// it: every hit is an AES-marker hit on that file, and there is at least one.
//
// The count itself is deliberately not asserted -- a fixture that both imports
// crypto/aes and calls aes.NewCipher legitimately produces two hits, and an
// earlier draft of these tests failed precisely by demanding one. What has to
// hold is attribution: with the per-root floor in play, "out.Pass == false" on
// its own would also be satisfied by an empty root, so a FAIL-expecting test
// that checks nothing else can pass with its own fixture deleted.
func assertAllMarkerHits(t *testing.T, out Outcome, wantFileSuffix string) {
	t.Helper()
	if len(out.Hits) == 0 {
		t.Fatalf("expected at least one hit on %s, got none", wantFileSuffix)
	}
	for _, h := range out.Hits {
		if !strings.HasSuffix(h.File, wantFileSuffix) {
			t.Errorf("hit outside %s: %+v", wantFileSuffix, h)
		}
		if !strings.Contains(h.Reason, "AES code path remains") {
			t.Errorf("hit on %s is not an AES-marker finding: %+v", wantFileSuffix, h)
		}
	}
}

func seedNonAPIRoots(t *testing.T, tmp string) []string {
	t.Helper()
	var written []string
	for _, root := range scanRoots {
		if root == "api" {
			continue
		}
		rel := root + "/seed/seed.go"
		writeGo(t, tmp, rel, "package seed\n")
		written = append(written, rel)
	}
	return written
}

func TestCheck_EmptyRoot_Errors(t *testing.T) {
	_, err := Check(Config{RepoRoot: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheck_PreEnforcement_AESPresent_WARNonly(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
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
	seedNonAPIRoots(t, tmp)
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
	// The FAIL must be attributable to the AES marker, not to a floor hit for
	// some empty root. Without this the test would keep passing if the fixture
	// above were deleted.
	assertAllMarkerHits(t, out, "secret_wrapper.go")
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
	seeded := seedNonAPIRoots(t, tmp)
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
	if want := 2 + len(seeded); out.FilesScanned != want {
		t.Errorf("expected both non-test files scanned plus %d seeds = %d, got %d",
			len(seeded), want, out.FilesScanned)
	}
}

// Zero candidate files means the scan read nothing, and a scan that read
// nothing cannot certify anything. Before B-162 this was the gate's cleanest
// PASS.
//
// B-195 made the floor per-root, so an empty tree now yields one violation per
// declared root rather than a single aggregate one.
func TestCheck_PostEnforcement_NothingScanned_FAILS(t *testing.T) {
	tmp := t.TempDir() // no scan root present at all

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
	if len(out.Hits) != len(scanRoots) {
		t.Fatalf("expected one zero-scan violation per root (%d), got %d: %+v",
			len(scanRoots), len(out.Hits), out.Hits)
	}
	for _, root := range scanRoots {
		var seen bool
		for _, h := range out.Hits {
			if h.File == root && strings.Contains(h.Reason, "scanned 0 Go files") {
				seen = true
			}
		}
		if !seen {
			t.Errorf("no zero-scan violation named root %q: %+v", root, out.Hits)
		}
	}
}

// The floor has to be PER root, not summed over them. An api/ tree that is
// populated while pkg/ has moved away keeps the total above zero: an aggregate
// floor stays quiet, the gate reports PASS, and it silently covers less than
// the scope it advertises -- which is the defect B-195 was opened to fix. This
// is the test that stops the fix from reintroducing it.
func TestCheck_PostEnforcement_OneRootEmpty_FAILS(t *testing.T) {
	tmp := t.TempDir()
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package store
func encryptForStore() string { return "vault:v1:ct" }
`)
	writeGo(t, tmp, "operators/api/v1/types.go", "package v1\n")
	// pkg/ deliberately absent.

	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("a scan whose pkg/ root read nothing reported PASS; a partly-missing tree is not a clean one")
	}
	if len(out.Hits) != 1 || out.Hits[0].File != "pkg" {
		t.Errorf("expected exactly one violation, naming pkg, got %+v", out.Hits)
	}
	if out.FilesFound != 2 {
		t.Errorf("expected the two populated roots still counted, got %d", out.FilesFound)
	}
}

// The B-195 oracle, in the form the audit measured it: a complete AES-256-GCM
// implementation -- write path included -- moved out of api/ entirely, into the
// pkg/vault package that the api binary links. `go list -deps` puts pkg/vault in
// the binary's closure, so this is a live code path by every definition the ADR
// uses; the only thing the old scan root had to say about it was PASS.
func TestCheck_PostEnforcement_AESRelocatedToPkgVault_FAILS(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `package store

import vaultpkg "github.com/getloopscale/loop-runtime/pkg/vault"

func (s *PostgresSecretStore) decrypt(b, aad []byte) ([]byte, error) {
	return vaultpkg.LegacyAESOpen(s.encryptionKey, b, aad)
}
`)
	writeGo(t, tmp, "pkg/vault/legacy_aes.go", `package vault

import (
	"crypto/aes"
	"crypto/cipher"
)

func LegacyAESSeal(key, plaintext, aad []byte) ([]byte, error) {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	return gcm.Seal(nil, make([]byte, gcm.NonceSize()), plaintext, aad), nil
}
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("AES moved one directory over, into a package the api binary links, passed the gate")
	}
	for _, h := range out.Hits {
		if !strings.HasSuffix(h.File, "legacy_aes.go") {
			t.Errorf("unexpected hit outside the relocated file: %+v", h)
		}
	}
}

// The third root is not decoration: operators/api/v1 is in the api binary's
// dependency closure too.
func TestCheck_PostEnforcement_AESUnderOperatorsAPI_FAILS(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", "package store\n")
	writeGo(t, tmp, "operators/api/v1/crypto.go", `package v1
import "crypto/aes"
func x(k []byte) { _, _ = aes.NewCipher(k) }
`)
	out, err := Check(Config{RepoRoot: tmp, NowOverride: enforcedNow()})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out.Pass {
		t.Fatal("expected FAIL for an AES import under operators/api")
	}
	assertAllMarkerHits(t, out, "operators/api/v1/crypto.go")
}

// The evasion the T10.3 ADR named and rejected on judgment: leave the store
// file clean and move the AES implementation one file sideways. The gate
// scanned a single hardcoded path, so this passed.
func TestCheck_PostEnforcement_AESRelocatedToSiblingFile_FAILS(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
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
	seedNonAPIRoots(t, tmp)
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
	assertAllMarkerHits(t, out, "api/internal/routes/secrets/crypto.go")
}

// Test files are excluded on purpose: they never compile into a running
// binary, and the legacy AES fixtures are what B-162 DoD bullet 3 leans on —
// the read path must stay provable while pre-T10.3 rows can still exist.
func TestCheck_TestFilesAreNotScanned(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
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
	if want := 1 + len(scanRoots) - 1; out.FilesScanned != want {
		t.Errorf("expected only the non-test file (plus one seed per non-api root) scanned, got %d want %d",
			out.FilesScanned, want)
	}
}

func TestCheck_CommentedAES_Ignored(t *testing.T) {
	tmp := t.TempDir()
	seedNonAPIRoots(t, tmp)
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
	seeds := seedNonAPIRoots(t, tmp)
	writeGo(t, tmp, "api/internal/db/store/secret_wrapper.go", `import "crypto/aes"`)
	// The seeds are excused along with the file this test is about, so the tree
	// stays what the test needs it to be: every candidate found, none read.
	out, err := Check(Config{
		RepoRoot:       tmp,
		NowOverride:    enforcedNow(),
		AllowlistPaths: append([]string{"api/internal/db/store/secret_wrapper.go"}, seeds...),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !out.Pass {
		t.Errorf("expected PASS for allowlisted path")
	}
	// An allowlisted file was still FOUND: the zero-scan floor must not fire
	// just because the operator excused the only file in the tree.
	if want := 1 + len(seeds); out.FilesFound != want || out.FilesScanned != 0 {
		t.Errorf("expected found=%d scanned=0, got found=%d scanned=%d",
			want, out.FilesFound, out.FilesScanned)
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
