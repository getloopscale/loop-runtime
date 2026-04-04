// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

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
	// maxDecompressedSize is the maximum total bytes extracted (5GB).
	maxDecompressedSize int64 = 5 * 1024 * 1024 * 1024
	// maxFileCount is the maximum number of files in the archive.
	maxFileCount = 100_000
	// maxSingleFileSize is the maximum size of a single extracted file (1GB).
	maxSingleFileSize int64 = 1 * 1024 * 1024 * 1024
)

// ErrPathTraversal indicates a tar entry attempted to escape the target directory.
var ErrPathTraversal = errors.New("tar entry path traversal detected")

// ErrSymlinkInArchive indicates a tar entry contains a symbolic or hard link.
var ErrSymlinkInArchive = errors.New("tar entry contains symlink or hardlink")

// ErrDecompressionLimit indicates the archive exceeds decompression size limits.
var ErrDecompressionLimit = errors.New("tar archive exceeds decompression size limit")

// ErrFileCountLimit indicates the archive contains too many files.
var ErrFileCountLimit = errors.New("tar archive exceeds file count limit")

// SafeExtractTarGz extracts a gzip-compressed tar archive into targetDir
// with strict security validation on every entry:
//   - No symlinks or hardlinks (prevents credential theft via symlink-to-SA-token)
//   - No path traversal (no ".." components, no absolute paths)
//   - Decompressed size limit (prevents zip bombs)
//   - File count limit (prevents inode exhaustion)
//
// Returns the number of files extracted.
func SafeExtractTarGz(archivePath, targetDir string) (int, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return 0, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var totalBytes int64
	fileCount := 0

	// Resolve targetDir to absolute path for containment check.
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

		// SEC: Reject symlinks and hardlinks unconditionally.
		// A symlink to /var/run/secrets/kubernetes.io/serviceaccount/token
		// followed by git-add would exfiltrate cluster credentials.
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink {
			return fileCount, fmt.Errorf("%w: %s", ErrSymlinkInArchive, hdr.Name)
		}

		// SEC: Validate path — no absolute paths, no traversal.
		cleanName := filepath.Clean(hdr.Name)
		if filepath.IsAbs(cleanName) {
			return fileCount, fmt.Errorf("%w: absolute path %s", ErrPathTraversal, hdr.Name)
		}
		if strings.HasPrefix(cleanName, "..") || strings.Contains(cleanName, string(filepath.Separator)+"..") {
			return fileCount, fmt.Errorf("%w: %s", ErrPathTraversal, hdr.Name)
		}

		destPath := filepath.Join(absTarget, cleanName)

		// SEC: Double-check that the resolved path is within targetDir.
		// This catches edge cases that Clean might not handle.
		if !strings.HasPrefix(destPath, absTarget+string(filepath.Separator)) && destPath != absTarget {
			return fileCount, fmt.Errorf("%w: resolved path %s escapes %s", ErrPathTraversal, destPath, absTarget)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fileCount, fmt.Errorf("mkdir %s: %w", cleanName, err)
			}

		case tar.TypeReg:
			// SEC: Check file count limit.
			fileCount++
			if fileCount > maxFileCount {
				return fileCount, fmt.Errorf("%w: exceeded %d files", ErrFileCountLimit, maxFileCount)
			}

			// SEC: Check single file size before extraction.
			if hdr.Size > maxSingleFileSize {
				return fileCount, fmt.Errorf("%w: file %s is %d bytes (max %d)",
					ErrDecompressionLimit, cleanName, hdr.Size, maxSingleFileSize)
			}

			// SEC: Check total decompressed size.
			totalBytes += hdr.Size
			if totalBytes > maxDecompressedSize {
				return fileCount, fmt.Errorf("%w: total size exceeds %d bytes",
					ErrDecompressionLimit, maxDecompressedSize)
			}

			// Ensure parent directory exists.
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return fileCount, fmt.Errorf("mkdir parent for %s: %w", cleanName, err)
			}

			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0755)
			if err != nil {
				return fileCount, fmt.Errorf("create %s: %w", cleanName, err)
			}

			// SEC: Use io.LimitedReader to enforce the declared size.
			// This prevents a crafted archive where hdr.Size lies and the
			// actual stream is much larger.
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
			// Skip other types (block devices, char devices, FIFOs, etc.)
			// These have no legitimate use in source code archives.
			continue
		}
	}

	return fileCount, nil
}
