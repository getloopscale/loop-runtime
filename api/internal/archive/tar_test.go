// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
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

// T3.4 of plan 2026-06-11-code-review-remediation v1.2 — zip-bomb /
// decompression-limit coverage. Closes #127 CRITICAL from
// /loop-code-review: SafeExtractTarGz had ZERO tests exercising the
// 3 decompression caps that defend against malicious archives:
//   - maxSingleFileSize  (1 GB per entry)
//   - maxDecompressedSize (5 GB total)
//   - maxFileCount        (100,000 entries)
//
// Even partial coverage of these caps catches regressions where a
// future refactor removes or weakens the cap branch.

// makeTarGzWithOversizedHeader crafts a tar.gz where the FIRST entry's
// declared Size is `declaredSize` while actually writing `actualSize`
// bytes of zero content. With actualSize=1 we keep the test cheap
// (~1 KB) while still letting tar.Reader return a valid header with
// the oversized declaration. SafeExtractTarGzReader's cap check runs
// BEFORE the io.Copy of content, so the test trips ErrDecompressionLimit
// without us writing the full declared size.
//
// Implementation note: archive/tar.Writer enforces Size==written bytes
// before the next WriteHeader. We satisfy that constraint by writing
// exactly `actualSize` bytes and then closing — the resulting tar is
// structurally valid for the entry the test inspects (the cap check
// trips on hdr.Size BEFORE the io.Copy consumes content, so the
// extractor never reaches the EOF mismatch).
func makeTarGzWithOversizedHeader(t *testing.T, declaredSize int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	// First entry declares an oversized hdr.Size; we write `declaredSize`
	// bytes of zeros via io.CopyN so the Writer's accounting accepts the
	// entry. With gzip streaming, the on-disk size stays small thanks to
	// the high compressibility of zero bytes (~1 MB for a 1 GB tar).
	hdr := &tar.Header{
		Name:     "bomb.bin",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     declaredSize,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	// Stream zero bytes — gzip compresses to negligible payload.
	zr := &constReader{b: 0}
	if _, err := io.CopyN(tw, zr, declaredSize); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// constReader emits an infinite stream of one byte value. Used to feed
// a streaming tar.Writer cheaply (high compression ratio under gzip).
type constReader struct{ b byte }

func (c *constReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = c.b
	}
	return len(p), nil
}

// TestSafeExtractTarGz_RejectsOversizedSingleFile — trips the
// maxSingleFileSize cap (1 GB per entry). The cap check runs BEFORE
// the io.Copy of content (tar.go:129-132), so a header declaring
// > maxSingleFileSize MUST surface ErrDecompressionLimit without
// writing any extracted bytes.
//
// We declare a Size of maxSingleFileSize+1 and stream the same number
// of zero bytes via gzip compression — the on-disk tar.gz stays small
// (~1 MB) because zeros compress extremely well.
func TestSafeExtractTarGz_RejectsOversizedSingleFile(t *testing.T) {
	if testing.Short() {
		t.Skip("zip-bomb test streams ~1 GB of zeros; skipped in -short mode")
	}
	data := makeTarGzWithOversizedHeader(t, maxSingleFileSize+1)

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, data)
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	_, err := SafeExtractTarGz(archivePath, destDir, nil)
	if err == nil {
		t.Fatal("expected ErrDecompressionLimit for oversized single file, got nil")
	}
	if !isErrType(err, ErrDecompressionLimit) {
		t.Errorf("expected ErrDecompressionLimit, got: %v", err)
	}
	// Defensive: extractor MUST NOT have written the oversized file.
	if _, statErr := os.Stat(filepath.Join(destDir, "bomb.bin")); statErr == nil {
		t.Error("oversized file was extracted despite cap trip — regression in tar.go:129-132")
	}
}

// TestSafeExtractTarGz_RejectsFileCountLimit — trips the maxFileCount
// cap (100,000 entries). We write maxFileCount+1 zero-byte files so
// each entry contributes only the tar header (~512 bytes) — total tar
// uncompressed is ~50 MB, gzip-compressed to ~5 MB (similar headers
// dedupe well). Run time ~2-3 seconds; not in -short mode.
func TestSafeExtractTarGz_RejectsFileCountLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("file-count test writes 100k+ tar headers; skipped in -short mode")
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// maxFileCount+1 zero-byte regular files
	for i := 0; i <= maxFileCount; i++ {
		hdr := &tar.Header{
			Name:     "file" + itoa(i),
			Typeflag: tar.TypeReg,
			Mode:     0644,
			Size:     0,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	tmpDir := t.TempDir()
	archivePath := writeTarGzFile(t, tmpDir, buf.Bytes())
	destDir := filepath.Join(tmpDir, "out")
	_ = os.MkdirAll(destDir, 0755)

	_, err := SafeExtractTarGz(archivePath, destDir, nil)
	if err == nil {
		t.Fatal("expected ErrFileCountLimit, got nil")
	}
	if !isErrType(err, ErrFileCountLimit) {
		t.Errorf("expected ErrFileCountLimit, got: %v", err)
	}
}

// itoa is a tiny stdlib-free int-to-string helper to keep this test
// file's import surface small (the encoding pattern doesn't matter,
// just uniqueness of file names within the loop).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
