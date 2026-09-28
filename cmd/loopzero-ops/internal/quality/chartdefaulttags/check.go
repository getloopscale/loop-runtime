// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package chartdefaulttags closes the blind spot behind issue #232: a
// loopzero-owned Helm chart whose `values.yaml` DEFAULTS its image tag to a
// mutable ref (`latest`, `stable`, a branch name).
//
// WHY THE EXISTING GATE CANNOT SEE THIS. `prod-image-tags` scans
// `infra/helmfile/environments/prod.yaml` + `infra/helmfile/values/prod` —
// the values actually deployed — and reported `✓ Scanned 394 file(s)` while
// the bootstrap chart's `values.yaml` said `tag: latest`. That scope is correct
// for its name; the gap is that nothing looks at chart defaults. Widening the
// existing gate would blur what a failure there means, so this is a sibling.
//
// WHY A DEFAULT MATTERS WHEN CI OVERRIDES IT. The usual objection is "Pulumi
// and CI always set the tag". That is exactly the problem: the default is only
// consulted on the path where somebody FORGOT to override, which is the path
// that most needs a loud failure. `latest` gives it the quietest one — the
// deploy either succeeds non-deterministically, or is rejected by the repo's
// own `vap-disallow-latest-tag` VAP with an error that points at admission
// rather than at the missing override.
//
// The repository already forbids `:latest` in-cluster. A loopzero-owned chart
// defaulting to it is the policy contradicting itself.
//
// PREFERRED FIX is `tag: ""` rather than a pinned version: an empty default
// fails loudly and immediately if nothing overrides it, where a pinned one
// silently ships a stale image. The gate accepts both — which of the two to
// use is a judgement the gate should not make for the author.
package chartdefaulttags

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// DefaultChartGlob is the loopzero-owned bootstrap chart family.
const DefaultChartGlob = "infra/helm/loopzero-*"

// BootstrapValuesGlob matches the bootstrap defaults of a chart Helmfile installs
// from charts/. When a bootstrap/published pair became one chart, the bootstrap
// defaults moved from infra/helm/<name>/values.yaml to this file, and they are
// still what a forgotten override ships (docs/decisions.md, "One chart per
// bootstrap/published pair").
const BootstrapValuesGlob = "charts/*/values-bootstrap.yaml"

// mutableTags are refs whose content can change under a fixed name.
var mutableTags = map[string]bool{
	"latest": true, "stable": true, "main": true,
	"master": true, "dev": true, "edge": true,
}

// tagRe matches a top-level-ish `tag:` assignment, capturing the value and
// ignoring a trailing comment. Anchored to line start + indentation so a
// `tag:` inside a prose comment does not match.
var tagRe = regexp.MustCompile(`(?m)^\s*tag:\s*"?([A-Za-z0-9._-]*)"?\s*(?:#.*)?$`)

// Config points the check at the chart family.
type Config struct {
	RepoRoot  string
	ChartGlob string // default: DefaultChartGlob
}

// Finding is one chart defaulting to a mutable tag.
type Finding struct {
	Chart string
	File  string
	Line  int
	Tag   string
}

// Outcome is the verdict.
type Outcome struct {
	Pass     bool
	Skipped  bool
	Reason   string
	Scanned  int
	Findings []Finding
}

// Verdict is Skip when the subject is absent from this checkout, and
// otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.Skipped {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// Check runs the guard.
func Check(cfg Config) (*Outcome, error) {
	if cfg.RepoRoot == "" {
		cfg.RepoRoot = "."
	}
	glob := cfg.ChartGlob
	if glob == "" {
		glob = DefaultChartGlob
	}

	dirs, err := filepath.Glob(filepath.Join(cfg.RepoRoot, glob))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", glob, err)
	}
	files := make([]string, 0, len(dirs))
	for _, d := range dirs {
		files = append(files, filepath.Join(d, "values.yaml"))
	}
	bootstrap, err := filepath.Glob(filepath.Join(cfg.RepoRoot, BootstrapValuesGlob))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", BootstrapValuesGlob, err)
	}
	files = append(files, bootstrap...)
	if len(files) == 0 {
		// Not a pass: say the charts were not found, so the green is not read
		// as proof that the defaults are sound.
		return &Outcome{
			Skipped: true,
			Reason:  fmt.Sprintf("no charts matched %s or %s — defaults NOT verified", glob, BootstrapValuesGlob),
		}, nil
	}
	sort.Strings(files)

	out := &Outcome{Pass: true}
	for _, vf := range files {
		body, readErr := os.ReadFile(vf) //nolint:gosec // operator-controlled repo path
		if readErr != nil {
			continue // a chart without values.yaml has no default to assert
		}
		d := filepath.Dir(vf)
		out.Scanned++
		rel, _ := filepath.Rel(cfg.RepoRoot, vf)
		for _, loc := range tagRe.FindAllStringSubmatchIndex(string(body), -1) {
			value := string(body)[loc[2]:loc[3]]
			if !mutableTags[strings.ToLower(value)] {
				continue
			}
			out.Pass = false
			out.Findings = append(out.Findings, Finding{
				Chart: filepath.Base(d),
				File:  filepath.ToSlash(rel),
				Line:  1 + strings.Count(string(body)[:loc[0]], "\n"),
				Tag:   value,
			})
		}
	}
	if out.Pass {
		out.Reason = fmt.Sprintf("%d loopzero-owned chart(s) scanned; no mutable default image tag", out.Scanned)
	}
	return out, nil
}
