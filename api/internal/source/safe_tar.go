// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package source

import (
	"github.com/getloopscale/loop-runtime/api/internal/archive"
)

// Re-export error types for backward compatibility with existing tests.
var (
	ErrPathTraversal      = archive.ErrPathTraversal
	ErrSymlinkInArchive   = archive.ErrSymlinkInArchive
	ErrDecompressionLimit = archive.ErrDecompressionLimit
	ErrFileCountLimit     = archive.ErrFileCountLimit
)

// SafeExtractTarGz delegates to archive.SafeExtractTarGz with no strip components.
// This maintains backward compatibility for source/ callers.
func SafeExtractTarGz(archivePath, targetDir string) (int, error) {
	return archive.SafeExtractTarGz(archivePath, targetDir, nil)
}
