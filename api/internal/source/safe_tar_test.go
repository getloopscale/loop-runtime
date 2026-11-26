// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package source

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// mustMkdir creates a directory in tests, failing on error.
func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// mustWriteFile writes a file in tests, failing on error.
func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// createTestTarGz creates a tar.gz archive at archivePath with the given entries.
func createTestTarGz(t *testing.T, archivePath string, entries []testTarEntry) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer func() { _ = f.Close() }()

	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()

	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.Name,
			Mode:     0644,
			Size:     int64(len(e.Content)),
			Typeflag: e.Typeflag,
		}
		if e.Typeflag == tar.TypeSymlink {
			hdr.Linkname = e.Linkname
			hdr.Size = 0
		}
		if e.Typeflag == tar.TypeLink {
			hdr.Linkname = e.Linkname
			hdr.Size = 0
		}
		if e.Typeflag == tar.TypeDir {
			hdr.Size = 0
			if hdr.Mode == 0644 {
				hdr.Mode = 0755
			}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", e.Name, err)
		}
		if e.Typeflag == tar.TypeReg && len(e.Content) > 0 {
			if _, err := tw.Write([]byte(e.Content)); err != nil {
				t.Fatalf("write content %s: %v", e.Name, err)
			}
		}
	}
}

type testTarEntry struct {
	Name     string
	Typeflag byte
	Content  string
	Linkname string
}

func TestSafeExtractTarGz_ValidArchive(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "valid.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "src/", Typeflag: tar.TypeDir},
		{Name: "src/main.go", Typeflag: tar.TypeReg, Content: "package main"},
		{Name: "README.md", Typeflag: tar.TypeReg, Content: "# Hello"},
	})

	count, err := SafeExtractTarGz(archivePath, targetDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 files, got %d", count)
	}

	// Verify files exist with correct content
	data, err := os.ReadFile(filepath.Join(targetDir, "src", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(data) != "package main" {
		t.Errorf("unexpected content: %s", data)
	}

	data, err = os.ReadFile(filepath.Join(targetDir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(data) != "# Hello" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestSafeExtractTarGz_RejectsPathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "traversal.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "../../etc/cron.d/backdoor", Typeflag: tar.TypeReg, Content: "pwned"},
	})

	_, err := SafeExtractTarGz(archivePath, targetDir)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}

	// Verify the file was NOT created
	if _, err := os.Stat(filepath.Join(tmpDir, "etc", "cron.d", "backdoor")); !os.IsNotExist(err) {
		t.Error("path traversal file should not exist")
	}
}

func TestSafeExtractTarGz_RejectsAbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "absolute.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "/etc/passwd", Typeflag: tar.TypeReg, Content: "root:x:0:0"},
	})

	_, err := SafeExtractTarGz(archivePath, targetDir)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestSafeExtractTarGz_RejectsSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "symlink.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "token", Typeflag: tar.TypeSymlink, Linkname: "/var/run/secrets/kubernetes.io/serviceaccount/token"},
	})

	_, err := SafeExtractTarGz(archivePath, targetDir)
	if err == nil {
		t.Fatal("expected symlink error, got nil")
	}
	if !errors.Is(err, ErrSymlinkInArchive) {
		t.Errorf("expected ErrSymlinkInArchive, got: %v", err)
	}
}

func TestSafeExtractTarGz_RejectsHardlink(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "hardlink.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "hardlink-to-secrets", Typeflag: tar.TypeLink, Linkname: "/etc/shadow"},
	})

	_, err := SafeExtractTarGz(archivePath, targetDir)
	if err == nil {
		t.Fatal("expected hardlink error, got nil")
	}
	if !errors.Is(err, ErrSymlinkInArchive) {
		t.Errorf("expected ErrSymlinkInArchive, got: %v", err)
	}
}

func TestSafeExtractTarGz_RejectsEmbeddedTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "embedded.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	// Sneaky traversal: valid-looking prefix then ..
	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "src/../../etc/hosts", Typeflag: tar.TypeReg, Content: "127.0.0.1 evil"},
	})

	_, err := SafeExtractTarGz(archivePath, targetDir)
	if err == nil {
		t.Fatal("expected path traversal error, got nil")
	}
	if !errors.Is(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestSafeExtractTarGz_EmptyArchive(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "empty.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	createTestTarGz(t, archivePath, nil)

	count, err := SafeExtractTarGz(archivePath, targetDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 files, got %d", count)
	}
}

func TestComputeDirHash_Deterministic(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteFile(t, filepath.Join(tmpDir, "a.txt"), []byte("hello"))
	mustWriteFile(t, filepath.Join(tmpDir, "b.txt"), []byte("world"))

	h1, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hashes differ: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex, got %d", len(h1))
	}
}

func TestComputeDirHash_SkipsGitDir(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteFile(t, filepath.Join(tmpDir, "app.js"), []byte("code"))
	hashWithout, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Add .git directory — should not change hash
	mustMkdir(t, filepath.Join(tmpDir, ".git", "objects"))
	mustWriteFile(t, filepath.Join(tmpDir, ".git", "HEAD"), []byte("ref: refs/heads/main"))
	hashWith, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if hashWithout != hashWith {
		t.Errorf(".git directory changed hash: %s vs %s", hashWithout, hashWith)
	}
}

func TestComputeDirHash_IncludesGithubDir(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteFile(t, filepath.Join(tmpDir, "app.js"), []byte("code"))
	hashWithout, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Add .github directory — MUST be included in hash (F-03 bug fix)
	mustMkdir(t, filepath.Join(tmpDir, ".github", "workflows"))
	mustWriteFile(t, filepath.Join(tmpDir, ".github", "workflows", "ci.yml"), []byte("name: CI"))
	hashWith, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if hashWithout == hashWith {
		t.Error(".github directory should change hash but was excluded")
	}
}

func TestComputeDirHash_IncludesGitignore(t *testing.T) {
	tmpDir := t.TempDir()
	mustWriteFile(t, filepath.Join(tmpDir, "app.js"), []byte("code"))
	hashWithout, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Add .gitignore — MUST be included in hash (F-03 bug fix)
	mustWriteFile(t, filepath.Join(tmpDir, ".gitignore"), []byte("node_modules/"))
	hashWith, err := computeDirHash(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if hashWithout == hashWith {
		t.Error(".gitignore should change hash but was excluded")
	}
}

func TestSafeExtractTarGz_SkipsSpecialTypes(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "special.tar.gz")
	targetDir := filepath.Join(tmpDir, "out")
	mustMkdir(t, targetDir)

	// FIFO and char device entries should be silently skipped
	createTestTarGz(t, archivePath, []testTarEntry{
		{Name: "normal.txt", Typeflag: tar.TypeReg, Content: "ok"},
		{Name: "fifo", Typeflag: tar.TypeFifo},
		{Name: "chardev", Typeflag: tar.TypeChar},
	})

	count, err := SafeExtractTarGz(archivePath, targetDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 file (skipping fifo+chardev), got %d", count)
	}
}
