// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package prodimagetags implements the T1.16 gate: reject mutable :prod / :latest
// image tags in production manifests (ADR-0020 D1, INV-1).
//
// Plan T5.3 of 2026-05-22-shell-script-strategic-plan v1.3:
// replaces infra/scripts/check-prod-image-tags.sh. Pure-Go (filepath walk +
// regexp) — replaces bash find/grep/sed. No external CLI.
//
// Production deploys MUST use immutable :sha7 (or fuller :sha) tags so the Git
// SHA always maps to one image. Exceptions: dev/staging-scoped files, comment
// lines, and lines tagged `# loop-runtime:mutable-tag-allowed`.
package prodimagetags

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

// DefaultScanPaths are scanned when Config.ScanPaths is empty.
var DefaultScanPaths = []string{
	"infra/helmfile/environments/prod.yaml",
	"infra/helmfile/values/prod", // #232 — prod overlays (the actual deployed values)
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

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

var (
	// `(image|tag): [<quote>]<repo>:(prod|latest)<boundary>` — matches the
	// mutable-tag patterns; excludes inline comments via [^"'#]+.
	mutableTagRe = regexp.MustCompile(`(image|tag):\s*["']?[^"'#]+:(prod|latest)([^a-zA-Z0-9_]|$)`)
	// #232 — split-form (bare) mutable tag, e.g. `tag: stable`. Anchored to
	// end-of-value so date-stamped/semver/sha/templated tags do NOT match
	// (`tag: latest-20260420`, `tag: v1.2.3`, `tag: {{ .Values.x }}` all PASS).
	// Applied ONLY to prod-overlay files (see isProdOverlay) — bootstrap
	// defaults under infra/helm/ legitimately carry `tag: latest` and are
	// overridden at deploy time, so they stay out of scope (MEET-2026-07-12-001 C2).
	splitMutableTagRe = regexp.MustCompile(`^\s*tag:\s*["']?(prod|latest|stable|develop)["']?\s*(#.*)?$`)
	// #232 — inline colon-form `image: repo:stable` / `:develop`, overlay-scoped
	// (the existing mutableTagRe only covers :prod/:latest). Boundary excludes
	// `-`/`.` so `repo:stable-v2` / `repo:stable.1` are pins that PASS; `foo-stable:v1`
	// PASS (stable is in the repo name, before the colon).
	overlayInlineMutableRe = regexp.MustCompile(`(image|tag):\s*["']?[^"'#]+:(stable|develop)([^a-zA-Z0-9_.-]|$)`)
	leadWSRe               = regexp.MustCompile(`^\s*`)
	devFileRe              = regexp.MustCompile(`dev\.ya?ml$`)
	stagingRe              = regexp.MustCompile(`staging\.ya?ml$`)
	// #232 — prod overlays live here; only these get the split-form check and
	// have their `.gotmpl` templates scanned.
	prodOverlayRe = regexp.MustCompile(`helmfile/values/prod/`)
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
				if d.IsDir() || !isScannable(path) {
					return nil
				}
				return scanFile(path, out)
			})
			if err != nil {
				return nil, fmt.Errorf("walk %s: %w", full, err)
			}
		} else if isScannable(full) {
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

// isProdOverlay reports whether path is a prod overlay under
// infra/helmfile/values/prod/ — the only scope for split-form detection and
// the only place `.gotmpl` templates are scanned (#232, MEET-2026-07-12-001 C2).
func isProdOverlay(path string) bool {
	return prodOverlayRe.MatchString(filepath.ToSlash(path))
}

// isScannable reports whether a file should be read. YAML everywhere; templated
// (.gotmpl) files ONLY when they are prod overlays — so infra/helm/**/*.gotmpl
// keep their prior (unscanned) behavior.
func isScannable(path string) bool {
	if isYAML(path) {
		return true
	}
	return isProdOverlay(path) && strings.HasSuffix(path, ".gotmpl")
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
	overlay := isProdOverlay(path) // #232 — split-form check applies here only
	lineNum := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		matched := mutableTagRe.MatchString(line)
		if !matched && overlay {
			// #232 — prod overlays: bare `tag: stable` OR inline `image: repo:stable`.
			matched = splitMutableTagRe.MatchString(line) || overlayInlineMutableRe.MatchString(line)
		}
		if !matched {
			continue
		}
		stripped := leadWSRe.ReplaceAllString(line, "")
		if strings.HasPrefix(stripped, "#") {
			continue // commented line
		}
		if strings.Contains(line, "loop-runtime:mutable-tag-allowed") {
			continue // explicit override
		}
		out.Violations = append(out.Violations, fmt.Sprintf("%s:%d: %s", path, lineNum, stripped))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
