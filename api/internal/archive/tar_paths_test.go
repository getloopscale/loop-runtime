// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Basis-path coverage for the extractor: this is the security boundary for
// untrusted uploaded archives, so every branch that rejects or skips an entry
// is exercised on its own.

// rawTarGz writes headers (and, for regular files, content) verbatim, so a
// test can build entries tar.Writer's helpers would not.
func rawTarGz(t *testing.T, entries []rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.content))
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
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

type rawEntry struct {
	hdr     tar.Header
	content string
}

func reg(name, content string) rawEntry {
	return rawEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, content: content}
}

func extract(t *testing.T, data []byte, opts *ExtractOpts) (string, int, error) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	n, err := SafeExtractTarGzReader(bytes.NewReader(data), dest, opts)
	return dest, n, err
}

func TestExtractRejectsTraversingEntries(t *testing.T) {
	cases := []struct {
		name  string
		entry rawEntry
	}{
		{"absolute file path", reg("/etc/passwd", "x")},
		{"dot-dot in the middle", reg("a/../../escape.txt", "x")},
		{"leading dot-dot", reg("../escape.txt", "x")},
		{"directory escaping the target", rawEntry{hdr: tar.Header{Name: "../evil/", Typeflag: tar.TypeDir, Mode: 0o755}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := extract(t, rawTarGz(t, []rawEntry{tc.entry}), nil)
			if !errors.Is(err, ErrPathTraversal) {
				t.Fatalf("err = %v, want ErrPathTraversal", err)
			}
		})
	}
}

func TestExtractRejectsHardlinks(t *testing.T) {
	data := rawTarGz(t, []rawEntry{
		reg("a.txt", "x"),
		{hdr: tar.Header{Name: "b.txt", Typeflag: tar.TypeLink, Linkname: "a.txt"}},
	})

	_, _, err := extract(t, data, nil)

	if !errors.Is(err, ErrSymlinkInArchive) {
		t.Fatalf("err = %v, want ErrSymlinkInArchive", err)
	}
}

func TestExtractSkipsEntryTypesItDoesNotWrite(t *testing.T) {
	data := rawTarGz(t, []rawEntry{
		{hdr: tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o644}},
		reg("kept.txt", "ok"),
	})

	dest, n, err := extract(t, data, nil)

	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dest, "fifo")); !os.IsNotExist(err) {
		t.Errorf("fifo entry was materialised: %v", err)
	}
}

func TestExtractSkipsEntriesStrippedToNothing(t *testing.T) {
	data := rawTarGz(t, []rawEntry{reg("toplevel", "x"), reg("prefix/kept.txt", "ok")})

	dest, n, err := extract(t, data, &ExtractOpts{StripComponents: 1})

	if err != nil || n != 1 {
		t.Fatalf("extract = (%d, %v), want (1, nil)", n, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "kept.txt")); err != nil || string(b) != "ok" {
		t.Errorf("kept.txt = %q, %v", b, err)
	}
}

func TestExtractRejectsInputThatIsNotGzip(t *testing.T) {
	_, _, err := extract(t, []byte("plain text"), nil)
	if err == nil {
		t.Fatal("non-gzip input extracted without error")
	}
}

func TestExtractReportsATruncatedArchive(t *testing.T) {
	data := rawTarGz(t, []rawEntry{reg("big.txt", string(bytes.Repeat([]byte("a"), 4096)))})
	// Re-compress only the first half of the uncompressed tar stream.
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if _, err := plain.ReadFrom(zr); err != nil {
		t.Fatal(err)
	}
	var cut bytes.Buffer
	gz := gzip.NewWriter(&cut)
	_, _ = gz.Write(plain.Bytes()[:1024])
	_ = gz.Close()

	_, _, err = extract(t, cut.Bytes(), nil)

	if err == nil {
		t.Fatal("a truncated entry was reported as extracted")
	}
}

func TestExtractPreservesFileContentAndNestedDirectories(t *testing.T) {
	data := rawTarGz(t, []rawEntry{reg("a/b/c.txt", "deep")})

	dest, n, err := extract(t, data, nil)

	if err != nil || n != 1 {
		t.Fatalf("extract = (%d, %v)", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a", "b", "c.txt")); string(b) != "deep" {
		t.Errorf("content = %q", b)
	}
}

func TestExtractionBudgetEnforcesEachLimit(t *testing.T) {
	limits := extractionLimits{maxTotalBytes: 10, maxFiles: 2, maxFileBytes: 6}
	cases := []struct {
		name  string
		sizes []int64
		want  error
	}{
		{"within every limit", []int64{5, 5}, nil},
		{"one file over the per-file limit", []int64{7}, ErrDecompressionLimit},
		{"total over the aggregate limit", []int64{6, 6}, ErrDecompressionLimit},
		{"too many files", []int64{1, 1, 1}, ErrFileCountLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := extractionBudget{limits: limits}
			var err error
			for _, s := range tc.sizes {
				if err = b.admit("f", s); err != nil {
					break
				}
			}
			if !errors.Is(err, tc.want) && !(err == nil && tc.want == nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
