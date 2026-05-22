// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package prodimagetags implements the T1.16 gate: reject mutable :prod / :latest
// image tags in production manifests (ADR-0020 D1, INV-1).
//
// Plan T5.3 of docs/plans/2026-05-22-shell-script-strategic-plan.md v1.3:
// replaces infra/scripts/check-prod-image-tags.sh. Pure-Go (filepath walk +
// regexp) — replaces bash find/grep/sed. No external CLI.
//
// Production deploys MUST use immutable :sha7 (or fuller :sha) tags so the Git
// SHA always maps to one image. Exceptions: dev/staging-scoped files, comment
// lines, and lines tagged `# loopzero:mutable-tag-allowed`.
package prodimagetags

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultScanPaths are scanned when Config.ScanPaths is empty.
var DefaultScanPaths = []string{
	"infra/helmfile/environments/prod.yaml",
	"infra/helm",
	"charts",
}

// Config drives the gate.
type Config struct {
	// Root is prepended to relative scan paths (default "." via caller).
	Root string
	// ScanPaths overrides DefaultScanPaths when non-empty.
	ScanPaths []string
}

// Outcome captures the result.
type Outcome struct {
	Pass         bool
	ScannedFiles int
	Violations   []string // "file:lineno: <stripped line>"
}

var (
	// `(image|tag): [<quote>]<repo>:(prod|latest)<boundary>` — matches the
	// mutable-tag patterns; excludes inline comments via [^"'#]+.
	mutableTagRe = regexp.MustCompile(`(image|tag):\s*["']?[^"'#]+:(prod|latest)([^a-zA-Z0-9_]|$)`)
	leadWSRe     = regexp.MustCompile(`^\s*`)
	devFileRe    = regexp.MustCompile(`dev\.ya?ml$`)
	stagingRe    = regexp.MustCompile(`staging\.ya?ml$`)
)

// Check scans the configured paths for mutable prod image tags.
func Check(cfg Config) (*Outcome, error) {
	paths := cfg.ScanPaths
	if len(paths) == 0 {
		paths = DefaultScanPaths
	}
	out := &Outcome{Pass: true}

	for _, p := range paths {
		full := p
		if cfg.Root != "" {
			full = filepath.Join(cfg.Root, p)
		}
		info, err := os.Stat(full)
		if err != nil {
			continue // missing path is not an error (parity with bash `! -e`)
		}
		if info.IsDir() {
			err = filepath.WalkDir(full, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if d.IsDir() || !isYAML(path) {
					return nil
				}
				return scanFile(path, out)
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", full, err)
			}
		} else if isYAML(full) {
			if err := scanFile(full, out); err != nil {
				return nil, err
			}
		}
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out, nil
}

func isYAML(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}

func scanFile(path string, out *Outcome) error {
	// Skip dev/staging-scoped files (mutable tags allowed there).
	if devFileRe.MatchString(path) || stagingRe.MatchString(path) {
		out.ScannedFiles++
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	out.ScannedFiles++
	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if !mutableTagRe.MatchString(line) {
			continue
		}
		stripped := leadWSRe.ReplaceAllString(line, "")
		if strings.HasPrefix(stripped, "#") {
			continue // commented line
		}
		if strings.Contains(line, "loopzero:mutable-tag-allowed") {
			continue // explicit override
		}
		out.Violations = append(out.Violations, fmt.Sprintf("%s:%d: %s", path, lineNum, stripped))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
