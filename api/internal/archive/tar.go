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
	ErrPathTraversal    = errors.New("tar entry path traversal detected")
	ErrSymlinkInArchive = errors.New("tar entry contains symlink or hardlink")
	ErrDecompressionLimit = errors.New("tar archive exceeds decompression size limit")
	ErrFileCountLimit   = errors.New("tar archive exceeds file count limit")
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
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	return SafeExtractTarGzReader(f, targetDir, opts)
}

// SafeExtractTarGzReader extracts from an io.Reader instead of a file path.
func SafeExtractTarGzReader(r io.Reader, targetDir string, opts *ExtractOpts) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var totalBytes int64
	fileCount := 0
	stripN := 0
	if opts != nil {
		stripN = opts.StripComponents
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return 0, fmt.Errorf("resolve target dir: %w", err)
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fileCount, fmt.Errorf("read tar header: %w", err)
		}

		// Reject symlinks/hardlinks unconditionally.
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fileCount, fmt.Errorf("%w: %s", ErrSymlinkInArchive, hdr.Name)
		}

		// Strip leading path components.
		name := hdr.Name
		if stripN > 0 {
			name = stripComponents(name, stripN)
			if name == "" {
				continue
			}
		}

		// Validate path.
		cleanName := filepath.Clean(name)
		if filepath.IsAbs(cleanName) {
			return fileCount, fmt.Errorf("%w: absolute path %s", ErrPathTraversal, hdr.Name)
		}
		if strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, string(filepath.Separator)+"..") {
			return fileCount, fmt.Errorf("%w: %s", ErrPathTraversal, hdr.Name)
		}

		destPath := filepath.Join(absTarget, cleanName)
		if !strings.HasPrefix(destPath, absTarget+string(filepath.Separator)) && destPath != absTarget {
			return fileCount, fmt.Errorf("%w: resolved path %s escapes %s", ErrPathTraversal, destPath, absTarget)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o750); err != nil {
				return fileCount, fmt.Errorf("mkdir %s: %w", cleanName, err)
			}
		case tar.TypeReg:
			fileCount++
			if fileCount > maxFileCount {
				return fileCount, fmt.Errorf("%w: exceeded %d files", ErrFileCountLimit, maxFileCount)
			}
			if hdr.Size > maxSingleFileSize {
				return fileCount, fmt.Errorf("%w: file %s is %d bytes (max %d)",
					ErrDecompressionLimit, cleanName, hdr.Size, maxSingleFileSize)
			}
			totalBytes += hdr.Size
			if totalBytes > maxDecompressedSize {
				return fileCount, fmt.Errorf("%w: total size exceeds %d bytes",
					ErrDecompressionLimit, maxDecompressedSize)
			}
			if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
				return fileCount, fmt.Errorf("mkdir parent for %s: %w", cleanName, err)
			}
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0755)
			if err != nil {
				return fileCount, fmt.Errorf("create %s: %w", cleanName, err)
			}
			written, err := io.Copy(outFile, io.LimitReader(tr, hdr.Size+1))
			closeErr := outFile.Close()
			if err != nil {
				return fileCount, fmt.Errorf("write %s: %w", cleanName, err)
			}
			if closeErr != nil {
				return fileCount, fmt.Errorf("close %s: %w", cleanName, closeErr)
			}
			if written > hdr.Size {
				return fileCount, fmt.Errorf("%w: file %s actual size exceeds declared size",
					ErrDecompressionLimit, cleanName)
			}
		default:
			continue
		}
	}

	return fileCount, nil
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
