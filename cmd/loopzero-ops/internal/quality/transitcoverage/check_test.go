// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package transitcoverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFixture writes content to api/internal/db/store/secret_wrapper.go under a tmp root.
func writeFixture(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	full := filepath.Join(root, secretWrapperPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const cleanSource = `package db
var vaultPrefix = []byte("vault:v1:")

func (s *PostgresSecretStore) encrypt(p []byte) ([]byte, error) { return p, nil }
func (s *PostgresSecretStore) decrypt(p []byte) ([]byte, error) { return p, nil }

func (s *PostgresSecretStore) encryptForStore(_ context.Context, _ string, p []byte) ([]byte, error) {
	return s.encrypt(p)
}
func (s *PostgresSecretStore) decryptFromStore(_ context.Context, _ string, p []byte) ([]byte, error) {
	return s.decrypt(p)
}

func (s *PostgresSecretStore) Set(_ context.Context, _ string, _ []byte) error {
	_, err := s.encryptForStore(nil, "", nil)
	return err
}
func (s *PostgresSecretStore) ListForResync(_ context.Context) error {
	_, err := s.decryptFromStore(nil, "", nil)
	return err
}
`

func TestCheck_CleanSourcePasses(t *testing.T) {
	root := writeFixture(t, cleanSource)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Pass {
		t.Errorf("expected PASS, got FAIL: %s", out.FormatReport())
	}
}

func TestCheck_MissingVaultPrefixFails(t *testing.T) {
	src := strings.Replace(cleanSource,
		`var vaultPrefix = []byte("vault:v1:")`,
		`var vaultPrefix = []byte("v1:")`, // wrong prefix
		1)
	root := writeFixture(t, src)
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL for wrong prefix")
	}
	var found bool
	for _, v := range out.Violations {
		if strings.Contains(v, "vaultPrefix") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected vaultPrefix violation, got: %v", out.Violations)
	}
}

func TestCheck_RawEncryptInSetFails(t *testing.T) {
	// Set bypasses dispatcher
	src := strings.Replace(cleanSource,
		`_, err := s.encryptForStore(nil, "", nil)`,
		`_, err := s.encrypt(nil)`,
		1)
	root := writeFixture(t, src)
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL for raw s.encrypt in Set, got PASS")
	}
	var found bool
	for _, v := range out.Violations {
		if strings.Contains(v, "Set() calls s.encrypt() directly") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Set bypass violation, got: %v", out.Violations)
	}
}

func TestCheck_RawDecryptInReadPathFails(t *testing.T) {
	src := strings.Replace(cleanSource,
		`_, err := s.decryptFromStore(nil, "", nil)`,
		`_, err := s.decrypt(nil)`,
		1)
	root := writeFixture(t, src)
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL for raw s.decrypt in ListForResync")
	}
}

func TestCheck_NoDispatcherUseFails(t *testing.T) {
	// Both Set + ListForResync inlined raw calls
	src := strings.Replace(cleanSource,
		`_, err := s.encryptForStore(nil, "", nil)`,
		`return nil`,
		1)
	src = strings.Replace(src,
		`_, err := s.decryptFromStore(nil, "", nil)`,
		`return nil`,
		1)
	root := writeFixture(t, src)
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL when no dispatcher uses found")
	}
}

func TestCheck_MissingFileFails(t *testing.T) {
	root := t.TempDir()
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass {
		t.Errorf("expected FAIL when target file missing")
	}
}

func TestFormatReport_PASSPrintsMetrics(t *testing.T) {
	root := writeFixture(t, cleanSource)
	out, _ := Check(Config{RepoRoot: root})
	rep := out.FormatReport()
	if !strings.Contains(rep, "PASS") {
		t.Errorf("expected PASS in report")
	}
	if !strings.Contains(rep, "dispatcher_encrypt_uses") {
		t.Errorf("expected findings metrics in report")
	}
}

func TestFormatReport_FailIncludesViolations(t *testing.T) {
	src := strings.Replace(cleanSource,
		`_, err := s.encryptForStore(nil, "", nil)`,
		`_, err := s.encrypt(nil)`,
		1)
	root := writeFixture(t, src)
	out, _ := Check(Config{RepoRoot: root})
	rep := out.FormatReport()
	if !strings.Contains(rep, "FAIL") {
		t.Errorf("expected FAIL in report")
	}
	if !strings.Contains(rep, "T3.5") {
		t.Errorf("expected T3.5 marker in violation message, got: %s", rep)
	}
}

// A brace inside a string literal must not end the method early: the raw call
// after it still belongs to Set.
func TestCheck_BraceInStringLiteralDoesNotHideARawCall(t *testing.T) {
	src := strings.Replace(cleanSource,
		`_, err := s.encryptForStore(nil, "", nil)`,
		"_ = \"}\"\n\t_, _ = s.encryptForStore(nil, \"\", nil)\n\t_, err := s.encrypt(nil)",
		1)
	root := writeFixture(t, src)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if out.Pass {
		t.Fatalf("a raw s.encrypt after a brace in a string literal passed: %s", out.FormatReport())
	}
}

// Moving the bypass one file sideways must not evade the gate: every non-test
// file of the store package is read.
func TestCheck_RawCallInAnotherStoreFileFails(t *testing.T) {
	root := writeFixture(t, cleanSource)
	other := filepath.Join(root, filepath.Dir(secretWrapperPath), "secret_bulk.go")
	src := "package db\nfunc (s *PostgresSecretStore) BulkSet() { _, _ = s.encrypt(nil) }\n"
	if err := os.WriteFile(other, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if out.Pass {
		t.Fatalf("a raw s.encrypt in a sibling store file passed: %s", out.FormatReport())
	}
	if !strings.Contains(out.FormatReport(), "secret_bulk.go") {
		t.Errorf("the violation does not name the file it is in: %s", out.FormatReport())
	}
}

// Test files may call the primitives directly; they are not a write path.
func TestCheck_TestFilesAreNotScanned(t *testing.T) {
	root := writeFixture(t, cleanSource)
	other := filepath.Join(root, filepath.Dir(secretWrapperPath), "secret_wrapper_test.go")
	src := "package db\nfunc (s *PostgresSecretStore) helper() { _, _ = s.encrypt(nil) }\n"
	if err := os.WriteFile(other, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Pass {
		t.Fatalf("a test file was treated as a write path: %s", out.FormatReport())
	}
}

func TestCheck_UnparseableStoreFileIsAnError(t *testing.T) {
	root := writeFixture(t, cleanSource)
	other := filepath.Join(root, filepath.Dir(secretWrapperPath), "broken.go")
	if err := os.WriteFile(other, []byte("this is not Go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("an unparseable store file was accepted")
	}
}

func TestFormatReport_IsDeterministic(t *testing.T) {
	root := writeFixture(t, cleanSource)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	first := out.FormatReport()
	for i := 0; i < 20; i++ {
		if got := out.FormatReport(); got != first {
			t.Fatalf("report order changed between calls:\n%s\n---\n%s", first, got)
		}
	}
}
