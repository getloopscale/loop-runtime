// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package agplisolation implements the T9.1 gate: fail CI if an AGPL-3.0 Go
// package is imported outside its ADR-approved allow-list location.
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check_agpl_isolation.sh. Pure-Go (filepath walk +
// substring scan) — replaces bash grep. No external CLI.
//
// Per ADR-0048 the only acceptable AGPL import is minio/madmin-go in the
// operator binary (operators/internal/minio/provisioner.go) — AGPL must not
// leak into api/cli/cloud/pkg (Apache-2.0 + shared-library boundaries).
package agplisolation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// importPrefix is prepended to a pattern to form the literal Go import string
// searched for. Kept separate from the default pattern below so this source
// file never contains the prefix and the AGPL package name contiguously
// (which would self-match when pkg/ is scanned).
const importPrefix = `"github.com/`

// Exception is an ADR-approved AGPL import: Import path allowed only in Path.
type Exception struct {
	Import string // e.g. "minio/madmin-go"
	Path   string // repo-relative file, e.g. "operators/internal/minio/provisioner.go"
}

// DefaultPatterns are the known AGPL package suffixes (after github.com/).
var DefaultPatterns = []string{"minio/madmin-go"}

// DefaultAllowlist are the ADR-approved exceptions (ADR-0048).
var DefaultAllowlist = []Exception{
	{Import: "minio/madmin-go", Path: "operators/internal/minio/provisioner.go"},
}

// DefaultScanDirs are the boundaries scanned for AGPL leakage.
var DefaultScanDirs = []string{"api", "cli", "cloud", "pkg", "operators"}

// Config drives the gate.
type Config struct {
	Root      string
	Patterns  []string    // default DefaultPatterns
	Allowlist []Exception // default DefaultAllowlist
	ScanDirs  []string    // default DefaultScanDirs
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Violations []string // "<rel> imports <pattern>"
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Check scans the configured boundaries for unallowlisted AGPL imports.
func Check(cfg Config) *Outcome {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	patterns := cfg.Patterns
	if patterns == nil {
		patterns = DefaultPatterns
	}
	allowlist := cfg.Allowlist
	if allowlist == nil {
		allowlist = DefaultAllowlist
	}
	scanDirs := cfg.ScanDirs
	if scanDirs == nil {
		scanDirs = DefaultScanDirs
	}

	out := &Outcome{Pass: true}
	for _, dir := range scanDirs {
		base := filepath.Join(root, dir)
		if _, statErr := os.Stat(base); os.IsNotExist(statErr) {
			continue // scan dir legitimately absent
		}
		// M25: fail-CLOSED — a walk/read error means the gate could not read files
		// it must scan; recorded as a violation (Pass=false) below, not swallowed.
		walkErr := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return fmt.Errorf("%s: %w", path, rerr)
			}
			content := string(data)
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, pattern := range patterns {
				if !strings.Contains(content, importPrefix+pattern) {
					continue
				}
				if !allowed(rel, pattern, allowlist) {
					out.Violations = append(out.Violations, rel+" imports "+pattern)
				}
			}
			return nil
		})
		if walkErr != nil {
			out.Pass = false
			out.Violations = append(out.Violations,
				fmt.Sprintf("scan error under %s: %v (fail-closed)", dir, walkErr))
		}
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out
}

func allowed(rel, pattern string, allowlist []Exception) bool {
	for _, e := range allowlist {
		if e.Path == rel && e.Import == pattern {
			return true
		}
	}
	return false
}
