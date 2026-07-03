// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package prodimagedigests implements the T2.3 gate: detect platform
// manifests that use :tag references instead of immutable image@sha256:<digest>.
//
// T2.3 of docs/plans/completed/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md.
//
// Why: a tag like :v1.2.3 is mutable in DOCR. Attacker with push access
// can re-tag the same name to point at a malicious image; next pod
// restart (HPA, eviction, OOM kill) fetches the new content. Even if
// the image is signed (Fulcio mints cert for ANY identity), digest
// immutability is a separate, layered defense.
//
// Mode: WARN — this gate reports violations but Outcome.Pass is true
// when zero violations OR in warn-mode (default). Sponsor flips
// EnforceMode=true post-7d-soak via the EnforceMode field to make
// violations fail the gate.
//
// Scope: prod-published Helm charts (charts/loopzero-*/) that ship to
// platform namespaces. Tenant chart templates are out of scope
// (user apps may use :tag for dev convenience; production deploy
// pipeline emits digest via build pipeline).
package prodimagedigests

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultScanPaths are scanned when Config.ScanPaths is empty.
// Limited to published Helm charts in charts/loopzero-* — these are the
// artifacts that ship to production via ArgoCD. infra/helm/ bootstrap
// charts run inside Helmfile (operator-driven) so are evaluated by
// loopzero-common.image helper at template time, not by this gate.
var DefaultScanPaths = []string{
	"charts/loopzero-api/templates",
	"charts/loopzero-cloud/templates",
	"charts/loopzero-app-operator/templates",
	"charts/loopzero-platform-operator/templates",
	"charts/loopzero-preview-operator/templates",
	"charts/loopzero-tenant-operator/templates",
	"charts/loopzero-dashboard/templates",
}

// Config drives the gate.
type Config struct {
	// Root is prepended to relative scan paths (default "." via caller).
	Root string
	// ScanPaths overrides DefaultScanPaths when non-empty.
	ScanPaths []string
	// EnforceMode flips warn-mode (default) to enforce. When false,
	// violations are reported but Pass stays true (warn-mode soak).
	// Sponsor flips to true post-7d-soak per EC-5 of plan.
	EnforceMode bool
}

// Outcome captures the result.
type Outcome struct {
	// Pass is true when zero violations OR in warn-mode (regardless
	// of violation count).
	Pass         bool
	ScannedFiles int
	// Violations are file:lineno: <stripped line> entries for each
	// occurrence of an image reference that uses :tag without an
	// adjacent digest (or via the helper that prefers digest).
	Violations []string
	// EnforceMode reflects the config — useful for callers to render
	// the appropriate message ("warn" vs "violation").
	EnforceMode bool
}

var (
	// Matches a hardcoded `image: ...:tag` line — `image:` followed by
	// any chars (non-quote/non-#) ending in `:<word>`. Excludes lines
	// with `@sha256:` (already a digest) and lines with `loopzero-common.image`
	// include (helper handles digest at template time).
	hardcodedTagRe = regexp.MustCompile(`^\s*image:\s*["']?[^"'#@]+:[A-Za-z0-9._-]+\s*$`)
	// Matches `image: {{ include "loopzero-common.image" ... }}` — the
	// helper template, which handles digest preference itself. NOT
	// a violation regardless of digest field state — the violation
	// surfaces at the values.yaml level when digest is missing, but
	// at render-time the helper produces correct output.
	helperIncludeRe = regexp.MustCompile(`include\s+"loopzero-common\.image"`)
	leadWSRe        = regexp.MustCompile(`^\s*`)
)

// Check scans the configured paths for hardcoded :tag image references.
func Check(cfg Config) (*Outcome, error) {
	paths := cfg.ScanPaths
	if len(paths) == 0 {
		paths = DefaultScanPaths
	}
	out := &Outcome{Pass: true, EnforceMode: cfg.EnforceMode}

	for _, p := range paths {
		full := p
		if cfg.Root != "" {
			full = filepath.Join(cfg.Root, p)
		}
		info, err := os.Stat(full)
		if err != nil {
			continue // missing path silently skipped (chart may not exist in test env)
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

	// Warn-mode: report violations but stay Pass=true. Sponsor flips
	// EnforceMode=true to turn violations into fail.
	if cfg.EnforceMode && len(out.Violations) > 0 {
		out.Pass = false
	}
	return out, nil
}

func isYAML(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}

func scanFile(path string, out *Outcome) error {
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

		// Skip comment lines.
		stripped := leadWSRe.ReplaceAllString(line, "")
		if strings.HasPrefix(stripped, "#") {
			continue
		}

		// Helper include = digest preference handled at template time.
		// Not a violation regardless of values.yaml state.
		if helperIncludeRe.MatchString(line) {
			continue
		}

		// Hardcoded `image: foo/bar:v1.2.3` = violation.
		if hardcodedTagRe.MatchString(line) {
			out.Violations = append(out.Violations, fmt.Sprintf("%s:%d: %s", path, lineNum, stripped))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
