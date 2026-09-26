// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package prodimagedigests implements the T2.3 gate: detect platform
// manifests that use :tag references instead of immutable image@sha256:<digest>.
//
// T2.3 of 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.
//
// Why: a tag like :v1.2.3 is mutable in DOCR. Attacker with push access
// can re-tag the same name to point at a malicious image; next pod
// restart (HPA, eviction, OOM kill) fetches the new content. Even if
// the image is signed (Fulcio mints cert for ANY identity), digest
// immutability is a separate, layered defense.
//
// Mode: PER KIND, not global. `KindLiteralTag` is enforced by default;
// `KindMissingDigest` warns until the image publisher emits the value. See
// kind.go and MEET-ASYNC-AMENDMENT-2026-08-17-digest-enforcement-has-two-populations
// for why one boolean over both populations was the defect: it left three
// fixable findings invisible inside a number dominated by seven that were not,
// for three months against a declared 7-day soak.
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

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
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
	// Enforce names the kinds whose violations make Pass false. A nil map
	// means DefaultEnforce() — nil is the common case (every caller that does
	// not care about the split), so it must mean the decided policy rather
	// than "enforce nothing", which is what the old zero value meant.
	Enforce map[Kind]bool
}

// Outcome captures the result.
type Outcome struct {
	// Pass is false when at least one violation of an ENFORCED kind exists.
	Pass         bool
	ScannedFiles int
	// Violations are every finding, of every kind. Use Enforced/Warned to
	// separate what blocks from what is being reported.
	Violations []Violation
	// Enforce is the policy this run applied — callers render "FAIL" vs
	// "WARN" per finding from it, instead of from one global mode.
	Enforce map[Kind]bool
}

// Verdict is Fail when a violation of an enforced kind exists; findings of a
// kind the policy only reports leave it a Pass.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// Enforced lists the findings that made (or would make) the gate fail.
func (o *Outcome) Enforced() []Violation { return o.byEnforcement(true) }

// Warned lists the findings reported without blocking.
func (o *Outcome) Warned() []Violation { return o.byEnforcement(false) }

func (o *Outcome) byEnforcement(enforced bool) []Violation {
	var out []Violation
	for _, v := range o.Violations {
		if o.Enforce[v.Kind] == enforced {
			out = append(out, v)
		}
	}
	return out
}

var (
	// Matches a hardcoded `image: ...:tag` line — `image:` followed by
	// any chars (non-quote/non-#) ending in `:<word>`. Excludes lines
	// with `@sha256:` (already a digest) and lines with `loopzero-common.image`
	// include (helper handles digest at template time).
	hardcodedTagRe = regexp.MustCompile(`^\s*image:\s*["']?[^"'#@]+:[A-Za-z0-9._-]+\s*$`)
	// Matches `image: {{ include "loopzero-common.image" ... }}` — the helper
	// template, which handles digest preference itself. NOT a violation at the
	// template layer: the helper prefers `.Values.image.digest`.
	//
	// The violation surfaces at the values.yaml level when digest is missing —
	// and for three months that sentence lived here describing a check nobody had
	// written. `scanChartValues` is that check now (B-113).
	//
	// The companion claim this comment used to make — "at render-time the helper
	// produces correct output" — is true only GIVEN a digest. Without one the
	// helper produces a tag, correctly, and the output is mutable. Measured
	// 2026-08-16: all seven published charts used the helper, none declared a
	// digest, all declared `tag: dev`.
	helperIncludeRe = regexp.MustCompile(`include\s+"loopzero-common\.image"`)
	leadWSRe        = regexp.MustCompile(`^\s*`)
)

// Check scans the configured paths for hardcoded :tag image references.
func Check(cfg Config) (*Outcome, error) {
	paths := cfg.ScanPaths
	if len(paths) == 0 {
		paths = DefaultScanPaths
	}
	enforce := cfg.Enforce
	if enforce == nil {
		enforce = DefaultEnforce()
	}
	out := &Outcome{Pass: true, Enforce: enforce}

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

	// B-113: the values layer. The template scan above deliberately clears every
	// chart that defers to `loopzero-common.image`, so without this the gate reports
	// PASS on a chart whose effective default is a mutable tag — which is what it
	// did, on all seven published charts, for three months.
	if err := scanChartValues(filepath.Join(cfg.Root, "charts"), out); err != nil {
		return nil, err
	}

	// The verdict reads the policy per kind. A warned finding is still
	// reported — it just does not decide the verdict.
	if len(out.Enforced()) > 0 {
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
			out.Violations = append(out.Violations, Violation{
				Kind: KindLiteralTag, File: path, Line: lineNum, Text: stripped,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", path, err)
	}
	return nil
}
