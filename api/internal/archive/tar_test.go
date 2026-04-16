// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz creates a tar.gz in memory with the given entries.
func makeTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		switch {
		case e.isSymlink:
			hdr := &tar.Header{Name: e.name, Typeflag: tar.TypeSymlink, Linkname: e.linkTarget}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		case e.isDir:
			hdr := &tar.Header{Name: e.name, Typeflag: tar.TypeDir, Mode: 0755}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		default:
			hdr := &tar.Header{Name: e.name, Typeflag: tar.TypeReg, Size: int64(len(e.content)), Mode: 0644}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type tarEntry struct {
	name       string
	content    string
	isDir      bool
	isSymlink  bool
	linkTarget string
}

func writeTarGzFile(t *testing.T, dir string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, "test.tar.gz")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSafeExtractTarGz_BasicExtraction(t *testing.T) {
	data := makeTarGz(t, []tarEntry{
		{name: "hello.txt", content: "world"},
		{name: "subdir/", isDir: true},
		{name: "subdir/nested.txt", content: "nested"},
	})

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, data)
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	count, err := SafeExtractTarGz(archivePath, destDir, nil)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 files, got %d", count)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "hello.txt"))
	if err != nil || string(content) != "world" {
		t.Errorf("hello.txt content mismatch: %s", content)
	}
}

func TestSafeExtractTarGz_SymlinkRejected(t *testing.T) {
	data := makeTarGz(t, []tarEntry{
		{name: "evil", isSymlink: true, linkTarget: "/etc/passwd"},
	})

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, data)
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	_, err := SafeExtractTarGz(archivePath, destDir, nil)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !isErrType(err, ErrSymlinkInArchive) {
		t.Errorf("expected ErrSymlinkInArchive, got: %v", err)
	}
}

func TestSafeExtractTarGz_PathTraversalRejected(t *testing.T) {
	data := makeTarGz(t, []tarEntry{
		{name: "../../../etc/passwd", content: "pwned"},
	})

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, data)
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	_, err := SafeExtractTarGz(archivePath, destDir, nil)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !isErrType(err, ErrPathTraversal) {
		t.Errorf("expected ErrPathTraversal, got: %v", err)
	}
}

func TestSafeExtractTarGz_StripComponents(t *testing.T) {
	// Simulate GitHub tarball with owner-repo-sha/ prefix
	data := makeTarGz(t, []tarEntry{
		{name: "owner-repo-abc123/", isDir: true},
		{name: "owner-repo-abc123/package.json", content: `{"name":"test"}`},
		{name: "owner-repo-abc123/src/", isDir: true},
		{name: "owner-repo-abc123/src/index.js", content: "console.log('hi')"},
	})

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, data)
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	count, err := SafeExtractTarGz(archivePath, destDir, &ExtractOpts{StripComponents: 1})
	if err != nil {
		t.Fatalf("extract with strip failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 files, got %d", count)
	}

	// Files should be at root, not under owner-repo-abc123/
	if _, err := os.Stat(filepath.Join(destDir, "package.json")); err != nil {
		t.Error("package.json should be at root after strip")
	}
	if _, err := os.Stat(filepath.Join(destDir, "src", "index.js")); err != nil {
		t.Error("src/index.js should exist after strip")
	}
}

func isErrType(err, target error) bool {
	return err != nil && (err == target || (len(target.Error()) > 0 && bytes.Contains([]byte(err.Error()), []byte(target.Error()))))
}
