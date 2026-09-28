// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package archive provides safe tar extraction with security validations.
// Extracted from source/safe_tar.go to be importable by github/ and source/.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Extraction safety limits.
const (
	maxDecompressedSize int64 = 5 * 1024 * 1024 * 1024 // 5GB
	maxFileCount              = 100_000
	maxSingleFileSize   int64 = 1 * 1024 * 1024 * 1024 // 1GB
)

// Errors returned by SafeExtractTarGz.
var (
	ErrPathTraversal      = errors.New("tar entry path traversal detected")
	ErrSymlinkInArchive   = errors.New("tar entry contains symlink or hardlink")
	ErrDecompressionLimit = errors.New("tar archive exceeds decompression size limit")
	ErrFileCountLimit     = errors.New("tar archive exceeds file count limit")
)

// ExtractOpts configures extraction behavior.
type ExtractOpts struct {
	// StripComponents removes N leading path components from each entry.
	// Equivalent to tar --strip-components=N. GitHub tarballs use 1
	// to remove the "owner-repo-sha/" prefix.
	StripComponents int
}

// SafeExtractTarGz extracts a gzip-compressed tar archive into targetDir
// with strict security validation:
//   - No symlinks or hardlinks
//   - No path traversal
//   - Decompressed size limit (5GB)
//   - File count limit (100k)
//   - Single file size limit (1GB)
//
// Returns the number of files extracted.
func SafeExtractTarGz(archivePath, targetDir string, opts *ExtractOpts) (int, error) {
	f, err := os.Open(archivePath) //nolint:gosec // G304 expected: archivePath is caller-validated trusted tmp path
	if err != nil {
		return 0, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	return SafeExtractTarGzReader(f, targetDir, opts)
}

// SafeExtractTarGzReader extracts from an io.Reader instead of a file path.
func SafeExtractTarGzReader(r io.Reader, targetDir string, opts *ExtractOpts) (int, error) {
	return extractTarGz(r, targetDir, opts, defaultLimits)
}

// extractTarGz is SafeExtractTarGzReader with the limits injected, so tests
// can exercise every limit without producing gigabytes.
func extractTarGz(r io.Reader, targetDir string, opts *ExtractOpts, limits extractionLimits) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return 0, fmt.Errorf("resolve target dir: %w", err)
	}
	stripN := 0
	if opts != nil {
		stripN = opts.StripComponents
	}

	tr := tar.NewReader(gz)
	budget := extractionBudget{limits: limits}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return budget.files, nil
		}
		if err != nil {
			return budget.files, fmt.Errorf("read tar header: %w", err)
		}
		if err := extractEntry(tr, hdr, absTarget, stripN, &budget); err != nil {
			return budget.files, err
		}
	}
}

// extractEntry validates one entry and materialises it when it is a
// directory or a regular file; every other type is skipped.
func extractEntry(tr io.Reader, hdr *tar.Header, absTarget string, stripN int, budget *extractionBudget) error {
	// Reject symlinks/hardlinks unconditionally.
	if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
		return fmt.Errorf("%w: %s", ErrSymlinkInArchive, hdr.Name)
	}
	name := hdr.Name
	if stripN > 0 {
		if name = stripComponents(name, stripN); name == "" {
			return nil
		}
	}
	destPath, rel, err := resolveEntryPath(absTarget, name, hdr.Name)
	if err != nil {
		return err
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(destPath, 0o750); err != nil {
			return fmt.Errorf("mkdir %s: %w", rel, err)
		}
	case tar.TypeReg:
		if err := budget.admit(rel, hdr.Size); err != nil {
			return err
		}
		return writeRegularFile(tr, destPath, rel, hdr)
	}
	return nil
}

// resolveEntryPath maps an entry name to a path inside absTarget, rejecting
// absolute names and any name that resolves outside the target.
func resolveEntryPath(absTarget, name, original string) (dest, rel string, err error) {
	rel = filepath.Clean(name)
	if filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("%w: absolute path %s", ErrPathTraversal, original)
	}
	if strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+"..") {
		return "", "", fmt.Errorf("%w: %s", ErrPathTraversal, original)
	}
	dest = filepath.Join(absTarget, rel)
	if dest != absTarget && !strings.HasPrefix(dest, absTarget+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: resolved path %s escapes %s", ErrPathTraversal, dest, absTarget)
	}
	return dest, rel, nil
}

// writeRegularFile copies exactly hdr.Size bytes of the entry to destPath.
// tar.Reader never yields more than the declared size, and a short entry
// (truncated archive) fails the copy.
func writeRegularFile(tr io.Reader, destPath, rel string, hdr *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return fmt.Errorf("mkdir parent for %s: %w", rel, err)
	}
	outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o755) //nolint:gosec // G304 false positive: destPath validated by resolveEntryPath
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	_, copyErr := io.CopyN(outFile, tr, hdr.Size)
	closeErr := outFile.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", rel, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", rel, closeErr)
	}
	return nil
}

// extractionLimits bounds what one archive may expand to.
type extractionLimits struct {
	maxTotalBytes int64
	maxFiles      int
	maxFileBytes  int64
}

var defaultLimits = extractionLimits{
	maxTotalBytes: maxDecompressedSize,
	maxFiles:      maxFileCount,
	maxFileBytes:  maxSingleFileSize,
}

// extractionBudget tracks what an extraction has consumed against its limits.
type extractionBudget struct {
	limits     extractionLimits
	files      int
	totalBytes int64
}

// admit charges one regular file of the declared size to the budget, before
// any of its bytes are written.
func (b *extractionBudget) admit(rel string, size int64) error {
	b.files++
	if b.files > b.limits.maxFiles {
		return fmt.Errorf("%w: exceeded %d files", ErrFileCountLimit, b.limits.maxFiles)
	}
	if size > b.limits.maxFileBytes {
		return fmt.Errorf("%w: file %s is %d bytes (max %d)", ErrDecompressionLimit, rel, size, b.limits.maxFileBytes)
	}
	b.totalBytes += size
	if b.totalBytes > b.limits.maxTotalBytes {
		return fmt.Errorf("%w: total size exceeds %d bytes", ErrDecompressionLimit, b.limits.maxTotalBytes)
	}
	return nil
}

// stripComponents removes n leading path components from a tar entry name.
func stripComponents(name string, n int) string {
	for i := 0; i < n; i++ {
		idx := strings.IndexByte(name, '/')
		if idx < 0 {
			return ""
		}
		name = name[idx+1:]
	}
	return name
}
